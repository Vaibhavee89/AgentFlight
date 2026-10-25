package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Registry holds a set of tools addressable by name. It is safe for
// concurrent use.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds t to the registry under its spec name. It returns an error
// wrapping ErrDuplicateTool if a tool with the same name is already
// registered.
func (r *Registry) Register(t Tool) error {
	name := t.Spec().Name

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("register tool %q: %w", name, ErrDuplicateTool)
	}
	r.tools[name] = t
	return nil
}

// Get returns the tool registered under name, if any.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	return t, ok
}

// Specs returns the specs of all registered tools, sorted by name.
func (r *Registry) Specs() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	specs := make([]ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, t.Spec())
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// Execute runs the tool named in call. It returns an error wrapping
// ErrToolNotFound if no tool is registered under that name.
func (r *Registry) Execute(ctx context.Context, call ToolCall) (ToolOutput, error) {
	t, ok := r.Get(call.Name)
	if !ok {
		return ToolOutput{}, &ErrToolNotFound{Name: call.Name}
	}
	return t.Execute(ctx, call)
}
