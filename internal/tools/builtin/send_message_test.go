package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

type fakeLedger struct {
	mu      sync.Mutex
	seen    map[string]string
	err     error
	applied int
}

func (l *fakeLedger) ApplyEffect(ctx context.Context, key, tool, payload string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return false, l.err
	}
	if l.seen == nil {
		l.seen = make(map[string]string)
	}
	if _, ok := l.seen[key]; ok {
		return false, nil
	}
	l.seen[key] = payload
	l.applied++
	return true, nil
}

func TestSendMessage_Execute(t *testing.T) {
	ledgerErr := errors.New("disk full")
	call := func(key, input string) tools.ToolCall {
		return tools.ToolCall{Name: "send_message", Input: json.RawMessage(input), IdempotencyKey: key}
	}

	tests := []struct {
		name        string
		calls       []tools.ToolCall
		ledgerErr   error
		wantApplied int
		wantResult  string
		wantErr     error
	}{
		{name: "first send", calls: []tools.ToolCall{call("k1", `{"to":"bob","body":"hi"}`)}, wantApplied: 1, wantResult: "message to bob sent"},
		{
			name:        "same key twice sends once and answers the same",
			calls:       []tools.ToolCall{call("k1", `{"to":"bob","body":"hi"}`), call("k1", `{"to":"bob","body":"hi"}`)},
			wantApplied: 1, wantResult: "message to bob sent",
		},
		{
			name:        "different keys send twice",
			calls:       []tools.ToolCall{call("k1", `{"to":"bob","body":"hi"}`), call("k2", `{"to":"bob","body":"hi"}`)},
			wantApplied: 2, wantResult: "message to bob sent",
		},
		{name: "no key refuses to send", calls: []tools.ToolCall{call("", `{"to":"bob","body":"hi"}`)}, wantErr: ErrMissingIdempotencyKey},
		{name: "missing recipient", calls: []tools.ToolCall{call("k1", `{"body":"hi"}`)}, wantErr: ErrInvalidMessage},
		{name: "missing body", calls: []tools.ToolCall{call("k1", `{"to":"bob"}`)}, wantErr: ErrInvalidMessage},
		{name: "ledger failure is returned", calls: []tools.ToolCall{call("k1", `{"to":"bob","body":"hi"}`)}, ledgerErr: ledgerErr, wantErr: ledgerErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledger := &fakeLedger{err: tt.ledgerErr}
			tool := NewSendMessage(ledger)

			var out tools.ToolOutput
			var err error
			for _, c := range tt.calls {
				out, err = tool.Execute(context.Background(), c)
			}

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ledger.applied != tt.wantApplied {
				t.Fatalf("effects applied = %d, want %d", ledger.applied, tt.wantApplied)
			}
			if out.Result != tt.wantResult {
				t.Fatalf("result = %q, want %q", out.Result, tt.wantResult)
			}
		})
	}
}
