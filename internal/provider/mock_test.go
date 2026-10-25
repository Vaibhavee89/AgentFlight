package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

var errBoom = errors.New("boom")

func TestMockProvider_Complete(t *testing.T) {
	tests := []struct {
		name      string
		responses []ModelResponse
		errs      []error
		calls     int
		wantErr   error
	}{
		{
			name:      "returns scripted responses in order",
			responses: []ModelResponse{{Content: "a"}, {Content: "b"}},
			calls:     2,
		},
		{
			name:      "returns scripted error at index",
			responses: []ModelResponse{{Content: "a"}, {Content: "b"}},
			errs:      []error{nil, errBoom},
			calls:     2,
			wantErr:   errBoom,
		},
		{
			name:      "exhausted script returns typed error",
			responses: []ModelResponse{{Content: "a"}},
			calls:     2,
			wantErr:   ErrScriptExhausted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewMockProvider(tt.responses, tt.errs)
			var lastErr error
			var lastResp ModelResponse
			for i := 0; i < tt.calls; i++ {
				lastResp, lastErr = p.Complete(context.Background())
			}
			if tt.wantErr != nil {
				if !errors.Is(lastErr, tt.wantErr) {
					t.Fatalf("Complete() error = %v, want %v", lastErr, tt.wantErr)
				}
				return
			}
			if lastErr != nil {
				t.Fatalf("Complete() unexpected error: %v", lastErr)
			}
			want := tt.responses[tt.calls-1]
			if lastResp != want {
				t.Fatalf("Complete() = %+v, want %+v", lastResp, want)
			}
		})
	}
}

func TestMockProvider_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewMockProvider([]ModelResponse{{Content: "a"}}, nil)
	if _, err := p.Complete(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete() error = %v, want %v", err, context.Canceled)
	}
}

func TestMockProvider_ConcurrentUse(t *testing.T) {
	n := 50
	responses := make([]ModelResponse, n)
	for i := range responses {
		responses[i] = ModelResponse{Content: fmt.Sprintf("resp-%d", i)}
	}
	p := NewMockProvider(responses, nil)

	seen := make([]bool, n)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := p.Complete(context.Background())
			if err != nil {
				t.Errorf("Complete() unexpected error: %v", err)
				return
			}
			idx := -1
			for j, r := range responses {
				if r.Content == resp.Content {
					idx = j
					break
				}
			}
			if idx == -1 {
				t.Errorf("Complete() returned unexpected response %+v", resp)
				return
			}
			mu.Lock()
			seen[idx] = true
			mu.Unlock()
		}()
	}
	wg.Wait()

	for i, ok := range seen {
		if !ok {
			t.Errorf("response %d was never returned", i)
		}
	}

	if _, err := p.Complete(context.Background()); !errors.Is(err, ErrScriptExhausted) {
		t.Fatalf("Complete() after exhaustion error = %v, want %v", err, ErrScriptExhausted)
	}
}
