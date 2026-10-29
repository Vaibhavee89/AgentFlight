import type { CompareDocument } from "../api/types";

/** A compact comparison document shaped exactly as the Go server writes it. */
export function compareFixture(overrides: Partial<CompareDocument> = {}): CompareDocument {
  return {
    schema_version: 1,
    generated_at: "2026-10-29T10:00:00Z",
    git_commit: "abcdef1234567890",
    seed: 1,
    suite: "benchmarks/basic.jsonl",
    repeats: 10,
    fault_rates: [0, 0.2],
    rows: [
      {
        runtime: "agentflight",
        fault_rate: 0,
        executions: 120,
        success_rate: 1,
        mean_steps: 2.25,
        p95_latency_ms: 0.8,
        duplicate_tool_execs: 0,
        runs_with_final_state: 120,
      },
      {
        runtime: "baseline",
        fault_rate: 0,
        executions: 120,
        success_rate: 0.75,
        mean_steps: 2.08,
        p95_latency_ms: 0,
        duplicate_tool_execs: 0,
        runs_with_final_state: 0,
      },
      {
        runtime: "agentflight",
        fault_rate: 0.2,
        executions: 120,
        success_rate: 0.583,
        mean_steps: 1.3,
        p95_latency_ms: 44.6,
        duplicate_tool_execs: 24,
        runs_with_final_state: 120,
      },
      {
        runtime: "baseline",
        fault_rate: 0.2,
        executions: 120,
        success_rate: 0.408,
        mean_steps: 1.07,
        p95_latency_ms: 400.7,
        duplicate_tool_execs: 0,
        runs_with_final_state: 0,
      },
    ],
    kill_scenario: [
      {
        runtime: "agentflight",
        attempts: 30,
        recovered: 30,
        recovery_rate: 1,
        total_tool_execs: 60,
        expected_tool_execs: 60,
        duplicate_tool_execs: 0,
      },
      {
        runtime: "baseline",
        attempts: 30,
        recovered: 20,
        recovery_rate: 0.667,
        total_tool_execs: 90,
        expected_tool_execs: 60,
        duplicate_tool_execs: 30,
      },
    ],
    ...overrides,
  };
}
