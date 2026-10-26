package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// compareHeader carries everything needed to reproduce a `compare` run.
type compareHeader struct {
	GeneratedAt time.Time
	GitCommit   string
	Seed        int64
	Suite       string
	Repeats     int
	FaultRates  []float64
	EventsDB    string
}

// writeCompareReport writes a Markdown comparison of rows (the fault-rate
// sweep, one row per runtime per fault rate) and killSummaries (the
// mid-run-kill-and-recover scenario, one row per runtime) to path.
func writeCompareReport(path string, header compareHeader, rows []compareRow, killSummaries []killSummary) error {
	var b strings.Builder

	fmt.Fprintln(&b, "# AgentFlight vs Baseline: Fault-Injection Comparison")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Generated: %s\n", header.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Git commit: %s\n", header.GitCommit)
	fmt.Fprintf(&b, "- Seed: %d\n", header.Seed)
	fmt.Fprintf(&b, "- Suite: %s\n", header.Suite)
	fmt.Fprintf(&b, "- Repeats per task per fault rate: %d\n", header.Repeats)
	fmt.Fprintf(&b, "- Fault rates: %s\n", formatRates(header.FaultRates))
	fmt.Fprintf(&b, "- Events DB (AgentFlight only): %s\n", header.EventsDB)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Fault-rate sweep")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Both runtimes see the exact same scripted responses and the exact same seeded fault sequence at each rate; only the harness around the tool/provider calls differs.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Runtime | Fault rate | Success rate | Mean steps | p95 latency | Duplicate tool execs | Runs with recorded final state |")
	fmt.Fprintln(&b, "| --- | --- | --- | --- | --- | --- | --- |")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %.2f | %.1f%% | %.2f | %s | %d | %d/%d |\n",
			r.RuntimeName, r.FaultRate, r.SuccessRate()*100, r.MeanSteps(), r.P95Latency(),
			r.DuplicateToolExecs(), r.FinalStateCount, r.Executions,
		)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Notes: \"Duplicate tool execs\" is total tool-executor calls minus the number of tool calls the scripted responses make exactly once; for AgentFlight this counts policy retries after transient faults, for baseline it is always 0 since baseline never retries. \"Runs with recorded final state\" is always 0 for baseline, which keeps no event log at all.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Mid-run kill scenario")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Each multi-step task's context is cancelled after a randomly chosen tool call (seeded, so the kill point is reproducible), then AgentFlight calls Resume while baseline restarts the task from scratch with no memory of the killed attempt.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Runtime | Attempts | Recovered | Recovery rate | Total tool execs | Expected tool execs | Duplicate tool execs |")
	fmt.Fprintln(&b, "| --- | --- | --- | --- | --- | --- | --- |")
	for _, k := range killSummaries {
		fmt.Fprintf(&b, "| %s | %d | %d | %.1f%% | %d | %d | %d |\n",
			k.RuntimeName, k.Attempts, k.Recovered, k.RecoveryRate()*100,
			k.TotalToolExecs, k.ExpectedToolExecs, k.DuplicateToolExecs(),
		)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Notes: a duplicate here means a tool call was executed again after it had already completed successfully once. AgentFlight's Resume reuses the recorded result for any call already completed before the kill, so it should show 0 duplicates; baseline's restart-from-scratch redoes every call from step 0, so it should show one duplicate per completed call before the kill.")
	fmt.Fprintln(&b)

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func formatRates(rates []float64) string {
	parts := make([]string, len(rates))
	for i, r := range rates {
		parts[i] = fmt.Sprintf("%.2f", r)
	}
	return strings.Join(parts, ", ")
}
