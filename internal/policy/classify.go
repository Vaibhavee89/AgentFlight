package policy

import (
	"context"
	"errors"
	"net"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// Classify decides whether err represents a transient failure worth
// retrying. Unrecognized errors default to Terminal: only failures known to
// be transient (net.Error timeouts, 429/5xx provider errors, and
// ErrToolTimeout) are classified Retryable.
func Classify(err error) Disposition {
	if err == nil {
		return Terminal
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Terminal
	}
	if errors.Is(err, ErrPolicyDenied) {
		return Terminal
	}

	var notFound *tools.ErrToolNotFound
	if errors.As(err, &notFound) {
		return Terminal
	}

	if errors.Is(err, ErrToolTimeout) {
		return Retryable
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Retryable
	}

	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		if statusErr.StatusCode == 429 || statusErr.StatusCode >= 500 {
			return Retryable
		}
		return Terminal
	}

	return Terminal
}
