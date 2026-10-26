package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// outputResult is one task execution's full record, written to results.json.
type outputResult struct {
	TaskID       string `json:"task_id"`
	Repeat       int    `json:"repeat"`
	RunID        string `json:"run_id"`
	Content      string `json:"content"`
	Steps        int    `json:"steps"`
	Status       string `json:"status"`
	Retries      int    `json:"retries"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Pass         bool   `json:"pass"`
	Reason       string `json:"reason,omitempty"`
	Error        string `json:"error,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
}

// writeResults writes the full per-execution results as an indented JSON
// array to path, in the same (task, repeat) order Runner.Run returned.
func writeResults(path string, results []gradedResult) error {
	out := make([]outputResult, len(results))
	for i, gr := range results {
		r := gr.result
		o := outputResult{
			TaskID:       r.TaskID,
			Repeat:       r.Repeat,
			RunID:        r.RunID,
			Content:      r.Result.Content,
			Steps:        r.Result.Steps,
			Status:       r.Result.Status,
			Retries:      r.Result.Retries,
			InputTokens:  r.Result.Usage.InputTokens,
			OutputTokens: r.Result.Usage.OutputTokens,
			Pass:         gr.grade.Pass,
			Reason:       gr.grade.Reason,
			DurationMS:   r.Duration.Milliseconds(),
		}
		if r.Err != nil {
			o.Error = r.Err.Error()
		}
		out[i] = o
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		f.Close()
		return fmt.Errorf("encode results: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}
