package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

func TestCompute_PerTaskPassAt1AndPassAll(t *testing.T) {
	tests := []struct {
		name        string
		execs       []Execution
		wantPassAt1 bool
		wantPassAll bool
		wantRate    float64
	}{
		{
			name: "all repeats pass",
			execs: []Execution{
				{TaskID: "t1", Repeat: 0, Pass: true},
				{TaskID: "t1", Repeat: 1, Pass: true},
			},
			wantPassAt1: true,
			wantPassAll: true,
			wantRate:    1.0,
		},
		{
			name: "first repeat fails but a later one passes",
			execs: []Execution{
				{TaskID: "t1", Repeat: 0, Pass: false},
				{TaskID: "t1", Repeat: 1, Pass: true},
			},
			wantPassAt1: false,
			wantPassAll: false,
			wantRate:    0.5,
		},
		{
			name: "first repeat passes but a later one fails",
			execs: []Execution{
				{TaskID: "t1", Repeat: 0, Pass: true},
				{TaskID: "t1", Repeat: 1, Pass: false},
			},
			wantPassAt1: true,
			wantPassAll: false,
			wantRate:    0.5,
		},
		{
			name: "all repeats fail",
			execs: []Execution{
				{TaskID: "t1", Repeat: 0, Pass: false},
				{TaskID: "t1", Repeat: 1, Pass: false},
			},
			wantPassAt1: false,
			wantPassAll: false,
			wantRate:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Compute(tt.execs, nil)
			if len(report.Tasks) != 1 {
				t.Fatalf("got %d task metrics, want 1", len(report.Tasks))
			}
			m := report.Tasks[0]
			if m.PassAt1 != tt.wantPassAt1 {
				t.Fatalf("PassAt1 = %v, want %v", m.PassAt1, tt.wantPassAt1)
			}
			if m.PassAll != tt.wantPassAll {
				t.Fatalf("PassAll = %v, want %v", m.PassAll, tt.wantPassAll)
			}
			if m.PassRate != tt.wantRate {
				t.Fatalf("PassRate = %v, want %v", m.PassRate, tt.wantRate)
			}
		})
	}
}

func TestCompute_PassAt1OutOfOrderRepeats(t *testing.T) {
	// Repeat 0 is listed second in the input; PassAt1 must still reflect
	// repeat 0 specifically, not input order.
	execs := []Execution{
		{TaskID: "t1", Repeat: 1, Pass: true},
		{TaskID: "t1", Repeat: 0, Pass: false},
	}
	report := Compute(execs, nil)
	if got := report.Tasks[0].PassAt1; got != false {
		t.Fatalf("PassAt1 = %v, want false (repeat 0 failed)", got)
	}
}

func TestCompute_AggregateAcrossTasks(t *testing.T) {
	execs := []Execution{
		{TaskID: "t1", Repeat: 0, Pass: true},
		{TaskID: "t1", Repeat: 1, Pass: true},
		{TaskID: "t2", Repeat: 0, Pass: true},
		{TaskID: "t2", Repeat: 1, Pass: false},
		{TaskID: "t3", Repeat: 0, Pass: false},
		{TaskID: "t3", Repeat: 1, Pass: false},
	}
	report := Compute(execs, nil)

	if len(report.Tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(report.Tasks))
	}
	// Tasks must be sorted by ID for reproducible output.
	wantOrder := []string{"t1", "t2", "t3"}
	for i, want := range wantOrder {
		if report.Tasks[i].TaskID != want {
			t.Fatalf("Tasks[%d].TaskID = %q, want %q", i, report.Tasks[i].TaskID, want)
		}
	}

	agg := report.Aggregate
	if agg.Tasks != 3 {
		t.Fatalf("Aggregate.Tasks = %d, want 3", agg.Tasks)
	}
	if agg.Executions != 6 {
		t.Fatalf("Aggregate.Executions = %d, want 6", agg.Executions)
	}
	// pass@1: t1 (true), t2 (true), t3 (false) -> 2/3
	if want := 2.0 / 3.0; agg.PassAt1Rate != want {
		t.Fatalf("Aggregate.PassAt1Rate = %v, want %v", agg.PassAt1Rate, want)
	}
	// pass-all: only t1 -> 1/3
	if want := 1.0 / 3.0; agg.PassAllRate != want {
		t.Fatalf("Aggregate.PassAllRate = %v, want %v", agg.PassAllRate, want)
	}
	// overall: 3 passes out of 6 executions
	if want := 0.5; agg.OverallPassRate != want {
		t.Fatalf("Aggregate.OverallPassRate = %v, want %v", agg.OverallPassRate, want)
	}
}

