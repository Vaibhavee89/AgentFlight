package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/provider"
)

// loadedSuite is a benchmark suite's tasks, indexed and paired with their
// scripted responses, ready to drive a runtime deterministically offline.
type loadedSuite struct {
	tasks    []benchmark.Task
	taskByID map[string]benchmark.Task
	byPrompt map[string][]provider.ModelResponse
	maxSteps int
}

// loadSuite loads suitePath's tasks and its sibling *.mock.json script.
func loadSuite(suitePath string) (loadedSuite, error) {
	tasks, err := benchmark.LoadTasks(suitePath)
	if err != nil {
		return loadedSuite{}, fmt.Errorf("load suite: %w", err)
	}

	mockPath := strings.TrimSuffix(suitePath, filepath.Ext(suitePath)) + ".mock.json"
	scripts, err := benchmark.LoadMockScripts(mockPath)
	if err != nil {
		return loadedSuite{}, fmt.Errorf("load mock scripts %s: %w", mockPath, err)
	}

	byPrompt := make(map[string][]provider.ModelResponse, len(tasks))
	taskByID := make(map[string]benchmark.Task, len(tasks))
	maxSteps := 10
	for _, task := range tasks {
		script := scripts[task.ID]
		byPrompt[task.Prompt] = script
		taskByID[task.ID] = task
		if len(script) > maxSteps {
			maxSteps = len(script)
		}
	}

	return loadedSuite{tasks: tasks, taskByID: taskByID, byPrompt: byPrompt, maxSteps: maxSteps}, nil
}

// toolCallCount returns how many tool calls a scripted response sequence
// makes in total, across every step.
func toolCallCount(script []provider.ModelResponse) int {
	n := 0
	for _, resp := range script {
		n += len(resp.ToolCalls)
	}
	return n
}
