package replay

import (
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

// MismatchError reports where a replay first diverged from the recording.
// Seq is the sequence number of the recorded event that disagreed, or 0 if
// the divergence cannot be tied to a single event. It unwraps to
// runtime.ErrReplayMismatch, so errors.Is(err, runtime.ErrReplayMismatch)
// holds.
type MismatchError struct {
	Seq    int64
	Detail string
}

func (e *MismatchError) Error() string {
	if e.Seq == 0 {
		return fmt.Sprintf("replay: mismatch: %s", e.Detail)
	}
	return fmt.Sprintf("replay: mismatch at seq %d: %s", e.Seq, e.Detail)
}

func (e *MismatchError) Unwrap() error {
	return runtime.ErrReplayMismatch
}
