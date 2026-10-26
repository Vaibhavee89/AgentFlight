package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/metrics"
)

// reportHeader carries everything needed to reproduce a run, written at the
// top of report.md.
type reportHeader struct {
	GeneratedAt time.Time
	GitCommit   string
	Seed        int64
	Suite       string
	EventsDB    string
	Config      benchmark.Config
	PricesPath  string
}

// gitCommit returns the VCS revision Go's toolchain stamped into this
// binary at build time (via `go build`'s default VCS stamping), or
// "unknown" if it is not available.
func gitCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return "unknown"
}

// writeReport writes a Markdown summary of report to path: a reproducible
// header (generation time, git commit, seed, suite, and config), a
// per-task and aggregate summary table, an error-class breakdown, and a
// list of failures with run IDs so each can be replayed.
func writeReport(path string, header reportHeader, report metrics.Report, results []gradedResult) error {
	var b strings.Builder

	fmt.Fprintln(&b, "# Benchmark Report")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Generated: %s\n", header.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Git commit: %s\n", header.GitCommit)
	fmt.Fprintf(&b, "- Seed: %d\n", header.Seed)
	fmt.Fprintf(&b, "- Suite: %s\n", header.Suite)
	fmt.Fprintf(&b, "- Events DB: %s\n", header.EventsDB)
	fmt.Fprintf(&b, "- Concurrency: %d\n", header.Config.Concurrency)
	fmt.Fprintf(&b, "- Repeats: %d\n", header.Config.Repeats)
	fmt.Fprintf(&b, "- Per-task timeout: %s\n", header.Config.PerTaskTimeout)
	if header.PricesPath != "" {
		fmt.Fprintf(&b, "- Price table: %s\n", header.PricesPath)
	} else {
		fmt.Fprintln(&b, "- Price table: none (cost estimates are 0)")
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Task | Executions | Pass@1 | Pass-all | Pass rate | p50 | p95 | Avg steps | Tokens in/out | Cost (USD) | Retries |")
	fmt.Fprintln(&b, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, m := range report.Tasks {
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %.1f%% | %s | %s | %.2f | %d/%d | %.6f | %d |\n",
			m.TaskID, m.Executions, yesNo(m.PassAt1), yesNo(m.PassAll), m.PassRate*100,
			m.P50Latency, m.P95Latency, m.AvgSteps, m.TotalInputTokens, m.TotalOutputTokens,
			m.EstimatedCostUSD, m.Retries,
		)
	}
	fmt.Fprintln(&b)

	agg := report.Aggregate
	fmt.Fprintf(&b, "**Aggregate** — tasks: %d, executions: %d, pass@1: %.1f%%, pass-all: %.1f%%, overall pass rate: %.1f%%, p50: %s, p95: %s, avg steps: %.2f, tokens in/out: %d/%d, cost: $%.6f, retries: %d\n",
		agg.Tasks, agg.Executions, agg.PassAt1Rate*100, agg.PassAllRate*100, agg.OverallPassRate*100,
		agg.P50Latency, agg.P95Latency, agg.AvgSteps, agg.TotalInputTokens, agg.TotalOutputTokens,
		agg.EstimatedCostUSD, agg.Retries,
	)
	fmt.Fprintln(&b)

	if len(agg.ErrorCounts) > 0 {
		fmt.Fprintln(&b, "### Errors by type")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "| Class | Count |")
		fmt.Fprintln(&b, "| --- | --- |")
		for _, class := range sortedKeys(agg.ErrorCounts) {
			fmt.Fprintf(&b, "| %s | %d |\n", class, agg.ErrorCounts[class])
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Failures")
	fmt.Fprintln(&b)
	failures := sortedFailures(results)
	if len(failures) == 0 {
		fmt.Fprintln(&b, "None.")
	} else {
		for _, f := range failures {
			fmt.Fprintf(&b, "- `%s` (%s, repeat %d): %s — replay with `agentflight replay -db %s %s`\n",
				f.result.RunID, f.result.TaskID, f.result.Repeat, failureReason(f), header.EventsDB, f.result.RunID)
		}
	}
	fmt.Fprintln(&b)

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func failureReason(gr gradedResult) string {
	if gr.result.Err != nil {
		return metrics.ClassifyError(gr.result.Err) + ": " + gr.result.Err.Error()
	}
	return metrics.ErrorClassGradeMismatch + ": " + gr.grade.Reason
}

// sortedFailures returns every result that did not pass, sorted by
// (TaskID, Repeat) for reproducible output.
func sortedFailures(results []gradedResult) []gradedResult {
	var failures []gradedResult
	for _, r := range results {
		if !r.grade.Pass {
			failures = append(failures, r)
		}
	}
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].result.TaskID != failures[j].result.TaskID {
			return failures[i].result.TaskID < failures[j].result.TaskID
		}
		return failures[i].result.Repeat < failures[j].result.Repeat
	})
	return failures
}

// sortedKeys returns m's keys sorted, for reproducible map iteration order
// in the report.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
