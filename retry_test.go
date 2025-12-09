package fluxo

import (
	"testing"
	"time"
)

// Ensure non-positive maxAttempts is normalized to 1.
func TestRetry_NonPositiveMaxAttemptsDefaultsToOne(t *testing.T) {
	p := Retry(0).Policy()
	if p.MaxAttempts != 1 {
		t.Fatalf("expected MaxAttempts=1 for Retry(0), got %d", p.MaxAttempts)
	}

	p = Retry(-5).Policy()
	if p.MaxAttempts != 1 {
		t.Fatalf("expected MaxAttempts=1 for Retry(-5), got %d", p.MaxAttempts)
	}
}

// Ensure WithExponentialBackoff wires fields correctly and default multiplier is applied.
func TestRetry_WithExponentialBackoff_UsesDefaults(t *testing.T) {
	initial := 100 * time.Millisecond
	max := 2 * time.Second

	// multiplier <= 0 should default to 2.0
	p := Retry(3).
		WithExponentialBackoff(initial, 0, max).
		Policy()

	if p.MaxAttempts != 3 {
		t.Fatalf("expected MaxAttempts=3, got %d", p.MaxAttempts)
	}
	if p.InitialBackoff != initial {
		t.Fatalf("expected InitialBackoff=%v, got %v", initial, p.InitialBackoff)
	}
	if p.MaxBackoff != max {
		t.Fatalf("expected MaxBackoff=%v, got %v", max, p.MaxBackoff)
	}
	if p.BackoffMultiplier != 2.0 {
		t.Fatalf("expected BackoffMultiplier=2.0 (default), got %v", p.BackoffMultiplier)
	}
	if p.Backoff != 0 {
		t.Fatalf("expected deprecated Backoff=0, got %v", p.Backoff)
	}
}

// Ensure WithExponentialBackoff respects an explicit multiplier.
func TestRetry_WithExponentialBackoff_ExplicitMultiplier(t *testing.T) {
	initial := 50 * time.Millisecond
	max := 500 * time.Millisecond
	mult := 3.0

	p := Retry(4).
		WithExponentialBackoff(initial, mult, max).
		Policy()

	if p.InitialBackoff != initial {
		t.Fatalf("expected InitialBackoff=%v, got %v", initial, p.InitialBackoff)
	}
	if p.MaxBackoff != max {
		t.Fatalf("expected MaxBackoff=%v, got %v", max, p.MaxBackoff)
	}
	if p.BackoffMultiplier != mult {
		t.Fatalf("expected BackoffMultiplier=%v, got %v", mult, p.BackoffMultiplier)
	}
}

// Ensure WithConstantBackoff sets a fixed delay and uses multiplier 1.0.
func TestRetry_WithConstantBackoff(t *testing.T) {
	delay := 250 * time.Millisecond

	p := Retry(5).
		WithConstantBackoff(delay).
		Policy()

	if p.MaxAttempts != 5 {
		t.Fatalf("expected MaxAttempts=5, got %d", p.MaxAttempts)
	}
	if p.InitialBackoff != delay {
		t.Fatalf("expected InitialBackoff=%v, got %v", delay, p.InitialBackoff)
	}
	if p.MaxBackoff != 0 {
		t.Fatalf("expected MaxBackoff=0 for constant backoff, got %v", p.MaxBackoff)
	}
	if p.BackoffMultiplier != 1.0 {
		t.Fatalf("expected BackoffMultiplier=1.0, got %v", p.BackoffMultiplier)
	}
	if p.Backoff != 0 {
		t.Fatalf("expected deprecated Backoff=0, got %v", p.Backoff)
	}
}

// Ensure Immediate clears all backoff-related timing without changing MaxAttempts.
func TestRetry_ImmediateClearsBackoff(t *testing.T) {
	p := Retry(7).
		WithExponentialBackoff(100*time.Millisecond, 2.0, 5*time.Second).
		Immediate().
		Policy()

	if p.MaxAttempts != 7 {
		t.Fatalf("expected MaxAttempts=7, got %d", p.MaxAttempts)
	}
	if p.InitialBackoff != 0 {
		t.Fatalf("expected InitialBackoff=0 after Immediate, got %v", p.InitialBackoff)
	}
	if p.MaxBackoff != 0 {
		t.Fatalf("expected MaxBackoff=0 after Immediate, got %v", p.MaxBackoff)
	}
	if p.BackoffMultiplier != 0 {
		t.Fatalf("expected BackoffMultiplier=0 after Immediate, got %v", p.BackoffMultiplier)
	}
	if p.Backoff != 0 {
		t.Fatalf("expected deprecated Backoff=0 after Immediate, got %v", p.Backoff)
	}
}
