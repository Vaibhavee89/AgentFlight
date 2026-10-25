package runtime

import "errors"

// Typed errors returned by Runtime.Run.
var (
	// ErrMaxSteps indicates a run reached its configured step limit without finishing.
	ErrMaxSteps = errors.New("runtime: max steps exceeded")
	// ErrToolTimeout indicates a tool call did not complete within its allotted time.
	ErrToolTimeout = errors.New("runtime: tool timed out")
	// ErrPolicyDenied indicates a policy check rejected an action before it ran.
	ErrPolicyDenied = errors.New("runtime: policy denied action")
	// ErrReplayMismatch indicates a replayed run diverged from its recorded trace.
	ErrReplayMismatch = errors.New("runtime: replay mismatch")
	// ErrCancelled indicates the run's context was cancelled or timed out.
	ErrCancelled = errors.New("runtime: run cancelled")
)
