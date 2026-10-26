package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/metrics"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/storage"
)

const version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("harnessbench " + version)
		return
	}

	switch os.Args[1] {
	case "run":
		runSuite(os.Args[2:])
	default:
		fmt.Println("harnessbench " + version)
	}
}

// runSuite implements `harnessbench run -suite <path.jsonl> [-concurrency N]
// [-repeats N] [-out results.json] [-report report.md] [-events-db path]
// [-prices path.json] [-timeout d] [-seed n]`: it loads the suite and its
// sibling *.mock.json script, runs it offline through benchmark.Runner with
// events persisted to a real SQLite store (so any run can be replayed with
// `agentflight replay`), grades each execution, and writes both the full
// JSON results and a Markdown summary report.
func runSuite(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	suite := fs.String("suite", "", "path to a benchmark suite JSONL file")
	concurrency := fs.Int("concurrency", 1, "maximum number of tasks to run at once")
	repeats := fs.Int("repeats", 1, "number of times to run each task")
	out := fs.String("out", "results.json", "path to write full JSON results to")
	reportPath := fs.String("report", "report.md", "path to write the Markdown summary report to")
	eventsDB := fs.String("events-db", "harnessbench-events.db", "path to the SQLite event store, replayable with agentflight replay")
	pricesPath := fs.String("prices", "", "optional path to a JSON price table for cost estimation")
	timeout := fs.Duration("timeout", 30*time.Second, "default per-task timeout when a task sets none")
	seed := fs.Int64("seed", 1, "seed recorded in the report header for reproducibility")
	fs.Parse(args)

	if *suite == "" {
		fmt.Fprintln(os.Stderr, "usage: harnessbench run -suite <path.jsonl> [-concurrency N] [-repeats N] [-out path.json] [-report path.md] [-events-db path] [-prices path.json] [-timeout d] [-seed n]")
		os.Exit(2)
	}

	tasks, err := benchmark.LoadTasks(*suite)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load suite: %v\n", err)
		os.Exit(1)
	}

	mockPath := strings.TrimSuffix(*suite, filepath.Ext(*suite)) + ".mock.json"
	scripts, err := benchmark.LoadMockScripts(mockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load mock scripts %s: %v\n", mockPath, err)
		os.Exit(1)
	}

	var prices metrics.PriceTable
	if *pricesPath != "" {
		prices, err = metrics.LoadPriceTable(*pricesPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load price table: %v\n", err)
			os.Exit(1)
		}
	}

	byPrompt := make(map[string][]provider.ModelResponse, len(tasks))
	taskByID := make(map[string]benchmark.Task, len(tasks))
	maxSteps := 10
	for _, task := range tasks {
		script := scripts[task.ID]
		byPrompt[task.Prompt] = script
		taskByID[task.ID] = task
		if len(script) > maxSteps {
			maxSteps = len(script)
		}
	}

	// Start from a clean event store each run: a stale file from a
	// previous run would collide on (run_id, seq) for any repeated run ID.
	if err := os.Remove(*eventsDB); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "remove stale events db: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	store, err := storage.Open(ctx, *eventsDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open events db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	config := benchmark.Config{
		Concurrency:    *concurrency,
		PerTaskTimeout: *timeout,
		Repeats:        *repeats,
	}
	runner := benchmark.NewRunner(config)

	results, runErr := runner.Run(ctx, tasks, newRuntimeFactory(byPrompt, store, maxSteps))
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "run suite: %v\n", runErr)
	}

	gradedResults := gradeResults(ctx, results, taskByID, store)

	if err := writeResults(*out, gradedResults); err != nil {
		fmt.Fprintf(os.Stderr, "write results: %v\n", err)
		os.Exit(1)
	}

	report := metrics.Compute(toExecutions(gradedResults), prices)
	header := reportHeader{
		GeneratedAt: time.Now().UTC(),
		GitCommit:   gitCommit(),
		Seed:        *seed,
		Suite:       *suite,
		EventsDB:    *eventsDB,
		Config:      config,
		PricesPath:  *pricesPath,
	}
	if err := writeReport(*reportPath, header, report, gradedResults); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ran %d task executions: %d ok, %d failed; results written to %s; report written to %s\n",
		len(gradedResults), countPassed(gradedResults), len(gradedResults)-countPassed(gradedResults), *out, *reportPath)

	if runErr != nil {
		os.Exit(1)
	}
}

func countPassed(results []gradedResult) int {
	n := 0
	for _, r := range results {
		if r.grade.Pass {
			n++
		}
	}
	return n
}

// eventsOf is a small convenience used by grading to fetch a run's recorded
// events, tolerating a nil store.
func eventsOf(ctx context.Context, store *storage.SQLiteStore, runID string) []recorder.Event {
	if store == nil {
		return nil
	}
	events, err := store.Events(ctx, runID)
	if err != nil {
		return nil
	}
	return events
}
