package runtime

import (
	"errors"

	"github.com/Vaibhavee89/agentflight/internal/policy"
)

// Typed errors returned by Runtime.Run.
var (
	// ErrMaxSteps indicates a run reached its configured step limit without finishing.
	ErrMaxSteps = errors.New("runtime: max steps exceeded")
	// ErrToolTimeout indicates a tool call did not complete within its allotted time.
	// It is an alias for policy.ErrToolTimeout, which is where per-tool timeouts
	// are actually enforced.
	ErrToolTimeout = policy.ErrToolTimeout
	// ErrPolicyDenied indicates a policy check rejected an action before it ran.
	// It is an alias for policy.ErrPolicyDenied, which is where tool-call gating
	// is actually enforced.
	ErrPolicyDenied = policy.ErrPolicyDenied
	// ErrReplayMismatch indicates a replayed run diverged from its recorded trace.
	ErrReplayMismatch = errors.New("runtime: replay mismatch")
	// ErrCancelled indicates the run's context was cancelled or timed out.
	ErrCancelled = errors.New("runtime: run cancelled")
	// ErrNoCheckpoint indicates Resume was called for a run with no saved
	// checkpoint, or with no checkpoint store configured.
	ErrNoCheckpoint = errors.New("runtime: no checkpoint found")
)
