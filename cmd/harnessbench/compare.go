package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/baseline"
	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/benchmark/fault"
	"github.com/Vaibhavee89/agentflight/internal/metrics"
	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/storage"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

// perRunTimeout bounds every single execution in `compare`, so an injected
// tool hang or provider latency never blocks the whole comparison for long.
const perRunTimeout = 400 * time.Millisecond

// runSuite implements `harnessbench compare -suite <path.jsonl> [-faults
// r1,r2,...] [-repeats N] [-report path.md] [-events-db path] [-seed n]`:
// at each fault rate, it runs the whole suite through both the AgentFlight
// runtime and internal/baseline against the same injected faults, plus a
// mid-run-kill-and-recover scenario, and writes a comparison table to a
// Markdown report.
func runCompare(args []string) {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	suite := fs.String("suite", "", "path to a benchmark suite JSONL file")
	faultsFlag := fs.String("faults", "0", "comma-separated list of fault rates in [0,1]")
	repeats := fs.Int("repeats", 1, "number of repeats per task at each fault rate")
	reportPath := fs.String("report", "docs/results.md", "path to write the comparison report to")
	eventsDB := fs.String("events-db", "harnessbench-compare-events.db", "path to the SQLite event store used by the AgentFlight runtime")
	seed := fs.Int64("seed", 1, "base seed for fault injection, for reproducibility")
	fs.Parse(args)

	if *suite == "" {
		fmt.Fprintln(os.Stderr, "usage: harnessbench compare -suite <path.jsonl> [-faults r1,r2,...] [-repeats N] [-report path.md] [-events-db path] [-seed n]")
		os.Exit(2)
	}

	faultRates, err := parseFaultRates(*faultsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse -faults: %v\n", err)
		os.Exit(1)
	}

	s, err := loadSuite(*suite)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

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

	var rows []compareRow
	for _, rate := range faultRates {
		rows = append(rows, runComboSuite(ctx, "agentflight", rate, s, *repeats, *seed, store))
		rows = append(rows, runComboSuite(ctx, "baseline", rate, s, *repeats, *seed, nil))
	}

	killSummaries := runKillScenario(ctx, s, *repeats, *seed, store)

	if err := writeCompareReport(*reportPath, compareHeader{
		GeneratedAt: time.Now().UTC(),
		GitCommit:   gitCommit(),
		Seed:        *seed,
		Suite:       *suite,
		Repeats:     *repeats,
		FaultRates:  faultRates,
		EventsDB:    *eventsDB,
	}, rows, killSummaries); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("compare complete across %d fault rate(s); report written to %s\n", len(faultRates), *reportPath)
}

// discardLogger silences Runtime's step/error diagnostics for `compare`:
// under fault injection and deliberate mid-run kills, Runtime logs are
// expected and routine, not actionable, so they would otherwise just spam
// the terminal for every injected failure.
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func parseFaultRates(spec string) ([]float64, error) {
	parts := strings.Split(spec, ",")
	rates := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		rate, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid fault rate %q: %w", p, err)
		}
		rates = append(rates, rate)
	}
	if len(rates) == 0 {
		return nil, fmt.Errorf("no fault rates given")
	}
	return rates, nil
}

// deriveSeed combines base with a rate/task/repeat identity into a single
// deterministic seed, so every execution gets its own reproducible fault
// sequence instead of all executions replaying an identical one.
func deriveSeed(base int64, rate float64, taskIndex, repeat int) int64 {
	rateBits := int64(rate * 1_000_000)
	return base*1_000_000_007 + rateBits*104729 + int64(taskIndex)*97 + int64(repeat)
}

// countingExecutor wraps a tools.Tool-shaped executor, counting every call
// made to it (including retries), used to measure duplicate tool
// executions.
type countingExecutor struct {
	inner   fault.ToolExecutor
	counter *int64
}

func (c countingExecutor) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	atomic.AddInt64(c.counter, 1)
	return c.inner.Execute(ctx, call)
}

func newToolRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewEcho())
	reg.Register(builtin.NewCalculator())
	return reg
}

// gradeContent grades result against task, treating a grader error the
// same as a failing grade.
func gradeContent(task benchmark.Task, result benchmark.RunResult) benchmark.Grade {
	grader, err := benchmark.GraderFor(task.Grader)
	if err != nil {
		return benchmark.Grade{Pass: false, Reason: err.Error()}
	}
	grade, err := grader.Grade(context.Background(), task, result)
	if err != nil {
		return benchmark.Grade{Pass: false, Reason: err.Error()}
	}
	return grade
}

// eventLogHasFinalState reports whether events records how a run ended:
// either a clean run_finished or a recorded terminal error.
func eventLogHasFinalState(events []recorder.Event) bool {
	for _, e := range events {
		if e.Type == recorder.EventRunFinished || e.Type == recorder.EventError {
			return true
		}
	}
	return false
}

// runOutcome is one execution's measurements, common to both runtimes.
type runOutcome struct {
	pass          bool
	steps         int
	duration      time.Duration
	hasFinalState bool
	toolExecs     int
}

// compareRow aggregates every execution for one (runtime, fault rate) combination.
type compareRow struct {
	RuntimeName     string
	FaultRate       float64
	Executions      int
	Passes          int
	TotalSteps      int
	Durations       []time.Duration
	FinalStateCount int
	// duplicateToolExecs is the sum, across executions, of each
	// execution's own (tool execs - expected tool calls) floored at 0.
	// Flooring per execution (not after summing) matters here: a run that
	// aborted before any tool call (toolExecs=0, a shortfall) must not
	// cancel out a different run's retry-driven surplus when both are
	// added together.
	duplicateToolExecs int
}

func (r compareRow) SuccessRate() float64 {
	if r.Executions == 0 {
		return 0
	}
	return float64(r.Passes) / float64(r.Executions)
}

func (r compareRow) MeanSteps() float64 {
	if r.Executions == 0 {
		return 0
	}
	return float64(r.TotalSteps) / float64(r.Executions)
}

func (r compareRow) P95Latency() time.Duration {
	return metrics.Percentile(r.Durations, 0.95)
}

func (r compareRow) DuplicateToolExecs() int {
	return r.duplicateToolExecs
}

// runComboSuite runs every task in s, repeats times each, through
// runtimeName ("agentflight" or "baseline") with faults injected at rate,
// aggregating the results into one compareRow.
func runComboSuite(ctx context.Context, runtimeName string, rate float64, s loadedSuite, repeats int, baseSeed int64, store *storage.SQLiteStore) compareRow {
	row := compareRow{RuntimeName: runtimeName, FaultRate: rate}

	for ti, task := range s.tasks {
		script := s.byPrompt[task.Prompt]
		expected := toolCallCount(script)

		for rep := 0; rep < repeats; rep++ {
			seed := deriveSeed(baseSeed, rate, ti, rep)

			var outcome runOutcome
			if runtimeName == "agentflight" {
				outcome = runAgentFlightOnce(ctx, task, script, rep, rate, seed, store)
			} else {
				outcome = runBaselineOnce(ctx, task, script, rep, rate, seed)
			}

			row.Executions++
			if outcome.pass {
				row.Passes++
			}
			row.TotalSteps += outcome.steps
			row.Durations = append(row.Durations, outcome.duration)
			if outcome.hasFinalState {
				row.FinalStateCount++
			}
			if dup := outcome.toolExecs - expected; dup > 0 {
				row.duplicateToolExecs += dup
			}
		}
	}

	return row
}

func faultConfig(rate float64) fault.Config {
	return fault.Config{
		ProviderErrorRate:   rate,
		LatencyRate:         rate,
		Latency:             5 * time.Millisecond,
		ToolTimeoutRate:     rate,
		MalformedOutputRate: rate,
	}
}

