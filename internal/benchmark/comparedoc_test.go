package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareDocument_ReadWrite(t *testing.T) {
	tests := []struct {
		name    string
		content string // raw file content; empty means write doc first
		wantErr string
	}{
		{name: "round trip", content: ""},
		{name: "unknown schema version rejected", content: `{"schema_version": 99}`, wantErr: "schema_version 99"},
		{name: "malformed json rejected", content: `{not json`, wantErr: "decode"},
	}

	doc := CompareDocument{
		SchemaVersion: CompareSchemaVersion,
		Suite:         "benchmarks/basic.jsonl",
		Repeats:       2,
		FaultRates:    []float64{0, 0.1},
		Rows:          []RuntimeFaultRow{{Runtime: "agentflight", FaultRate: 0.1, Executions: 4, SuccessRate: 0.75}},
		KillScenario:  []KillRow{{Runtime: "baseline", Attempts: 2, Recovered: 1, RecoveryRate: 0.5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "compare.json")
			if tt.content == "" {
				if err := WriteCompareDocument(path, doc); err != nil {
					t.Fatalf("WriteCompareDocument: %v", err)
				}
			} else if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			got, err := ReadCompareDocument(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadCompareDocument() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadCompareDocument: %v", err)
			}
			if got.Rows[0].SuccessRate != 0.75 || got.KillScenario[0].RecoveryRate != 0.5 || len(got.FaultRates) != 2 {
				t.Fatalf("round trip lost data: %+v", got)
			}
		})
	}
}

func TestCompareDocument_MissingFile(t *testing.T) {
	if _, err := ReadCompareDocument(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatalf("ReadCompareDocument on a missing file returned nil error")
	}
}