func TestCompute_Latency_Steps_Tokens_Retries_Cost(t *testing.T) {
	execs := []Execution{
		{TaskID: "t1", Repeat: 0, Pass: true, Duration: 10 * time.Millisecond, Steps: 1, Retries: 1, InputTokens: 100, OutputTokens: 50, Model: "m"},
		{TaskID: "t1", Repeat: 1, Pass: true, Duration: 20 * time.Millisecond, Steps: 3, Retries: 2, InputTokens: 200, OutputTokens: 100, Model: "m"},
		{TaskID: "t1", Repeat: 2, Pass: true, Duration: 30 * time.Millisecond, Steps: 2, Retries: 0, InputTokens: 300, OutputTokens: 150, Model: "m"},
	}
	prices := PriceTable{"m": {InputPerMillion: 1_000_000, OutputPerMillion: 2_000_000}}

	report := Compute(execs, prices)
	m := report.Tasks[0]

	if m.P50Latency != 20*time.Millisecond {
		t.Fatalf("P50Latency = %v, want 20ms", m.P50Latency)
	}
	if m.P95Latency != 30*time.Millisecond {
		t.Fatalf("P95Latency = %v, want 30ms", m.P95Latency)
	}
	if want := 2.0; m.AvgSteps != want {
		t.Fatalf("AvgSteps = %v, want %v", m.AvgSteps, want)
	}
	if m.TotalInputTokens != 600 || m.TotalOutputTokens != 300 {
		t.Fatalf("tokens = %d/%d, want 600/300", m.TotalInputTokens, m.TotalOutputTokens)
	}
	if m.Retries != 3 {
		t.Fatalf("Retries = %d, want 3", m.Retries)
	}
	// cost = 600 * 1 + 300 * 2 = 600 + 600 = 1200
	if want := 1200.0; m.EstimatedCostUSD != want {
		t.Fatalf("EstimatedCostUSD = %v, want %v", m.EstimatedCostUSD, want)
	}
}

func TestCompute_ErrorCountsByType(t *testing.T) {
	execs := []Execution{
		{TaskID: "t1", Repeat: 0, Pass: false, Err: errors.New("wrap: " + runtime.ErrMaxSteps.Error())},
		{TaskID: "t1", Repeat: 1, Pass: false, Err: runtime.ErrMaxSteps},
		{TaskID: "t1", Repeat: 2, Pass: false}, // no error, just failed grading
	}
	report := Compute(execs, nil)
	m := report.Tasks[0]

	// The first execution's error is a plain errors.New, not wrapped with
	// %w, so it must NOT be classified as max_steps (string matching is
	// not allowed); only the second one uses errors.Is-compatible wrapping.
	if got := m.ErrorCounts[ErrorClassMaxSteps]; got != 1 {
		t.Fatalf("ErrorCounts[max_steps] = %d, want 1", got)
	}
	if got := m.ErrorCounts[ErrorClassOther]; got != 1 {
		t.Fatalf("ErrorCounts[other] = %d, want 1", got)
	}
	if got := m.ErrorCounts[ErrorClassGradeMismatch]; got != 1 {
		t.Fatalf("ErrorCounts[grade_mismatch] = %d, want 1", got)
	}

	agg := report.Aggregate
	if agg.ErrorCounts[ErrorClassMaxSteps] != 1 || agg.ErrorCounts[ErrorClassOther] != 1 || agg.ErrorCounts[ErrorClassGradeMismatch] != 1 {
		t.Fatalf("Aggregate.ErrorCounts = %+v, want one each of max_steps/other/grade_mismatch", agg.ErrorCounts)
	}
}

func TestCompute_PassingExecutionHasNoErrorClass(t *testing.T) {
	execs := []Execution{{TaskID: "t1", Repeat: 0, Pass: true}}
	report := Compute(execs, nil)
	if len(report.Tasks[0].ErrorCounts) != 0 {
		t.Fatalf("ErrorCounts = %+v, want empty for a passing execution", report.Tasks[0].ErrorCounts)
	}
}

func TestCompute_Empty(t *testing.T) {
	report := Compute(nil, nil)
	if len(report.Tasks) != 0 {
		t.Fatalf("Tasks = %+v, want empty", report.Tasks)
	}
	if report.Aggregate.Tasks != 0 || report.Aggregate.Executions != 0 {
		t.Fatalf("Aggregate = %+v, want zero", report.Aggregate)
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name string
		in   []time.Duration
		p    float64
		want time.Duration
	}{
		{name: "empty", in: nil, p: 0.5, want: 0},
		{name: "single value", in: []time.Duration{5 * time.Millisecond}, p: 0.95, want: 5 * time.Millisecond},
		{
			name: "p50 of five values",
			in:   []time.Duration{1, 2, 3, 4, 5},
			p:    0.5,
			want: 3,
		},
		{
			name: "p95 of five values",
			in:   []time.Duration{1, 2, 3, 4, 5},
			p:    0.95,
			want: 5,
		},
		{
			name: "unsorted input is sorted first",
			in:   []time.Duration{5, 1, 3, 2, 4},
			p:    0.5,
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percentile(tt.in, tt.p); got != tt.want {
				t.Fatalf("percentile() = %v, want %v", got, tt.want)
			}
		})
	}
}
