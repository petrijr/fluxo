package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fakeEngine is a minimal Engine implementation used to test WaitForAnyChildStep.
// It only implements GetInstance; all other methods panic if called.
type fakeEngine struct {
	instances map[string]*WorkflowInstance
}

func newFakeEngine(children map[string]*WorkflowInstance) *fakeEngine {
	return &fakeEngine{instances: children}
}

func (f *fakeEngine) RegisterWorkflow(def WorkflowDefinition) error {
	panic("RegisterWorkflow should not be called in fakeEngine")
}

func (f *fakeEngine) Run(ctx context.Context, name string, input any) (*WorkflowInstance, error) {
	panic("Run should not be called in fakeEngine")
}

func (f *fakeEngine) GetInstance(ctx context.Context, id string) (*WorkflowInstance, error) {
	inst, ok := f.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance %s not found", id)
	}
	return inst, nil
}

func (f *fakeEngine) ListInstances(ctx context.Context, opts InstanceListOptions) ([]*WorkflowInstance, error) {
	panic("ListInstances should not be called in fakeEngine")
}

func (f *fakeEngine) Resume(ctx context.Context, id string) (*WorkflowInstance, error) {
	panic("Resume should not be called in fakeEngine")
}

func (f *fakeEngine) Signal(ctx context.Context, id string, name string, payload any) (*WorkflowInstance, error) {
	panic("Signal should not be called in fakeEngine")
}

func (f *fakeEngine) RecoverStuckInstances(ctx context.Context) (int, error) {
	panic("RecoverStuckInstances should not be called in fakeEngine")
}

func TestWaitForAnyChildStep_ReturnsFirstCompletedChildID(t *testing.T) {
	// Arrange: three children, only one is completed.
	childIDs := []string{"child-1", "child-2", "child-3"}

	children := map[string]*WorkflowInstance{
		"child-1": {
			ID:     "child-1",
			Status: StatusRunning,
		},
		"child-2": {
			ID:     "child-2",
			Status: StatusCompleted,
			Output: "ParallelResult-from-child-2",
		},
		"child-3": {
			ID:     "child-3",
			Status: StatusPending,
		},
	}

	engine := newFakeEngine(children)
	ctx := WithEngine(context.Background(), engine)

	step := WaitForAnyChildStep(func(input any) []string {
		return childIDs
	}, 500)

	// Act
	output, err := step(ctx, childIDs)

	// Assert
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	gotID, ok := output.(string)
	if !ok {
		t.Fatalf("expected output to be child ID string, got %T (%v)", output, output)
	}

	if gotID != "child-2" {
		t.Fatalf("expected first completed child ID \"child-2\", got %q", gotID)
	}
}

func TestWaitForAnyChildStep_WaitsWhenNoChildCompleted(t *testing.T) {
	// Arrange: all children still running/pending.
	childIDs := []string{"a", "b"}

	children := map[string]*WorkflowInstance{
		"a": {ID: "a", Status: StatusRunning},
		"b": {ID: "b", Status: StatusPending},
	}

	engine := newFakeEngine(children)
	ctx := WithEngine(context.Background(), engine)

	step := WaitForAnyChildStep(func(input any) []string {
		return childIDs
	}, 500)

	// Act
	output, err := step(ctx, childIDs)

	// Assert
	if output != nil {
		t.Fatalf("expected nil output while waiting, got %T (%v)", output, output)
	}

	var waitErr *WaitForAnyChildError
	if !errors.As(err, &waitErr) {
		t.Fatalf("expected WaitForAnyChildError, got %T (%v)", err, err)
	}

	// Check that the error carries the IDs and a positive poll interval.
	if len(waitErr.ChildIDs) != len(childIDs) {
		t.Fatalf("expected %d child IDs in error, got %d", len(childIDs), len(waitErr.ChildIDs))
	}
	for i, id := range childIDs {
		if waitErr.ChildIDs[i] != id {
			t.Fatalf("expected ChildIDs[%d]=%q, got %q", i, id, waitErr.ChildIDs[i])
		}
	}
	if waitErr.PollAfter <= 0 {
		t.Fatalf("expected positive PollAfter, got %s", waitErr.PollAfter)
	}
}

func TestWaitForAnyChildStep_FailsIfChildFailed(t *testing.T) {
	// Arrange: one child failed, others still running.
	childIDs := []string{"x", "y"}

	children := map[string]*WorkflowInstance{
		"x": {
			ID:     "x",
			Status: StatusFailed,
			Err:    fmt.Errorf("boom"),
		},
		"y": {
			ID:     "y",
			Status: StatusRunning,
		},
	}

	engine := newFakeEngine(children)
	ctx := WithEngine(context.Background(), engine)

	step := WaitForAnyChildStep(func(input any) []string {
		return childIDs
	}, 500)

	// Act
	output, err := step(ctx, childIDs)

	// Assert
	if output != nil {
		t.Fatalf("expected nil output on failure, got %T (%v)", output, output)
	}
	if err == nil {
		t.Fatalf("expected error when a child failed, got nil")
	}

	// Should NOT be a WaitForAnyChildError; it's a hard failure.
	var waitErr *WaitForAnyChildError
	if errors.As(err, &waitErr) {
		t.Fatalf("expected non-wait error on child failure, got WaitForAnyChildError: %v", err)
	}
}

func TestWaitForAnyChildStep_EmptyIDsReturnsImmediately(t *testing.T) {
	engine := newFakeEngine(map[string]*WorkflowInstance{})
	ctx := WithEngine(context.Background(), engine)

	step := WaitForAnyChildStep(func(input any) []string {
		return []string{}
	}, 500)

	// Act
	output, err := step(ctx, []string{})

	// Assert
	if err != nil {
		t.Fatalf("expected nil error for empty child ID list, got %v", err)
	}

	// For empty set, we accept any deterministic output; here we expect nil.
	if output != nil {
		t.Fatalf("expected nil output for empty child ID list, got %T (%v)", output, output)
	}
}

func TestIsWaitForAnyChildError(t *testing.T) {
	err := &WaitForAnyChildError{
		ChildIDs:  []string{"c1"},
		PollAfter: 250 * time.Millisecond,
	}

	if !IsWaitForAnyChildError(err) {
		t.Fatalf("expected IsWaitForAnyChildError to return true for WaitForAnyChildError")
	}

	if IsWaitForAnyChildError(fmt.Errorf("other")) {
		t.Fatalf("expected IsWaitForAnyChildError to return false for non-WaitForAnyChildError")
	}
}
