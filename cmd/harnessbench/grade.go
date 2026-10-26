package main

import (
	"context"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/metrics"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/storage"
)

// gradedResult pairs one task execution with its grade.
type gradedResult struct {
	result benchmark.TaskResult
	grade  benchmark.Grade
}

// gradeResults grades every result against its task's GraderSpec, using
// that run's recorded events (fetched from store) so graders like
// tool_called can inspect tool calls. An execution that errored is graded
// as a failure without consulting its grader.
func gradeResults(ctx context.Context, results []benchmark.TaskResult, taskByID map[string]benchmark.Task, store *storage.SQLiteStore) []gradedResult {
	graded := make([]gradedResult, len(results))
	for i, result := range results {
		graded[i] = gradedResult{result: result, grade: gradeOne(ctx, result, taskByID, store)}
	}
	return graded
}

func gradeOne(ctx context.Context, result benchmark.TaskResult, taskByID map[string]benchmark.Task, store *storage.SQLiteStore) benchmark.Grade {
	if result.Err != nil {
		return benchmark.Grade{Pass: false, Reason: result.Err.Error()}
	}

	task, ok := taskByID[result.TaskID]
	if !ok {
		return benchmark.Grade{Pass: false, Reason: fmt.Sprintf("unknown task %q", result.TaskID)}
	}

	grader, err := benchmark.GraderFor(task.Grader)
	if err != nil {
		return benchmark.Grade{Pass: false, Reason: err.Error()}
	}

	// Only tool_called inspects the event log; loading it for every other
	// grader was the largest allocation source in the 20k-execution profile.
	var events []recorder.Event
	if task.Grader.Type == "tool_called" {
		events = eventsOf(ctx, store, result.RunID)
	}
	grade, err := grader.Grade(ctx, task, benchmark.RunResult{RunResult: result.Result, Events: events})
	if err != nil {
		return benchmark.Grade{Pass: false, Reason: err.Error()}
	}
	return grade
}

// toExecutions converts graded results into metrics.Execution values.
func toExecutions(results []gradedResult) []metrics.Execution {
	execs := make([]metrics.Execution, len(results))
	for i, r := range results {
		execs[i] = metrics.Execution{
			TaskID:       r.result.TaskID,
			Repeat:       r.result.Repeat,
			RunID:        r.result.RunID,
			Pass:         r.grade.Pass,
			Err:          r.result.Err,
			Duration:     r.result.Duration,
			Steps:        r.result.Result.Steps,
			Retries:      r.result.Result.Retries,
			InputTokens:  r.result.Result.Usage.InputTokens,
			OutputTokens: r.result.Result.Usage.OutputTokens,
		}
	}
	return execs
}
