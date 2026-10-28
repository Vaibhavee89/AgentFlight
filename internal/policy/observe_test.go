package policy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

type sequenceExec struct {
	errs []error
	n    int
}

func (s *sequenceExec) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	i := s.n
	s.n++
	if i < len(s.errs) {
		return tools.ToolOutput{}, s.errs[i]
	}
	return tools.ToolOutput{Result: "ok"}, nil
}

func TestRetryObserver(t *testing.T) {
	tests := []struct {
		name        string
		errs        []error
		wantNotices []int // attempt numbers reported
	}{
		{name: "no failures, no notices", errs: nil, wantNotices: nil},
		{name: "two transient failures, two notices", errs: []error{ErrToolTimeout, ErrToolTimeout}, wantNotices: []int{1, 2}},
		{name: "terminal failure is not retried, so no notice", errs: []error{errors.New("bad input")}, wantNotices: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDefaultPolicy([]string{"echo"},
				WithMaxAttempts(4),
				WithBackoff(time.Millisecond, 2*time.Millisecond),
				WithSleeper(func(context.Context, time.Duration) error { return nil }),
			)
			var got []int
			ctx := WithRetryObserver(context.Background(), func(n RetryNotice) {
				got = append(got, n.Attempt)
				if n.Tool != "echo" || n.Err == nil || n.Backoff < 0 {
					t.Errorf("bad notice: %+v", n)
				}
			})
			_, _ = p.Execute(ctx, &sequenceExec{errs: tt.errs}, tools.ToolCall{Name: "echo"})
			if len(got) != len(tt.wantNotices) {
				t.Fatalf("notices = %v, want %v", got, tt.wantNotices)
			}
			for i, a := range tt.wantNotices {
				if got[i] != a {
					t.Fatalf("notices = %v, want %v", got, tt.wantNotices)
				}
			}
		})
	}
}
