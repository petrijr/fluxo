package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/petrijr/fluxo/pkg/api"
)

func TestInMemoryStore_LeaseAcquireRenewRelease(t *testing.T) {
	store := NewInMemoryStore()
	inst := &api.WorkflowInstance{
		ID:     "i1",
		Name:   "wf",
		Status: api.StatusWaiting,
	}
	if err := store.SaveInstance(inst); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}

	ctx := context.Background()

	acq, err := store.TryAcquireLease(ctx, inst.ID, "owner1", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	if !acq {
		t.Fatalf("expected acquired")
	}

	acq2, err := store.TryAcquireLease(ctx, inst.ID, "owner2", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("TryAcquireLease owner2: %v", err)
	}
	if acq2 {
		t.Fatalf("expected not acquired while lease active")
	}

	if err := store.RenewLease(ctx, inst.ID, "owner1", 50*time.Millisecond); err != nil {
		t.Fatalf("RenewLease owner1: %v", err)
	}

	if err := store.RenewLease(ctx, inst.ID, "owner2", 50*time.Millisecond); err == nil {
		t.Fatalf("expected RenewLease owner2 to fail")
	}

	if err := store.ReleaseLease(ctx, inst.ID, "owner1"); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}

	acq3, err := store.TryAcquireLease(ctx, inst.ID, "owner2", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("TryAcquireLease owner2 after release: %v", err)
	}
	if !acq3 {
		t.Fatalf("expected owner2 to acquire after release")
	}
}

func TestInMemoryStore_LeaseExpires(t *testing.T) {
	store := NewInMemoryStore()
	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	if err := store.SaveInstance(inst); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}

	ctx := context.Background()

	acq, err := store.TryAcquireLease(ctx, inst.ID, "owner1", 20*time.Millisecond)
	if err != nil || !acq {
		t.Fatalf("TryAcquireLease owner1: acq=%v err=%v", acq, err)
	}

	time.Sleep(30 * time.Millisecond)

	acq2, err := store.TryAcquireLease(ctx, inst.ID, "owner2", 20*time.Millisecond)
	if err != nil {
		t.Fatalf("TryAcquireLease owner2: %v", err)
	}
	if !acq2 {
		t.Fatalf("expected owner2 to acquire after expiry")
	}
}
