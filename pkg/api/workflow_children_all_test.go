package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- StartChildrenStep tests ---

// startChildrenEngine is a minimal Engine implementation for StartChildrenStep.
// Only Run is expected to be used; other methods panic if called.
type startChildrenEngine struct {
	runCalls []struct {
		name  string
		input any
	}
	runErr error
}

func (e *startChildrenEngine) RunVersion(ctx context.Context, name string, version string, input any) (*WorkflowInstance, error) {
	//TODO implement me
	panic("implement me")
}

func (e *startChildrenEngine) RegisterWorkflow(def WorkflowDefinition) error {
	panic("RegisterWorkflow should not be called in startChildrenEngine")
}

func (e *startChildrenEngine) Run(ctx context.Context, name string, input any) (*WorkflowInstance, error) {
	if e.runErr != nil {
		return nil, e.runErr
	}
	e.runCalls = append(e.runCalls, struct {
		name  string
		input any
	}{name: name, input: input})
	id := fmt.Sprintf("child-%d", len(e.runCalls))
	return &WorkflowInstance{
		ID:     id,
		Name:   name,
		Input:  input,
		Status: StatusRunning,
	}, nil
}

func (e *startChildrenEngine) GetInstance(ctx context.Context, id string) (*WorkflowInstance, error) {
	panic("GetInstance should not be called in startChildrenEngine")
}

func (e *startChildrenEngine) ListInstances(ctx context.Context, opts InstanceListOptions) ([]*WorkflowInstance, error) {
	panic("ListInstances should not be called in startChildrenEngine")
}

func (e *startChildrenEngine) Resume(ctx context.Context, id string) (*WorkflowInstance, error) {
	panic("Resume should not be called in startChildrenEngine")
}

func (e *startChildrenEngine) Signal(ctx context.Context, id string, name string, payload any) (*WorkflowInstance, error) {
	panic("Signal should not be called in startChildrenEngine")
}

func (e *startChildrenEngine) RecoverStuckInstances(ctx context.Context) (int, error) {
	panic("RecoverStuckInstances should not be called in startChildrenEngine")
}

func TestStartChildrenStep_ValidationAndSuccess(t *testing.T) {
	ctx := context.Background()

	// specsFn=nil should error before touching engine.
	step := StartChildrenStep(nil)
	if _, err := step(ctx, nil); err == nil {
		t.Fatalf("expected error for nil specsFn")
	}

	// engine missing from context
	step = StartChildrenStep(func(input any) ([]ChildWorkflowSpec, error) {
		return []ChildWorkflowSpec{{Name: "child", Input: 1}}, nil
	})
	if _, err := step(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "engine not available") {
		t.Fatalf("expected engine not available error, got %v", err)
	}

	// specsFn error is propagated
	engine := &startChildrenEngine{}
	ctxWithEng := WithEngine(ctx, engine)
	wantErr := errors.New("specs error")
	step = StartChildrenStep(func(input any) ([]ChildWorkflowSpec, error) {
		return nil, wantErr
	})
	if _, err := step(ctxWithEng, nil); err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected specs error, got %v", err)
	}

	// empty specs => empty []string, no Run calls
	step = StartChildrenStep(func(input any) ([]ChildWorkflowSpec, error) {
		return []ChildWorkflowSpec{}, nil
	})
	out, err := step(ctxWithEng, "ignored")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(engine.runCalls); got != 0 {
		t.Fatalf("expected no Run calls for empty specs, got %d", got)
	}
	if ids, ok := out.([]string); !ok || len(ids) != 0 {
		t.Fatalf("expected empty []string, got %#v", out)
	}

	// child with empty name should error
	step = StartChildrenStep(func(input any) ([]ChildWorkflowSpec, error) {
		return []ChildWorkflowSpec{
			{Name: "", Input: 1},
		}, nil
	})
	if _, err := step(ctxWithEng, nil); err == nil || !strings.Contains(err.Error(), "child workflow name is empty") {
		t.Fatalf("expected child-name error, got %v", err)
	}

	// Run failure is wrapped
	engine.runErr = errors.New("run-fail")
	step = StartChildrenStep(func(input any) ([]ChildWorkflowSpec, error) {
		return []ChildWorkflowSpec{
			{Name: "wf", Input: 123},
		}, nil
	})
	if _, err := step(ctxWithEng, nil); err == nil || !strings.Contains(err.Error(), "starting child") {
		t.Fatalf("expected starting child error, got %v", err)
	}
	engine.runErr = nil

	// happy path: two children successfully started
	engine.runCalls = nil
	step = StartChildrenStep(func(input any) ([]ChildWorkflowSpec, error) {
		return []ChildWorkflowSpec{
			{Name: "child-A", Input: "a"},
			{Name: "child-B", Input: "b"},
		}, nil
	})
	out, err = step(ctxWithEng, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids, ok := out.([]string)
	if !ok {
		t.Fatalf("expected []string output, got %T", out)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 child IDs, got %v", ids)
	}
	if len(engine.runCalls) != 2 {
		t.Fatalf("expected 2 Run calls, got %d", len(engine.runCalls))
	}
	if engine.runCalls[0].name != "child-A" || engine.runCalls[1].name != "child-B" {
		t.Fatalf("unexpected Run call names: %+v", engine.runCalls)
	}
}

