# AgentFlight vs Baseline: Fault-Injection Comparison

- Generated: 2026-10-26T05:18:06Z
- Git commit: 268fb130398514684e97e4d19ba72d227d5adeb4
- Seed: 1
- Suite: benchmarks/basic.jsonl
- Repeats per task per fault rate: 10
- Fault rates: 0.00, 0.05, 0.10, 0.20
- Events DB (AgentFlight only): harnessbench-compare-events.db

## Fault-rate sweep

Both runtimes see the exact same scripted responses and the exact same seeded fault sequence at each rate; only the harness around the tool/provider calls differs.

| Runtime | Fault rate | Success rate | Mean steps | p95 latency | Duplicate tool execs | Runs with recorded final state |
| --- | --- | --- | --- | --- | --- | --- |
| agentflight | 0.00 | 100.0% | 2.25 | 1.470041ms | 0 | 120/120 |
| baseline | 0.00 | 75.0% | 2.08 | 1.042µs | 0 | 0/120 |
| agentflight | 0.05 | 88.3% | 1.99 | 31.978291ms | 9 | 120/120 |
| baseline | 0.05 | 63.3% | 1.73 | 400.644667ms | 0 | 0/120 |
| agentflight | 0.10 | 78.3% | 1.74 | 33.100417ms | 13 | 120/120 |
| baseline | 0.10 | 52.5% | 1.42 | 400.322541ms | 0 | 0/120 |
| agentflight | 0.20 | 58.3% | 1.30 | 44.592167ms | 24 | 120/120 |
| baseline | 0.20 | 40.8% | 1.07 | 400.736625ms | 0 | 0/120 |

At 0% faults the baseline passes 9 of 12 tasks, for two different reasons: task-12 fails because baseline aborts on the first tool error (the calculator's divide-by-zero) instead of returning it to the model, a real capability gap, and tasks 09 and 10 fail because they are graded `tool_called`, which reads the event log that baseline does not keep, so they are counted as never called even though baseline made the call, a grading artifact.

The p95 latency column measures how long a run lasted before it ended, and under faults it is set by the harness rather than by either runtime's own behavior. Every run gets the same 400 ms deadline (`perRunTimeout` in `cmd/harnessbench/compare.go`). Baseline has no per-tool timeout, so a hung tool call holds the run until that deadline ends it, which is why its p95 is about 400 ms and is a ceiling, not a measured duration. AgentFlight sets a 30 ms per-tool timeout, so a hung call fails after 30 ms and is retried, and its p95 is about 30–45 ms, roughly one timed-out attempt plus a successful retry. The latency gap is therefore the per-tool timeout, not faster execution; the numbers do not show how long a successful baseline run would take.

Notes: "Duplicate tool execs" is total tool-executor calls minus the number of tool calls the scripted responses make exactly once; for AgentFlight this counts policy retries after transient faults, for baseline it is always 0 since baseline never retries. "Runs with recorded final state" is always 0 for baseline, which keeps no event log at all.

## Mid-run kill scenario

Each multi-step task's context is cancelled after a randomly chosen tool call (seeded, so the kill point is reproducible), then AgentFlight calls Resume while baseline restarts the task from scratch with no memory of the killed attempt.

| Runtime | Attempts | Recovered | Recovery rate | Total tool execs | Expected tool execs | Duplicate tool execs |
| --- | --- | --- | --- | --- | --- | --- |
| agentflight | 30 | 30 | 100.0% | 60 | 60 | 0 |
| baseline | 30 | 20 | 66.7% | 90 | 60 | 30 |

Notes: a duplicate here means a tool call was executed again after it had already completed successfully once. AgentFlight's Resume reuses the recorded result for any call already completed before the kill, so it should show 0 duplicates; baseline's restart-from-scratch redoes every call from step 0, so it should show one duplicate per completed call before the kill.

