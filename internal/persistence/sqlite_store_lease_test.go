package persistence

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

func TestSQLiteInstanceStore_LeaseAcquireRenewRelease(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	store, err := NewSQLiteInstanceStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteInstanceStore: %v", err)
	}

	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	if err := store.SaveInstance(inst); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}

	ctx := context.Background()

	acq, err := store.TryAcquireLease(ctx, inst.ID, "owner1", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("TryAcquireLease owner1: %v", err)
	}
	if !acq {
		t.Fatalf("expected owner1 to acquire")
	}

	acq2, err := store.TryAcquireLease(ctx, inst.ID, "owner2", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("TryAcquireLease owner2: %v", err)
	}
	if acq2 {
		t.Fatalf("expected owner2 not to acquire while active")
	}

	if err := store.RenewLease(ctx, inst.ID, "owner1", 100*time.Millisecond); err != nil {
		t.Fatalf("RenewLease owner1: %v", err)
	}

	if err := store.RenewLease(ctx, inst.ID, "owner2", 100*time.Millisecond); err == nil {
		t.Fatalf("expected RenewLease owner2 to fail")
	}

	if err := store.ReleaseLease(ctx, inst.ID, "owner1"); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}

	acq3, err := store.TryAcquireLease(ctx, inst.ID, "owner2", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("TryAcquireLease owner2 after release: %v", err)
	}
	if !acq3 {
		t.Fatalf("expected owner2 to acquire after release")
	}
}

func TestSQLiteInstanceStore_LeaseConcurrentAcquireOnlyOne(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	store, err := NewSQLiteInstanceStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteInstanceStore: %v", err)
	}

	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	if err := store.SaveInstance(inst); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}

	ctx := context.Background()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		acquired []string
	)

	owners := []string{"owner1", "owner2", "owner3", "owner4"}
	for _, owner := range owners {
		wg.Add(1)
		go func(o string) {
			defer wg.Done()
			ok, err := store.TryAcquireLease(ctx, inst.ID, o, 250*time.Millisecond)
			if err != nil {
				return
			}
			if ok {
				mu.Lock()
				acquired = append(acquired, o)
				mu.Unlock()
			}
		}(owner)
	}
	wg.Wait()

	if len(acquired) != 1 {
		t.Fatalf("expected exactly one acquirer, got %d: %v", len(acquired), acquired)
	}
}

func TestSQLiteInstanceStore_LeaseExpires(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	store, err := NewSQLiteInstanceStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteInstanceStore: %v", err)
	}

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
