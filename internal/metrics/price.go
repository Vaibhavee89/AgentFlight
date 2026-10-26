package metrics

import (
	"encoding/json"
	"fmt"
	"os"
)

// Price is the cost per million tokens for one model.
type Price struct {
	InputPerMillion  float64 `json:"input_per_million"`
	OutputPerMillion float64 `json:"output_per_million"`
}

// PriceTable maps a model name to its Price. The empty string "" is the
// fallback entry used for executions with no model set or no matching
// entry; a PriceTable with no matching or fallback entry costs nothing.
type PriceTable map[string]Price

// Cost estimates the dollar cost of inputTokens/outputTokens for model,
// falling back to the "" entry if model has none, and to zero if neither
// is present.
func (t PriceTable) Cost(model string, inputTokens, outputTokens int) float64 {
	price, ok := t[model]
	if !ok {
		price, ok = t[""]
		if !ok {
			return 0
		}
	}
	return float64(inputTokens)/1_000_000*price.InputPerMillion +
		float64(outputTokens)/1_000_000*price.OutputPerMillion
}

// LoadPriceTable reads a PriceTable from a JSON file mapping model name (or
// "" for the default) to its Price.
func LoadPriceTable(path string) (PriceTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("metrics: open %s: %w", path, err)
	}
	var table PriceTable
	if err := json.Unmarshal(data, &table); err != nil {
		return nil, fmt.Errorf("metrics: decode %s: %w", path, err)
	}
	return table, nil
}
