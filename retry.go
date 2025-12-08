package fluxo

import "time"

// RetryBuilder provides a fluent way to construct RetryPolicy values
// for use with FlowBuilder.StepWithRetry.
type RetryBuilder struct {
	policy RetryPolicy
}

// Retry creates a RetryBuilder with the given maxAttempts.
//
// maxAttempts <= 0 is treated as 1 (no retries).
func Retry(maxAttempts int) RetryBuilder {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	return RetryBuilder{
		policy: RetryPolicy{
			MaxAttempts: maxAttempts,
		},
	}
}

// WithExponentialBackoff configures exponential backoff:
//
//   - initial is the delay before the first retry.
//   - multiplier > 1 grows the delay each attempt (default 2.0 if <= 0).
//   - max caps the delay; if <= 0, there is no cap.
//
// Example:
//
//	Retry(3).WithExponentialBackoff(100*time.Millisecond, 2.0, 2*time.Second)
func (r RetryBuilder) WithExponentialBackoff(initial time.Duration, multiplier float64, max time.Duration) RetryBuilder {
	p := r.policy
	p.InitialBackoff = initial
	p.MaxBackoff = max
	if multiplier <= 0 {
		multiplier = 2.0
	}
	p.BackoffMultiplier = multiplier
	// Deprecated Backoff field is left zero on purpose.
	return RetryBuilder{policy: p}
}

// WithConstantBackoff configures a constant backoff between retries.
//
// This is equivalent to an exponential backoff with multiplier 1.0 and
// no max cap.
func (r RetryBuilder) WithConstantBackoff(delay time.Duration) RetryBuilder {
	p := r.policy
	p.InitialBackoff = delay
	p.MaxBackoff = 0
	p.BackoffMultiplier = 1.0
	return RetryBuilder{policy: p}
}

// Immediate disables any sleep between retries.
// Retries will still respect MaxAttempts.
func (r RetryBuilder) Immediate() RetryBuilder {
	p := r.policy
	p.InitialBackoff = 0
	p.MaxBackoff = 0
	p.BackoffMultiplier = 0
	p.Backoff = 0
	return RetryBuilder{policy: p}
}

// Policy returns the underlying RetryPolicy to be passed to
// FlowBuilder.StepWithRetry.
func (r RetryBuilder) Policy() RetryPolicy {
	return r.policy
}
