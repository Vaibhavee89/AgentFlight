package policy

import (
	"context"
	"time"
)

// RetryNotice describes a failed attempt that the policy is about to retry.
type RetryNotice struct {
	Tool    string
	Attempt int // the attempt that failed, 1-based
	Backoff time.Duration
	Err     error
}

// RetryObserver receives a notice before each retry's backoff wait.
type RetryObserver func(RetryNotice)

type retryObserverKey struct{}

// WithRetryObserver returns a context that reports retries to fn. The observer
// travels with the call, so one policy can serve many runs without shared state.
func WithRetryObserver(ctx context.Context, fn RetryObserver) context.Context {
	return context.WithValue(ctx, retryObserverKey{}, fn)
}

func retryObserverFrom(ctx context.Context) RetryObserver {
	fn, _ := ctx.Value(retryObserverKey{}).(RetryObserver)
	return fn
}
