package main

import (
	"time"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/metrics"
)

// toCompareDocument converts the rows and kill results of a compare run into
// the versioned, machine-readable form.
func toCompareDocument(header compareHeader, rows []compareRow, kills []killSummary) benchmark.CompareDocument {
	doc := benchmark.CompareDocument{
		SchemaVersion: benchmark.CompareSchemaVersion,
		GeneratedAt:   header.GeneratedAt.Format(time.RFC3339),
		GitCommit:     header.GitCommit,
		Seed:          header.Seed,
		Suite:         header.Suite,
		Repeats:       header.Repeats,
		FaultRates:    header.FaultRates,
		Rows:          make([]benchmark.RuntimeFaultRow, 0, len(rows)),
		KillScenario:  make([]benchmark.KillRow, 0, len(kills)),
	}
	for _, r := range rows {
		doc.Rows = append(doc.Rows, benchmark.RuntimeFaultRow{
			Runtime:            r.RuntimeName,
			FaultRate:          r.FaultRate,
			Executions:         r.Executions,
			SuccessRate:        r.SuccessRate(),
			MeanSteps:          r.MeanSteps(),
			P95LatencyMS:       float64(metrics.Percentile(r.Durations, 0.95)) / float64(time.Millisecond),
			DuplicateToolExecs: r.DuplicateToolExecs(),
			RunsWithFinalState: r.FinalStateCount,
		})
	}
	for _, k := range kills {
		doc.KillScenario = append(doc.KillScenario, benchmark.KillRow{
			Runtime:            k.RuntimeName,
			Attempts:           k.Attempts,
			Recovered:          k.Recovered,
			RecoveryRate:       k.RecoveryRate(),
			TotalToolExecs:     k.TotalToolExecs,
			ExpectedToolExecs:  k.ExpectedToolExecs,
			DuplicateToolExecs: k.DuplicateToolExecs(),
		})
	}
	return doc
}