// --- WaitForChildrenStep tests ---

func TestWaitForChildrenStep_ValidationAndEmpty(t *testing.T) {
	// getIDs=nil should error, even if engine is present.
	step := WaitForChildrenStep(nil, 0)
	ctx := WithEngine(context.Background(), newFakeEngine(map[string]*WorkflowInstance{}))

	if _, err := step(ctx, nil); err == nil || !strings.Contains(err.Error(), "getIDs must not be nil") {
		t.Fatalf("expected getIDs validation error, got %v", err)
	}

	// engine missing in context
	step = WaitForChildrenStep(func(input any) []string { return []string{"c1"} }, 0)
	if _, err := step(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "engine not available") {
		t.Fatalf("expected engine not available error, got %v", err)
	}

	// empty IDs => empty []any and nil error
	step = WaitForChildrenStep(func(input any) []string { return []string{} }, 0)
	out, err := step(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vals, ok := out.([]any); !ok || len(vals) != 0 {
		t.Fatalf("expected empty []any, got %#v", out)
	}
}

func TestWaitForChildrenStep_AllCompletedReturnsOutputs(t *testing.T) {
	childIDs := []string{"a", "b", "c"}
	children := map[string]*WorkflowInstance{
		"a": {ID: "a", Status: StatusCompleted, Output: "A"},
		"b": {ID: "b", Status: StatusCompleted, Output: "B"},
		"c": {ID: "c", Status: StatusCompleted, Output: "C"},
	}
	engine := newFakeEngine(children)
	ctx := WithEngine(context.Background(), engine)

	step := WaitForChildrenStep(func(input any) []string { return childIDs }, 250*time.Millisecond)
	out, err := step(ctx, childIDs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	results, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(results))
	}
	if results[0].(string) != "A" || results[1].(string) != "B" || results[2].(string) != "C" {
		t.Fatalf("unexpected outputs: %#v", results)
	}
}

func TestWaitForChildrenStep_SomePendingReturnsWaitError(t *testing.T) {
	childIDs := []string{"a", "b"}
	children := map[string]*WorkflowInstance{
		"a": {ID: "a", Status: StatusCompleted, Output: "A"},
		"b": {ID: "b", Status: StatusRunning},
	}
	engine := newFakeEngine(children)
	ctx := WithEngine(context.Background(), engine)

	step := WaitForChildrenStep(func(input any) []string { return childIDs }, 0) // pollInterval<=0 uses default
	out, err := step(ctx, childIDs)
	if err == nil {
		t.Fatalf("expected WaitForChildrenError, got nil (out=%v)", out)
	}
	waitErr, ok := err.(*WaitForChildrenError)
	if !ok {
		t.Fatalf("expected WaitForChildrenError, got %T (%v)", err, err)
	}
	if len(waitErr.ChildIDs) != 2 || waitErr.ChildIDs[0] != "a" || waitErr.ChildIDs[1] != "b" {
		t.Fatalf("unexpected ChildIDs: %#v", waitErr.ChildIDs)
	}
	if waitErr.PollAfter <= 0 {
		t.Fatalf("expected positive PollAfter, got %s", waitErr.PollAfter)
	}
}
