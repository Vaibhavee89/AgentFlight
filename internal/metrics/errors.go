package metrics

import (
	"errors"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// Error classes, stable names used to key ErrorCounts. They are determined
// via errors.Is/As against this project's typed errors, never by matching
// error message strings.
const (
	ErrorClassMaxSteps       = "max_steps"
	ErrorClassCancelled      = "cancelled"
	ErrorClassPolicyDenied   = "policy_denied"
	ErrorClassToolTimeout    = "tool_timeout"
	ErrorClassToolNotFound   = "tool_not_found"
	ErrorClassReplayMismatch = "replay_mismatch"
	ErrorClassNoCheckpoint   = "no_checkpoint"
	// ErrorClassGradeMismatch marks an execution that ran without error but
	// did not satisfy its task's grader.
	ErrorClassGradeMismatch = "grade_mismatch"
	// ErrorClassOther is any error not recognized as one of the classes above.
	ErrorClassOther = "other"
)

// ClassifyError maps err to a stable error class using errors.Is/As against
// this project's typed errors, falling back to ErrorClassOther for any
// unrecognized error and "" for a nil error.
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}

	switch {
	case errors.Is(err, runtime.ErrMaxSteps):
		return ErrorClassMaxSteps
	case errors.Is(err, runtime.ErrCancelled):
		return ErrorClassCancelled
	case errors.Is(err, policy.ErrPolicyDenied):
		return ErrorClassPolicyDenied
	case errors.Is(err, policy.ErrToolTimeout):
		return ErrorClassToolTimeout
	case errors.Is(err, runtime.ErrReplayMismatch):
		return ErrorClassReplayMismatch
	case errors.Is(err, runtime.ErrNoCheckpoint):
		return ErrorClassNoCheckpoint
	}

	var notFound *tools.ErrToolNotFound
	if errors.As(err, &notFound) {
		return ErrorClassToolNotFound
	}

	return ErrorClassOther
}
