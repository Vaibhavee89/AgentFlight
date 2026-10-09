# DP-Agent

An agent that diagnoses data pipeline failures (Airflow OOMs, spot interruptions, schema drift, upstream timeouts), explains the root cause in Slack, and proposes fixes that only run after human approval. Its diagnosis accuracy is measured by an eval suite against labeled synthetic failures.

> Inspired by problems I worked on during data-infra internships. All DAGs, logs, and failure cases in this repo are synthetic.

**Headline result:** _TBD, e.g. "Diagnoses 87% of 40 synthetic failures correctly (baseline: 52%)"_

<!-- demo.gif goes here -->

## Why this exists

On-call engineers lose time reading task logs to figure out why a DAG failed. Most agent demos are chatbots with no way to tell if they work. This one ships with an eval harness, so every change to prompts or tools shows up as a number.

## Architecture

```
Airflow (docker-compose) ──failure──▶ Webhook ──▶ Agent
                                                   │
                         ┌─────────────────────────┤
                         ▼                         ▼
                  MCP tools (RBAC)          Diagnosis + proposed fix
                  - get_task_logs                  │
                  - get_dag_metadata               ▼
                  - get_metrics                 Slack message
                  - (write) retry / resize         │
                         ▲                         ▼
                         └──── approval ◀──── Approve / Reject
```

_Replace with a proper diagram (Excalidraw or Mermaid) before publishing._

## Quickstart

```bash
git clone https://github.com/<you>/pipeline-medic && cd pipeline-medic
cp .env.example .env        # add ANTHROPIC_API_KEY, SLACK_BOT_TOKEN
docker compose up           # Airflow + agent + mock metrics
python scripts/break_dag.py oom_01     # trigger a synthetic failure
```

## How it works

1. **Failure simulator:** a set of Airflow DAGs that can be broken on demand.
2. **Agent:** reads logs and metadata through MCP tools, returns `{root_cause, category, evidence, proposed_fix}`.
3. **RBAC:** read tools are allowed by default. Write tools (retry, resize memory) require an approved role and a Slack approval.
4. **Evals:** labeled failure cases, scored automatically on every change.

## Evals

See [EVAL_SPEC.md](EVAL_SPEC.md).

| Version | Change | Category accuracy | Root-cause accuracy | Avg tool calls |
|---|---|---|---|---|
| v0 | Baseline, logs only | _TBD_ | _TBD_ | _TBD_ |
| v1 | + metrics tool | _TBD_ | _TBD_ | _TBD_ |
| v2 | + prompt revision | _TBD_ | _TBD_ | _TBD_ |

Run: `python -m evals.run --cases evals/cases/ --out evals/results/`

## Repo structure

```
pipeline-medic/
├── README.md
├── EVAL_SPEC.md
├── docker-compose.yml
├── .env.example
├── airflow/
│   ├── dags/                 # synthetic DAGs (oom, spot, schema, timeout, deps)
│   └── plugins/failure_hooks.py
├── agent/
│   ├── main.py               # webhook receiver + agent loop
│   ├── prompts/              # versioned system prompts (v0.md, v1.md ...)
│   ├── diagnosis.py          # output schema (pydantic)
│   └── slack.py              # message formatting + approval handlers
├── mcp_servers/
│   ├── airflow_server.py     # get_task_logs, get_dag_metadata
│   ├── metrics_server.py     # get_metrics (mock Grafana)
│   └── rbac.py               # role -> allowed tools policy
├── evals/
│   ├── cases/                # *.jsonl labeled failures
│   ├── generate_logs.py      # builds realistic synthetic logs per case
│   ├── scorers.py            # category match + LLM-judged root cause
│   ├── run.py
│   └── results/
├── scripts/
│   └── break_dag.py
├── deploy/                   # optional: k8s manifests, terraform (later)
└── dashboard/                # optional: eval report UI (later)
```

## Roadmap

- [x] Failure simulator
- [ ] Baseline agent + evals
- [ ] MCP tools + RBAC
- [ ] Slack approval flow
- [ ] K8s manifests
- [ ] Eval dashboard

## Disclaimer

All data is synthetic. This project is not affiliated with or derived from any employer's code or systems.