package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
)

// CompareSchemaVersion identifies the layout of CompareDocument. It changes
// only for incompatible edits; readers should check it before decoding.
const CompareSchemaVersion = 1

// CompareDocument is the machine-readable form of a `harnessbench compare`
// run, written by --json and served by the API as /bench/compare.
type CompareDocument struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   string            `json:"generated_at"`
	GitCommit     string            `json:"git_commit"`
	Seed          int64             `json:"seed"`
	Suite         string            `json:"suite"`
	Repeats       int               `json:"repeats"`
	FaultRates    []float64         `json:"fault_rates"`
	Rows          []RuntimeFaultRow `json:"rows"`
	KillScenario  []KillRow         `json:"kill_scenario"`
}

// RuntimeFaultRow is one runtime at one fault rate. Latency is the p95 of
// run durations in milliseconds; see docs/results.md for what it measures.
type RuntimeFaultRow struct {
	Runtime            string  `json:"runtime"`
	FaultRate          float64 `json:"fault_rate"`
	Executions         int     `json:"executions"`
	SuccessRate        float64 `json:"success_rate"`
	MeanSteps          float64 `json:"mean_steps"`
	P95LatencyMS       float64 `json:"p95_latency_ms"`
	DuplicateToolExecs int     `json:"duplicate_tool_execs"`
	RunsWithFinalState int     `json:"runs_with_final_state"`
}

// KillRow is one runtime's mid-run kill-and-recover results.
type KillRow struct {
	Runtime            string  `json:"runtime"`
	Attempts           int     `json:"attempts"`
	Recovered          int     `json:"recovered"`
	RecoveryRate       float64 `json:"recovery_rate"`
	TotalToolExecs     int     `json:"total_tool_execs"`
	ExpectedToolExecs  int     `json:"expected_tool_execs"`
	DuplicateToolExecs int     `json:"duplicate_tool_execs"`
}

// WriteCompareDocument writes doc to path as indented JSON.
func WriteCompareDocument(path string, doc CompareDocument) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("benchmark: encode compare document: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("benchmark: write %s: %w", path, err)
	}
	return nil
}

// ReadCompareDocument decodes a compare document from path and rejects any
// schema version other than CompareSchemaVersion.
func ReadCompareDocument(path string) (CompareDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CompareDocument{}, fmt.Errorf("benchmark: read %s: %w", path, err)
	}
	var doc CompareDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return CompareDocument{}, fmt.Errorf("benchmark: decode %s: %w", path, err)
	}
	if doc.SchemaVersion != CompareSchemaVersion {
		return CompareDocument{}, fmt.Errorf("benchmark: %s has schema_version %d, want %d", path, doc.SchemaVersion, CompareSchemaVersion)
	}
	return doc, nil
}