func runAgentFlightOnce(ctx context.Context, task benchmark.Task, script []provider.ModelResponse, repeat int, rate float64, seed int64, store *storage.SQLiteStore) runOutcome {
	innerProvider := &scriptedProvider{byPrompt: map[string][]provider.ModelResponse{task.Prompt: script}}
	faultedProvider := fault.NewFaultProvider(innerProvider, faultConfig(rate), seed)

	var toolExecs int64
	faultedTool := fault.NewFaultTool(newToolRegistry(), faultConfig(rate), seed+1)
	counting := countingExecutor{inner: faultedTool, counter: &toolExecs}

	pol := policy.NewDefaultPolicy([]string{"echo", "calculator"},
		policy.WithMaxAttempts(3),
		policy.WithBackoff(2*time.Millisecond, 10*time.Millisecond),
		// Without a per-tool timeout, a hung call blocks until the whole
		// run's context expires, which Classify treats as Terminal (plain
		// context.DeadlineExceeded) rather than retryable. This turns an
		// injected tool hang into policy.ErrToolTimeout, which Classify
		// does treat as Retryable, so FaultTool's ToolTimeoutRate is
		// actually something AgentFlight's retry policy can recover from.
		policy.WithTimeout(30*time.Millisecond),
	)

	rec := recorder.NewRecorder(store)
	runID := fmt.Sprintf("compare-af-%.3f-%s-%d", rate, task.ID, repeat)

	rt := runtime.NewRuntime(faultedProvider, counting, rec,
		runtime.WithMaxSteps(len(script)+5),
		runtime.WithPolicy(pol),
		runtime.WithLogger(discardLogger),
	)

	runCtx, cancel := context.WithTimeout(ctx, perRunTimeout)
	defer cancel()

	start := time.Now()
	result, runErr := rt.Run(runCtx, runID, task.Prompt)
	duration := time.Since(start)

	events, _ := store.Events(context.Background(), runID)

	pass := false
	if runErr == nil {
		pass = gradeContent(task, benchmark.RunResult{RunResult: result, Events: events}).Pass
	}

	return runOutcome{
		pass:          pass,
		steps:         result.Steps,
		duration:      duration,
		hasFinalState: eventLogHasFinalState(events),
		toolExecs:     int(atomic.LoadInt64(&toolExecs)),
	}
}

func runBaselineOnce(ctx context.Context, task benchmark.Task, script []provider.ModelResponse, repeat int, rate float64, seed int64) runOutcome {
	innerProvider := &scriptedProvider{byPrompt: map[string][]provider.ModelResponse{task.Prompt: script}}
	faultedProvider := fault.NewFaultProvider(innerProvider, faultConfig(rate), seed)

	var toolExecs int64
	faultedTool := fault.NewFaultTool(newToolRegistry(), faultConfig(rate), seed+1)
	counting := countingExecutor{inner: faultedTool, counter: &toolExecs}

	runID := fmt.Sprintf("compare-base-%.3f-%s-%d", rate, task.ID, repeat)

	runCtx, cancel := context.WithTimeout(ctx, perRunTimeout)
	defer cancel()

	start := time.Now()
	result, err := baseline.Run(runCtx, faultedProvider, counting, runID, task.Prompt)
	duration := time.Since(start)

	pass := false
	if err == nil {
		res := benchmark.RunResult{RunResult: runtime.RunResult{
			RunID: result.RunID, Content: result.Content, Steps: result.Steps, Status: "completed",
		}}
		pass = gradeContent(task, res).Pass
	}

	return runOutcome{
		pass:          pass,
		steps:         result.Steps,
		duration:      duration,
		hasFinalState: false, // baseline has no event log at all
		toolExecs:     int(atomic.LoadInt64(&toolExecs)),
	}
}

// killOutcome is one mid-run-kill-and-recover attempt's measurements.
type killOutcome struct {
	runtimeName       string
	taskID            string
	repeat            int
	recovered         bool
	toolExecs         int
	expectedToolExecs int
}

