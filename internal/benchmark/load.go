package benchmark

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Vaibhavee89/agentflight/internal/provider"
)

// ErrMalformedTask indicates a JSONL line could not be decoded as a Task.
var ErrMalformedTask = errors.New("benchmark: malformed task")

// LoadTasks reads the JSONL file at path, one Task per non-blank line.
func LoadTasks(path string) ([]Task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("benchmark: open %s: %w", path, err)
	}
	defer f.Close()

	tasks, err := LoadTasksFromReader(f)
	if err != nil {
		return nil, fmt.Errorf("benchmark: %s: %w", path, err)
	}
	return tasks, nil
}

// LoadTasksFromReader reads Tasks from r in JSONL format, one Task per
// non-blank line. It returns an error wrapping ErrMalformedTask, naming the
// offending line number, on the first line that fails to decode.
func LoadTasksFromReader(r io.Reader) ([]Task, error) {
	var tasks []Task
	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var task Task
		if err := json.Unmarshal([]byte(line), &task); err != nil {
			return nil, fmt.Errorf("line %d: %w: %v", lineNum, ErrMalformedTask, err)
		}
		tasks = append(tasks, task)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return tasks, nil
}

// LoadMockScripts reads a JSON file at path mapping task ID to the scripted
// provider.ModelResponse sequence for that task, as used by
// benchmarks/basic.mock.json to run a suite offline and deterministically.
func LoadMockScripts(path string) (map[string][]provider.ModelResponse, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("benchmark: open %s: %w", path, err)
	}

	var scripts map[string][]provider.ModelResponse
	if err := json.Unmarshal(data, &scripts); err != nil {
		return nil, fmt.Errorf("benchmark: decode %s: %w", path, err)
	}
	return scripts, nil
}
