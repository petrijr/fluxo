package persistence

import (
	"database/sql"
	"encoding/gob"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

type samplePayload struct {
	Msg string
	N   int
}

func init() {
	gob.Register(samplePayload{})
}

func newTestSQLiteStore(t *testing.T) *SQLiteInstanceStore {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	store, err := NewSQLiteInstanceStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteInstanceStore failed: %v", err)
	}

	return store
}

func TestSQLiteInstanceStore_SaveGetUpdate(t *testing.T) {
	store := newTestSQLiteStore(t)

	inst := &api.WorkflowInstance{
		ID:          "wf-1",
		Name:        "test-wf",
		Status:      api.StatusRunning,
		Output:      "hello",
		Input:       "in-hello",
		StepResults: make(map[int]any),
	}

	if err := store.SaveInstance(inst); err != nil {
		t.Fatalf("SaveInstance failed: %v", err)
	}

	got, err := store.GetInstance("wf-1")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if got.ID != inst.ID {
		t.Fatalf("expected ID %q, got %q", inst.ID, got.ID)
	}
	if got.Name != inst.Name {
		t.Fatalf("expected Name %q, got %q", inst.Name, got.Name)
	}
	if got.Status != inst.Status {
		t.Fatalf("expected Status %q, got %q", inst.Status, got.Status)
	}
	if got.Input != "in-hello" {
		t.Fatalf("expected Input %q, got %v", "in-hello", got.Input)
	}
	if got.Output != "hello" {
		t.Fatalf("expected Output %q, got %v", "hello", got.Output)
	}

	// Update status and output.
	inst.Status = api.StatusCompleted
	inst.Input = "new-input"
	inst.Output = "world"

	if err := store.UpdateInstance(inst); err != nil {
		t.Fatalf("UpdateInstance failed: %v", err)
	}

	got2, err := store.GetInstance("wf-1")
	if err != nil {
		t.Fatalf("GetInstance after update failed: %v", err)
	}

	if got2.Status != api.StatusCompleted {
		t.Fatalf("expected updated Status COMPLETED, got %q", got2.Status)
	}
	if got2.Input != "new-input" {
		t.Fatalf("expected Input %q, got %v", "new-input", got.Input)
	}
	if got2.Output != "world" {
		t.Fatalf("expected updated Output %q, got %v", "world", got2.Output)
	}
}

func TestSQLiteInstanceStore_StructOutputRoundtrip(t *testing.T) {
	store := newTestSQLiteStore(t)

	inst := &api.WorkflowInstance{
		ID:     "wf-struct",
		Name:   "wf-struct",
		Status: api.StatusCompleted,
		Output: samplePayload{
			Msg: "hello",
			N:   42,
		},
		StepResults: make(map[int]any),
	}

	if err := store.SaveInstance(inst); err != nil {
		t.Fatalf("SaveInstance failed: %v", err)
	}

	got, err := store.GetInstance("wf-struct")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if got.Status != api.StatusCompleted {
		t.Fatalf("expected Status COMPLETED, got %q", got.Status)
	}

	payload, ok := got.Output.(samplePayload)
	if !ok {
		t.Fatalf("expected Output to be samplePayload, got %T", got.Output)
	}

	if payload.Msg != "hello" || payload.N != 42 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestSQLiteInstanceStore_GetInstanceNotFound(t *testing.T) {
	store := newTestSQLiteStore(t)

	_, err := store.GetInstance("does-not-exist")
	if err == nil {
		t.Fatalf("expected error for missing instance")
	}
	if !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("expected ErrInstanceNotFound, got %v", err)
	}
}

func TestSQLiteInstanceStore_ListInstancesFilter(t *testing.T) {
	store := newTestSQLiteStore(t)

	// Create three instances: two for wf-A (COMPLETED), one for wf-B (FAILED).
	completedA1 := &api.WorkflowInstance{
		ID:          "a-1",
		Name:        "wf-A",
		Status:      api.StatusCompleted,
		Output:      "A1",
		StepResults: make(map[int]any),
	}
	completedA2 := &api.WorkflowInstance{
		ID:          "a-2",
		Name:        "wf-A",
		Status:      api.StatusCompleted,
		Output:      "A2",
		StepResults: make(map[int]any),
	}
	failedB := &api.WorkflowInstance{
		ID:          "b-1",
		Name:        "wf-B",
		Status:      api.StatusFailed,
		Output:      "B1",
		StepResults: make(map[int]any),
	}

	for _, inst := range []*api.WorkflowInstance{completedA1, completedA2, failedB} {
		if err := store.SaveInstance(inst); err != nil {
			t.Fatalf("SaveInstance(%q) failed: %v", inst.ID, err)
		}
	}

	// No filter -> all instances.
	all, err := store.ListInstances(InstanceFilter{})
	if err != nil {
		t.Fatalf("ListInstances (no filter) failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 instances, got %d", len(all))
	}

	// Filter by workflow name.
	onlyA, err := store.ListInstances(InstanceFilter{
		WorkflowName: "wf-A",
	})
	if err != nil {
		t.Fatalf("ListInstances (workflow filter) failed: %v", err)
	}
	if len(onlyA) != 2 {
		t.Fatalf("expected 2 instances for wf-A, got %d", len(onlyA))
	}
	for _, inst := range onlyA {
		if inst.Name != "wf-A" {
			t.Fatalf("expected workflow name wf-A, got %q", inst.Name)
		}
	}

	// Filter by status.
	completed, err := store.ListInstances(InstanceFilter{
		Status: api.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("ListInstances (status filter) failed: %v", err)
	}
	if len(completed) != 2 {
		t.Fatalf("expected 2 COMPLETED instances, got %d", len(completed))
	}
	for _, inst := range completed {
		if inst.Status != api.StatusCompleted {
			t.Fatalf("expected COMPLETED status, got %q", inst.Status)
		}
	}

	// Combined filter.
	completedA, err := store.ListInstances(InstanceFilter{
		WorkflowName: "wf-A",
		Status:       api.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("ListInstances (combined filter) failed: %v", err)
	}
	if len(completedA) != 2 {
		t.Fatalf("expected 2 COMPLETED wf-A instances, got %d", len(completedA))
	}
	for _, inst := range completedA {
		if inst.Name != "wf-A" || inst.Status != api.StatusCompleted {
			t.Fatalf("unexpected instance in combined filter: %+v", inst)
		}
	}
}
