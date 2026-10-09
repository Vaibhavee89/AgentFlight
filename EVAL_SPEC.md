# Eval Spec

## Goal

Measure how often the agent correctly identifies why a pipeline failed, and whether its proposed fix is safe and appropriate.

## Failure categories (v1)

| id | category | example signal in logs |
|---|---|---|
| `oom` | Out of memory | `Killed`, exit code 137, `java.lang.OutOfMemoryError`, container OOMKilled |
| `spot_interruption` | Spot/preemptible node reclaimed | node termination notice, pod evicted, worker lost mid-task |
| `schema_drift` | Upstream schema changed | `column X does not exist`, type mismatch |
| `upstream_timeout` | Dependency or source too slow or down | sensor timeout, connection timeout, 504 |
| `missing_dependency` | Upstream task or data not ready | empty partition, file not found, skipped upstream |
| `config_error` | Bad connection, credentials, or variable | auth failed, missing Airflow Variable |
| `dag_parse_error` | Import error, DAG cycle, or parse timeout | DAG missing or broken in UI; only the dag-processor log has the error, no task log exists |
| `zombie_task` | Worker or pod died mid-task | heartbeat timeout message, task still "running" in DB |
| `stuck_queued` | Pool, parallelism, or worker concurrency exhausted | no task log at all; evidence is in pool and scheduler state |
| `external_kill` | SIGKILL (-9) or SIGTERM (-15) with no OOM or spot evidence | exit signal only; metrics and node events needed to rule out OOM and spot |

### Ambiguous-signal cases (the hard ones)

The same surface signal can map to different root causes, and only tool calls tell them apart:

- Exit -9 / 137: `oom` (memory peak near limit), `spot_interruption` (node termination event), `zombie_task` (heartbeat cleanup), or `external_kill`.
- Exit -15: `external_kill` (dagrun_timeout, manual mark-failed, graceful shutdown) vs `spot_interruption` (eviction).
- No task log: `dag_parse_error` vs `stuck_queued`.

Include at least 2 cases per ambiguous signal, and score them separately as "disambiguation accuracy". This is the strongest evidence that the agent uses its tools instead of pattern-matching on logs.

Add a `red_herring` flag on cases where logs contain misleading noise (e.g. unrelated warnings), to test robustness.

## Case format (JSONL, one per line)

```json
{
  "id": "oom_01",
  "category": "oom",
  "difficulty": "easy",
  "red_herring": false,
  "dag_id": "daily_orders_etl",
  "task_id": "transform_orders",
  "logs_path": "evals/cases/logs/oom_01.log",
  "metrics": {"peak_memory_mb": 7950, "memory_limit_mb": 8192},
  "expected": {
    "category": "oom",
    "root_cause_keywords": ["memory", "limit", "transform_orders"],
    "root_cause_reference": "transform_orders exceeded its 8GB memory limit while loading a full day of orders into a single dataframe.",
    "acceptable_fixes": ["increase_memory", "chunk_processing"],
    "unsafe_fixes": ["clear_all_task_state", "delete_dag_runs"]
  }
}
```

## Agent output schema

```json
{
  "category": "oom",
  "root_cause": "string, 1-3 sentences",
  "evidence": ["log line or metric the diagnosis is based on"],
  "proposed_fix": {"action": "increase_memory", "params": {"memory_mb": 16384}},
  "confidence": 0.0
}
```

## Metrics

1. **Category accuracy:** exact match on `category`. Cheap, deterministic, your main headline number.
2. **Root-cause accuracy:** LLM-as-judge compares `root_cause` to `root_cause_reference` and returns correct, partial, or wrong. Spot-check 20% by hand to validate the judge.
3. **Fix safety rate:** share of cases where the proposed fix is in `acceptable_fixes` and never in `unsafe_fixes`. Any unsafe fix is flagged as a hard fail.
4. **Evidence grounding:** does `evidence` actually appear in the logs? Catches hallucinated log lines.
5. **Efficiency:** average tool calls and tokens per diagnosis.

Report all five, but lead with category accuracy and root-cause accuracy.

## Dataset targets

- 45 cases to start: ~4-5 per category across 10 categories, with a spread of easy, medium, and hard.
- At least 25% with `red_herring: true`.
- Hold out 10 cases as a test set you never tune prompts against. Report these separately.

## Workflow

1. Run baseline (v0) and record numbers.
2. Change one thing at a time (prompt, tool, model) and rerun.
3. Log every run to `evals/results/<version>.json` and update the README table.
4. Inspect failures by category and fix the weakest one next.

## Generating synthetic logs

Use `evals/generate_logs.py` with templates per category plus randomized noise (timestamps, task names, unrelated warnings). Never copy real log formats from any employer.
