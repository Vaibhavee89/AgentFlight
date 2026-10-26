package metrics

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil error", err: nil, want: ""},
		{name: "max steps", err: fmt.Errorf("run x: %w", runtime.ErrMaxSteps), want: ErrorClassMaxSteps},
		{name: "cancelled", err: fmt.Errorf("run x: %w", runtime.ErrCancelled), want: ErrorClassCancelled},
		{name: "policy denied", err: fmt.Errorf("wrap: %w", policy.ErrPolicyDenied), want: ErrorClassPolicyDenied},
		{name: "tool timeout", err: fmt.Errorf("wrap: %w", policy.ErrToolTimeout), want: ErrorClassToolTimeout},
		{name: "replay mismatch", err: fmt.Errorf("wrap: %w", runtime.ErrReplayMismatch), want: ErrorClassReplayMismatch},
		{name: "no checkpoint", err: fmt.Errorf("wrap: %w", runtime.ErrNoCheckpoint), want: ErrorClassNoCheckpoint},
		{name: "tool not found", err: &tools.ErrToolNotFound{Name: "x"}, want: ErrorClassToolNotFound},
		{name: "unrecognized error", err: errors.New("boom"), want: ErrorClassOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != tt.want {
				t.Fatalf("ClassifyError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