// killSummary aggregates killOutcomes for one runtime across every
// scenario attempted.
type killSummary struct {
	RuntimeName       string
	Attempts          int
	Recovered         int
	TotalToolExecs    int
	ExpectedToolExecs int
}

func (k killSummary) RecoveryRate() float64 {
	if k.Attempts == 0 {
		return 0
	}
	return float64(k.Recovered) / float64(k.Attempts)
}

func (k killSummary) DuplicateToolExecs() int {
	d := k.TotalToolExecs - k.ExpectedToolExecs
	if d < 0 {
		return 0
	}
	return d
}

// runKillScenario cancels a run partway through a multi-step task (after a
// randomly chosen tool call, via a seeded RNG) and compares AgentFlight's
// Resume against baseline's only option, restarting from scratch.
func runKillScenario(ctx context.Context, s loadedSuite, repeats int, baseSeed int64, store *storage.SQLiteStore) []killSummary {
	var multiStep []benchmark.Task
	for _, task := range s.tasks {
		if toolCallCount(s.byPrompt[task.Prompt]) >= 2 {
			multiStep = append(multiStep, task)
		}
	}

	summaries := map[string]*killSummary{
		"agentflight": {RuntimeName: "agentflight"},
		"baseline":    {RuntimeName: "baseline"},
	}

	for ti, task := range multiStep {
		script := s.byPrompt[task.Prompt]
		for rep := 0; rep < repeats; rep++ {
			seed := deriveSeed(baseSeed, -1, ti, rep)

			af := runAgentFlightKill(ctx, task, script, rep, seed, store)
			accumulateKill(summaries["agentflight"], af)

			base := runBaselineKill(ctx, task, script, rep, seed)
			accumulateKill(summaries["baseline"], base)
		}
	}

	return []killSummary{*summaries["agentflight"], *summaries["baseline"]}
}

func accumulateKill(sum *killSummary, o killOutcome) {
	sum.Attempts++
	if o.recovered {
		sum.Recovered++
	}
	sum.TotalToolExecs += o.toolExecs
	sum.ExpectedToolExecs += o.expectedToolExecs
}

// killAfterCount picks a random tool-call count, between 1 and totalCalls-1
// inclusive, after which to cancel a run, using a seeded RNG for
// reproducibility.
func killAfterCount(seed int64, totalCalls int) int {
	if totalCalls <= 1 {
		return 1
	}
	rng := rand.New(rand.NewSource(seed))
	return 1 + rng.Intn(totalCalls-1)
}

func runAgentFlightKill(ctx context.Context, task benchmark.Task, script []provider.ModelResponse, repeat int, seed int64, store *storage.SQLiteStore) killOutcome {
	innerProvider := &scriptedProvider{byPrompt: map[string][]provider.ModelResponse{task.Prompt: script}}
	totalCalls := toolCallCount(script)
	killAfter := killAfterCount(seed, totalCalls)

	var toolExecs, executed int64
	runCtx, cancel := context.WithCancel(context.Background())
	reg := newToolRegistry()
	killing := countingExecutorFunc(func(c context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
		out, err := reg.Execute(c, call)
		atomic.AddInt64(&toolExecs, 1)
		if atomic.AddInt64(&executed, 1) == int64(killAfter) {
			// Cancel synchronously, right as this call finishes: the
			// in-memory toolResults for this call are already captured by
			// the caller regardless, and the checkpoint save on the
			// resulting cancellation path uses a context detached from
			// this cancellation, so it still succeeds. Only this one
			// call's own tool_result *event* may fail to record (logged,
			// not fatal) — a faithful artifact of killing a run abruptly
			// enough to interrupt its own audit log write.
			cancel()
		}
		return out, err
	})

	runID := fmt.Sprintf("compare-kill-af-%s-%d", task.ID, repeat)
	rec := recorder.NewRecorder(store)
	rt := runtime.NewRuntime(innerProvider, killing, rec,
		runtime.WithMaxSteps(len(script)+5),
		runtime.WithCheckpoints(store),
		runtime.WithEventReader(store),
		runtime.WithLogger(discardLogger),
	)

	_, _ = rt.Run(runCtx, runID, task.Prompt)

	resumeCtx, resumeCancel := context.WithTimeout(context.Background(), perRunTimeout*4)
	defer resumeCancel()
	result, resumeErr := rt.Resume(resumeCtx, runID)

	pass := false
	if resumeErr == nil {
		events, _ := store.Events(context.Background(), runID)
		pass = gradeContent(task, benchmark.RunResult{RunResult: result, Events: events}).Pass
	}

	return killOutcome{
		runtimeName: "agentflight", taskID: task.ID, repeat: repeat,
		recovered: pass, toolExecs: int(atomic.LoadInt64(&toolExecs)), expectedToolExecs: totalCalls,
	}
}

