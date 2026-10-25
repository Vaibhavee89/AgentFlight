package benchmark

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadTasksFromReader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Task
		wantErr error
	}{
		{
			name:  "single valid task",
			input: `{"id":"t1","prompt":"hi","tools":["echo"],"grader":{"type":"exact_match","want":"hi"},"timeout":"5s","tags":["easy"]}` + "\n",
			want: []Task{
				{ID: "t1", Prompt: "hi", Tools: []string{"echo"}, Grader: GraderSpec{Type: "exact_match", Want: "hi"}, Timeout: 5 * time.Second, Tags: []string{"easy"}},
			},
		},
		{
			name: "multiple tasks and blank lines are skipped",
			input: `{"id":"t1","prompt":"a","grader":{"type":"contains","want":"x"}}

{"id":"t2","prompt":"b","grader":{"type":"contains","want":"y"}}
`,
			want: []Task{
				{ID: "t1", Prompt: "a", Grader: GraderSpec{Type: "contains", Want: "x"}},
				{ID: "t2", Prompt: "b", Grader: GraderSpec{Type: "contains", Want: "y"}},
			},
		},
		{
			name:    "malformed json line",
			input:   `{"id":"t1","prompt":` + "\n",
			wantErr: ErrMalformedTask,
		},
		{
			name:    "invalid timeout",
			input:   `{"id":"t1","prompt":"a","grader":{"type":"contains","want":"x"},"timeout":"not-a-duration"}` + "\n",
			wantErr: ErrMalformedTask,
		},
		{
			name:  "empty input yields no tasks",
			input: "",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadTasksFromReader(strings.NewReader(tt.input))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("LoadTasksFromReader() error = %v, want wrapping %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadTasksFromReader() unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("LoadTasksFromReader() = %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if !reflect.DeepEqual(got[i], tt.want[i]) {
					t.Fatalf("task[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLoadTasks_MissingFile(t *testing.T) {
	_, err := LoadTasks(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if err == nil {
		t.Fatalf("LoadTasks() expected error for missing file, got nil")
	}
}

func TestLoadTasks_BasicFixture(t *testing.T) {
	tasks, err := LoadTasks("../../benchmarks/basic.jsonl")
	if err != nil {
		t.Fatalf("LoadTasks() error: %v", err)
	}
	if len(tasks) != 12 {
		t.Fatalf("LoadTasks() loaded %d tasks, want 12", len(tasks))
	}

	seenIDs := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task.ID == "" {
			t.Fatalf("task has empty ID: %+v", task)
		}
		if seenIDs[task.ID] {
			t.Fatalf("duplicate task ID %q", task.ID)
		}
		seenIDs[task.ID] = true

		if task.Prompt == "" {
			t.Fatalf("task %q has empty Prompt", task.ID)
		}
		for _, tool := range task.Tools {
			if tool != "echo" && tool != "calculator" {
				t.Fatalf("task %q uses unexpected tool %q, want only echo/calculator", task.ID, tool)
			}
		}
		if _, err := GraderFor(task.Grader); err != nil {
			t.Fatalf("task %q has invalid grader: %v", task.ID, err)
		}
		if task.Timeout <= 0 {
			t.Fatalf("task %q has non-positive timeout %v", task.ID, task.Timeout)
		}
	}
}

func TestLoadMockScripts_BasicFixture(t *testing.T) {
	scripts, err := LoadMockScripts("../../benchmarks/basic.mock.json")
	if err != nil {
		t.Fatalf("LoadMockScripts() error: %v", err)
	}

	tasks, err := LoadTasks("../../benchmarks/basic.jsonl")
	if err != nil {
		t.Fatalf("LoadTasks() error: %v", err)
	}

	for _, task := range tasks {
		script, ok := scripts[task.ID]
		if !ok {
			t.Fatalf("no mock script found for task %q", task.ID)
		}
		if len(script) == 0 {
			t.Fatalf("mock script for task %q is empty", task.ID)
		}
		last := script[len(script)-1]
		if len(last.ToolCalls) != 0 {
			t.Fatalf("mock script for task %q does not end with a final content-only response: %+v", task.ID, last)
		}
	}
}

func TestLoadMockScripts_MissingFile(t *testing.T) {
	_, err := LoadMockScripts(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatalf("LoadMockScripts() expected error for missing file, got nil")
	}
}

func TestLoadMockScripts_Malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"task-01": not-json}`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := LoadMockScripts(path)
	if err == nil {
		t.Fatalf("LoadMockScripts() expected error for malformed json, got nil")
	}
}
