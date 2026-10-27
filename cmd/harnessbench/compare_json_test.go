package main

import (
	"math"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
)

func TestToCompareDocument(t *testing.T) {
	header := compareHeader{Seed: 7, Suite: "s.jsonl", Repeats: 2, FaultRates: []float64{0, 0.2}, GeneratedAt: time.Unix(0, 0).UTC()}
	rows := []compareRow{
		{
			RuntimeName: "agentflight", FaultRate: 0.2, Executions: 4, Passes: 3, TotalSteps: 8,
			Durations:          []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond, 40 * time.Millisecond},
			FinalStateCount:    4,
			duplicateToolExecs: 2,
		},
	}
	kills := []killSummary{{RuntimeName: "baseline", Attempts: 4, Recovered: 2, TotalToolExecs: 9, ExpectedToolExecs: 6}}

	tests := []struct {
		name string
		got  func(benchmark.CompareDocument) bool
	}{
		{name: "schema version is set", got: func(d benchmark.CompareDocument) bool {
			return d.SchemaVersion == benchmark.CompareSchemaVersion
		}},
		{name: "success rate is a fraction", got: func(d benchmark.CompareDocument) bool {
			return d.Rows[0].SuccessRate == 0.75
		}},
		{name: "p95 is reported in milliseconds", got: func(d benchmark.CompareDocument) bool {
			return math.Abs(d.Rows[0].P95LatencyMS-40) < 1e-9
		}},
		{name: "duplicates come from the row", got: func(d benchmark.CompareDocument) bool {
			return d.Rows[0].DuplicateToolExecs == 2 && d.Rows[0].RunsWithFinalState == 4
		}},
		{name: "kill row recovery and duplicates", got: func(d benchmark.CompareDocument) bool {
			k := d.KillScenario[0]
			return k.RecoveryRate == 0.5 && k.DuplicateToolExecs == 3
		}},
	}

	doc := toCompareDocument(header, rows, kills)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.got(doc) {
				t.Fatalf("document = %+v", doc)
			}
		})
	}
}
