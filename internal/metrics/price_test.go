package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPriceTable_Cost(t *testing.T) {
	table := PriceTable{
		"gpt-mini": {InputPerMillion: 1.0, OutputPerMillion: 2.0},
		"":         {InputPerMillion: 0.5, OutputPerMillion: 1.0},
	}

	tests := []struct {
		name         string
		model        string
		inputTokens  int
		outputTokens int
		want         float64
	}{
		{name: "known model", model: "gpt-mini", inputTokens: 1_000_000, outputTokens: 1_000_000, want: 3.0},
		{name: "unknown model falls back to default", model: "unknown", inputTokens: 1_000_000, outputTokens: 1_000_000, want: 1.5},
		{name: "fractional tokens", model: "gpt-mini", inputTokens: 500_000, outputTokens: 0, want: 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := table.Cost(tt.model, tt.inputTokens, tt.outputTokens)
			if got != tt.want {
				t.Fatalf("Cost() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPriceTable_Cost_NoMatchNoDefault(t *testing.T) {
	table := PriceTable{"gpt-mini": {InputPerMillion: 1.0, OutputPerMillion: 2.0}}
	if got := table.Cost("unknown", 1_000_000, 1_000_000); got != 0 {
		t.Fatalf("Cost() = %v, want 0", got)
	}
}

func TestLoadPriceTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prices.json")
	content := `{"gpt-mini": {"input_per_million": 1.5, "output_per_million": 3.0}, "": {"input_per_million": 0.1, "output_per_million": 0.2}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	table, err := LoadPriceTable(path)
	if err != nil {
		t.Fatalf("LoadPriceTable() error: %v", err)
	}
	if table["gpt-mini"].InputPerMillion != 1.5 || table["gpt-mini"].OutputPerMillion != 3.0 {
		t.Fatalf("table[gpt-mini] = %+v, want {1.5 3.0}", table["gpt-mini"])
	}
	if table[""].InputPerMillion != 0.1 {
		t.Fatalf("table[\"\"] = %+v, want InputPerMillion 0.1", table[""])
	}
}

func TestLoadPriceTable_MissingFile(t *testing.T) {
	_, err := LoadPriceTable(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatalf("LoadPriceTable() expected error for missing file, got nil")
	}
}

func TestLoadPriceTable_Malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := LoadPriceTable(path)
	if err == nil {
		t.Fatalf("LoadPriceTable() expected error for malformed json, got nil")
	}
}
