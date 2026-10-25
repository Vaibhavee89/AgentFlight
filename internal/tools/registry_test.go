package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type stubTool struct {
	name   string
	output ToolOutput
	err    error
}

func (s stubTool) Spec() ToolSpec { return ToolSpec{Name: s.name, Description: "stub"} }

func (s stubTool) Execute(ctx context.Context, call ToolCall) (ToolOutput, error) {
	return s.output, s.err
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	tests := []struct {
		name    string
		tools   []Tool
		wantErr bool
	}{
		{
			name:  "registers unique tools",
			tools: []Tool{stubTool{name: "a"}, stubTool{name: "b"}},
		},
		{
			name:    "rejects duplicate name",
			tools:   []Tool{stubTool{name: "a"}, stubTool{name: "a"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			var err error
			for _, tool := range tt.tools {
				if e := r.Register(tool); e != nil {
					err = e
				}
			}
			if tt.wantErr {
				if err == nil || !errors.Is(err, ErrDuplicateTool) {
					t.Fatalf("Register() error = %v, want wrapping %v", err, ErrDuplicateTool)
				}
				return
			}
			if err != nil {
				t.Fatalf("Register() unexpected error: %v", err)
			}
			for _, tool := range tt.tools {
				got, ok := r.Get(tool.Spec().Name)
				if !ok {
					t.Fatalf("Get(%q) not found", tool.Spec().Name)
				}
				if got.Spec().Name != tool.Spec().Name {
					t.Fatalf("Get(%q) = %+v, want %+v", tool.Spec().Name, got, tool)
				}
			}
		})
	}
}

func TestRegistry_Get_Missing(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get("missing"); ok {
		t.Fatalf("Get(%q) ok = true, want false", "missing")
	}
}

func TestRegistry_Specs_Sorted(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		if err := r.Register(stubTool{name: name}); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	specs := r.Specs()
	want := []string{"alpha", "bravo", "charlie"}
	if len(specs) != len(want) {
		t.Fatalf("Specs() returned %d entries, want %d", len(specs), len(want))
	}
	for i, s := range specs {
		if s.Name != want[i] {
			t.Fatalf("Specs()[%d].Name = %q, want %q", i, s.Name, want[i])
		}
	}
}

func TestRegistry_Execute(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stubTool{name: "ok", output: ToolOutput{Result: "done"}}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	t.Run("executes registered tool", func(t *testing.T) {
		out, err := r.Execute(context.Background(), ToolCall{Name: "ok"})
		if err != nil {
			t.Fatalf("Execute() unexpected error: %v", err)
		}
		if out.Result != "done" {
			t.Fatalf("Execute() = %+v, want Result %q", out, "done")
		}
	})

	t.Run("unknown tool returns typed error", func(t *testing.T) {
		_, err := r.Execute(context.Background(), ToolCall{Name: "missing"})
		var notFound *ErrToolNotFound
		if !errors.As(err, &notFound) {
			t.Fatalf("Execute() error = %v, want *ErrToolNotFound", err)
		}
		if notFound.Name != "missing" {
			t.Fatalf("ErrToolNotFound.Name = %q, want %q", notFound.Name, "missing")
		}
	})
}

func TestRegistry_ConcurrentRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	n := 100
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("tool-%d", i)
			if err := r.Register(stubTool{name: name}); err != nil {
				t.Errorf("Register(%q): %v", name, err)
			}
		}(i)
	}
	wg.Wait()

	var wg2 sync.WaitGroup
	for i := 0; i < n; i++ {
		wg2.Add(1)
		go func(i int) {
			defer wg2.Done()
			name := fmt.Sprintf("tool-%d", i)
			if _, ok := r.Get(name); !ok {
				t.Errorf("Get(%q) not found after concurrent register", name)
			}
		}(i)
	}
	wg2.Wait()

	if got := len(r.Specs()); got != n {
		t.Fatalf("Specs() returned %d entries, want %d", got, n)
	}
}