func runBaselineKill(ctx context.Context, task benchmark.Task, script []provider.ModelResponse, repeat int, seed int64) killOutcome {
	byPrompt := map[string][]provider.ModelResponse{task.Prompt: script}
	totalCalls := toolCallCount(script)
	killAfter := killAfterCount(seed, totalCalls)

	// Baseline's loop never checks ctx between steps (true to its naive
	// design: no cancellation awareness), so a cancelled context alone
	// would not stop it. Instead, the call right after killAfter returns
	// an error, which baseline's own "abort on first error" check does
	// catch — a one-time trigger (via killed), so it never also misfires
	// during the restart below, which reuses the same counter so its
	// total still reflects both attempts.
	var toolExecs, executed int64
	var killed int32
	reg := newToolRegistry()
	killing := countingExecutorFunc(func(c context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
		n := atomic.AddInt64(&executed, 1)
		if n == int64(killAfter)+1 && atomic.CompareAndSwapInt32(&killed, 0, 1) {
			return tools.ToolOutput{}, context.Canceled
		}
		atomic.AddInt64(&toolExecs, 1)
		return reg.Execute(c, call)
	})

	runID := fmt.Sprintf("compare-kill-base-%s-%d", task.ID, repeat)
	runCtx, cancel := context.WithTimeout(ctx, perRunTimeout*4)
	defer cancel()
	firstAttemptProvider := &scriptedProvider{byPrompt: byPrompt}
	_, _ = baseline.Run(runCtx, firstAttemptProvider, killing, runID, task.Prompt)

	// Baseline keeps no state about what already happened, so recovering
	// means restarting the whole task from scratch with a fresh provider
	// cursor and a plain (non-killing) counting executor sharing the same
	// counter, so the total reflects both the killed attempt and the
	// full restart.
	restartCtx, restartCancel := context.WithTimeout(context.Background(), perRunTimeout*4)
	defer restartCancel()
	restartCounting := countingExecutorFunc(func(c context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
		atomic.AddInt64(&toolExecs, 1)
		return reg.Execute(c, call)
	})
	restartProvider := &scriptedProvider{byPrompt: byPrompt}
	result, err := baseline.Run(restartCtx, restartProvider, restartCounting, runID, task.Prompt)

	pass := false
	if err == nil {
		res := benchmark.RunResult{RunResult: runtime.RunResult{
			RunID: result.RunID, Content: result.Content, Steps: result.Steps, Status: "completed",
		}}
		pass = gradeContent(task, res).Pass
	}

	return killOutcome{
		runtimeName: "baseline", taskID: task.ID, repeat: repeat,
		recovered: pass, toolExecs: int(atomic.LoadInt64(&toolExecs)), expectedToolExecs: totalCalls,
	}
}

// countingExecutorFunc adapts a function to the fault.ToolExecutor shape.
type countingExecutorFunc func(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error)

func (f countingExecutorFunc) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	return f(ctx, call)
}
