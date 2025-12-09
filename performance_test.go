package fluxo

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestStepOverheadUnder1ms verifies the non-functional performance requirement
// that the engine overhead per step (excluding user logic) is < 1ms.
//
// We construct a workflow with many sequential no-op steps to amortize timer
// granularity and incidental overhead, then measure average duration per step.
func TestStepOverheadUnder1ms(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eng := NewInMemoryEngine()

	// No-op step to minimize user logic time.
	noop := func(ctx context.Context, input any) (any, error) { return input, nil }

	const N = 1000 // enough steps to get a stable average without taking long

	flow := New("perf-step-overhead")
	for i := 0; i < N; i++ {
		flow = flow.Step(fmt.Sprintf("s%04d", i), noop)
	}

	require.NoError(t, flow.Register(eng))

	// Warm-up run to avoid measuring one-time costs (e.g., allocations, caches).
	_, err := Run(ctx, eng, flow.Name(), nil)
	require.NoError(t, err)

	start := time.Now()
	_, err = Run(ctx, eng, flow.Name(), nil)
	require.NoError(t, err)
	total := time.Since(start)

	avgPerStep := total / N
	if avgPerStep >= time.Millisecond {
		t.Fatalf("average engine overhead per step too high: %v (total %v for %d steps)", avgPerStep, total, N)
	}
}

// TestMinimalMemoryFootprintUnder5MB verifies the non-functional requirement
// that a minimal in-memory configuration stays under ~5MB of heap usage.
//
// We force a GC, capture HeapAlloc, create an in-memory engine, force another
// GC and compare HeapAlloc again. This provides a conservative estimate of
// retained heap usage attributable to engine initialization.
func TestMinimalMemoryFootprintUnder5MB(t *testing.T) {
	t.Parallel()

	// Help the GC by minimizing noise from other goroutines.
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	eng := NewInMemoryEngine()
	// Keep eng alive until after measurement.
	runtime.KeepAlive(eng)

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	const fiveMB = 5 * 1024 * 1024
	used := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if used < 0 {
		used = 0 // be robust to minor fluctuations
	}

	if used >= fiveMB {
		t.Fatalf("minimal memory footprint too high: %d bytes (>= %d)", used, fiveMB)
	}
}
