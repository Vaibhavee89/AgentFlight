package benchmark

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

// Config tunes how a Runner executes a suite.
type Config struct {
	// Concurrency is the maximum number of tasks run at once. Values <= 0
	// are treated as 1.
	Concurrency int
	// PerTaskTimeout bounds each task's run when the task itself does not
	// set a Timeout.
	PerTaskTimeout time.Duration
	// Repeats is how many times each task is run. Values <= 0 are treated
	// as 1.
	Repeats int
}

// TaskResult is the outcome of running one task once. RunID is always set
// (unlike Result.RunID, which is only set on a successful completion), so a
// failed execution can still be identified and replayed.
type TaskResult struct {
	TaskID   string
	Repeat   int
	RunID    string
	Result   runtime.RunResult
	Err      error
	Duration time.Duration
}

// Runner executes a benchmark suite with a bounded worker pool.
type Runner struct {
	config Config
}

// NewRunner builds a Runner from config, defaulting Concurrency and Repeats
// to 1 if not positive.
func NewRunner(config Config) *Runner {
	if config.Concurrency <= 0 {
		config.Concurrency = 1
	}
	if config.Repeats <= 0 {
		config.Repeats = 1
	}
	return &Runner{config: config}
}

type indexedJob struct {
	index  int
	task   Task
	repeat int
}

type indexedResult struct {
	index  int
	result TaskResult
}

// Run executes every task in tasks, Repeats times each, across a bounded
// pool of at most Concurrency workers. A single task's failure (returned
// via TaskResult.Err) never stops the suite. Results are returned in input
// order (grouped by task, then by repeat) regardless of completion order.
//
// If ctx is cancelled, Run stops starting new tasks, lets any in-flight
// ones unwind (each task's own context is derived from ctx, so it is
// cancelled too), and returns the results collected so far alongside ctx's
// error; entries for tasks that never started are left as a zero
// TaskResult.
func (r *Runner) Run(ctx context.Context, tasks []Task, newRuntime func() *runtime.Runtime) ([]TaskResult, error) {
	total := len(tasks) * r.config.Repeats
	results := make([]TaskResult, total)
	if total == 0 {
		return results, nil
	}

	jobs := make(chan indexedJob)
	// Buffered to exactly the number of results every worker could ever
	// produce, so no worker blocks sending and Wait can complete before we
	// drain the channel.
	resultsCh := make(chan indexedResult, total)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		defer close(jobs)
		idx := 0
		for _, task := range tasks {
			for rep := 0; rep < r.config.Repeats; rep++ {
				select {
				case jobs <- indexedJob{index: idx, task: task, repeat: rep}:
				case <-gctx.Done():
					return nil
				}
				idx++
			}
		}
		return nil
	})

	for w := 0; w < r.config.Concurrency; w++ {
		g.Go(func() error {
			for j := range jobs {
				resultsCh <- indexedResult{index: j.index, result: r.runOne(gctx, j.task, j.repeat, newRuntime)}
			}
			return nil
		})
	}

	waitErr := g.Wait()
	close(resultsCh)

	for ir := range resultsCh {
		results[ir.index] = ir.result
	}

	if waitErr != nil {
		return results, waitErr
	}
	return results, ctx.Err()
}

// runOne runs task once under its own timeout (task.Timeout if set,
// otherwise the Runner's PerTaskTimeout), building a fresh Runtime via
// newRuntime for each run.
func (r *Runner) runOne(ctx context.Context, task Task, repeat int, newRuntime func() *runtime.Runtime) TaskResult {
	timeout := task.Timeout
	if timeout <= 0 {
		timeout = r.config.PerTaskTimeout
	}

	taskCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		taskCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	rt := newRuntime()
	runID := fmt.Sprintf("%s#%d", task.ID, repeat)

	start := time.Now()
	result, err := rt.Run(taskCtx, runID, task.Prompt)
	duration := time.Since(start)

	return TaskResult{TaskID: task.ID, Repeat: repeat, RunID: runID, Result: result, Err: err, Duration: duration}
}
