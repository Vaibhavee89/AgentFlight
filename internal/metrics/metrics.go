package metrics

import (
	"math"
	"sort"
	"time"
)

// Execution is one task run's outcome, as input to Compute. Pass reflects
// grading (not just the absence of Err): a run can finish without error and
// still fail its task's grader.
type Execution struct {
	TaskID       string
	Repeat       int
	RunID        string
	Pass         bool
	Err          error
	Duration     time.Duration
	Steps        int
	Retries      int
	Model        string
	InputTokens  int
	OutputTokens int
}

// TaskMetrics summarizes every Execution recorded for one task.
type TaskMetrics struct {
	TaskID            string
	Executions        int
	PassAt1           bool // whether the repeat-0 execution passed
	PassAll           bool // whether every repeat passed
	PassRate          float64
	P50Latency        time.Duration
	P95Latency        time.Duration
	AvgSteps          float64
	TotalInputTokens  int
	TotalOutputTokens int
	EstimatedCostUSD  float64
	Retries           int
	ErrorCounts       map[string]int
}

// AggregateMetrics summarizes every Execution across every task.
type AggregateMetrics struct {
	Tasks             int
	Executions        int
	PassAt1Rate       float64 // fraction of tasks whose repeat-0 execution passed
	PassAllRate       float64 // fraction of tasks where every repeat passed
	OverallPassRate   float64 // fraction of all executions that passed
	P50Latency        time.Duration
	P95Latency        time.Duration
	AvgSteps          float64
	TotalInputTokens  int
	TotalOutputTokens int
	EstimatedCostUSD  float64
	Retries           int
	ErrorCounts       map[string]int
}

// Report is the full metrics output for a suite run.
type Report struct {
	Tasks     []TaskMetrics // sorted by TaskID
	Aggregate AggregateMetrics
}

// Compute aggregates executions into a Report, grouped and sorted by
// TaskID for reproducible output, and priced using prices.
func Compute(executions []Execution, prices PriceTable) Report {
	byTask := make(map[string][]Execution)
	for _, e := range executions {
		byTask[e.TaskID] = append(byTask[e.TaskID], e)
	}

	taskIDs := make([]string, 0, len(byTask))
	for id := range byTask {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)

	tasks := make([]TaskMetrics, 0, len(taskIDs))
	for _, id := range taskIDs {
		tasks = append(tasks, computeTaskMetrics(id, byTask[id], prices))
	}

	return Report{
		Tasks:     tasks,
		Aggregate: computeAggregate(tasks, executions, prices),
	}
}

func computeTaskMetrics(taskID string, execs []Execution, prices PriceTable) TaskMetrics {
	sorted := append([]Execution(nil), execs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Repeat < sorted[j].Repeat })

	m := TaskMetrics{TaskID: taskID, Executions: len(sorted), ErrorCounts: map[string]int{}}

	var durations []time.Duration
	var totalSteps, passCount int
	allPass := true

	for i, e := range sorted {
		if e.Pass {
			passCount++
		} else {
			allPass = false
		}
		if i == 0 {
			m.PassAt1 = e.Pass
		}

		durations = append(durations, e.Duration)
		totalSteps += e.Steps
		m.Retries += e.Retries
		m.TotalInputTokens += e.InputTokens
		m.TotalOutputTokens += e.OutputTokens
		m.EstimatedCostUSD += prices.Cost(e.Model, e.InputTokens, e.OutputTokens)

		if class := classifyExecution(e); class != "" {
			m.ErrorCounts[class]++
		}
	}

	m.PassAll = allPass
	if len(sorted) > 0 {
		m.PassRate = float64(passCount) / float64(len(sorted))
		m.AvgSteps = float64(totalSteps) / float64(len(sorted))
	}
	m.P50Latency = Percentile(durations, 0.50)
	m.P95Latency = Percentile(durations, 0.95)

	return m
}

func computeAggregate(tasks []TaskMetrics, execs []Execution, prices PriceTable) AggregateMetrics {
	agg := AggregateMetrics{ErrorCounts: map[string]int{}}
	agg.Tasks = len(tasks)
	if len(tasks) > 0 {
		var pass1, passAll int
		for _, t := range tasks {
			if t.PassAt1 {
				pass1++
			}
			if t.PassAll {
				passAll++
			}
			for class, count := range t.ErrorCounts {
				agg.ErrorCounts[class] += count
			}
		}
		agg.PassAt1Rate = float64(pass1) / float64(len(tasks))
		agg.PassAllRate = float64(passAll) / float64(len(tasks))
	}

	var durations []time.Duration
	var totalSteps, totalPass int
	for _, e := range execs {
		durations = append(durations, e.Duration)
		totalSteps += e.Steps
		if e.Pass {
			totalPass++
		}
		agg.Retries += e.Retries
		agg.TotalInputTokens += e.InputTokens
		agg.TotalOutputTokens += e.OutputTokens
		agg.EstimatedCostUSD += prices.Cost(e.Model, e.InputTokens, e.OutputTokens)
	}
	agg.Executions = len(execs)
	if len(execs) > 0 {
		agg.OverallPassRate = float64(totalPass) / float64(len(execs))
		agg.AvgSteps = float64(totalSteps) / float64(len(execs))
	}
	agg.P50Latency = Percentile(durations, 0.50)
	agg.P95Latency = Percentile(durations, 0.95)

	return agg
}

// classifyExecution reports e's error class: the typed-error class of
// e.Err if it errored, ErrorClassGradeMismatch if it ran without error but
// did not pass, or "" if it passed.
func classifyExecution(e Execution) string {
	if e.Err != nil {
		return ClassifyError(e.Err)
	}
	if !e.Pass {
		return ErrorClassGradeMismatch
	}
	return ""
}

// Percentile returns the nearest-rank p-th percentile (0 < p <= 1) of
// durations, which is not modified.
func Percentile(durations []time.Duration, p float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
