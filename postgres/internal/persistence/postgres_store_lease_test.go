package persistence

import (
	"context"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

func (p *PostgresStoreTestSuite) TestPostgresInstanceStore_LeaseAcquireRenewRelease() {
	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	err := p.store.SaveInstance(inst)
	p.NoErrorf(err, "SaveInstance failed: %v", "formatted")

	ctx := context.Background()

	acq, err := p.store.TryAcquireLease(ctx, inst.ID, "owner1", 100*time.Millisecond)
	p.NoErrorf(err, "TryAcquireLease owner1: %v", "formatted")
	p.True(acq, "expected owner1 to acquire")

	acq2, err := p.store.TryAcquireLease(ctx, inst.ID, "owner2", 100*time.Millisecond)
	p.NoErrorf(err, "TryAcquireLease owner2: %v", "formatted")
	p.False(acq2, "expected owner2 not to acquire while active")

	err = p.store.RenewLease(ctx, inst.ID, "owner1", 100*time.Millisecond)
	p.NoErrorf(err, "RenewLease owner1: %v", "formatted")

	err = p.store.RenewLease(ctx, inst.ID, "owner2", 100*time.Millisecond)
	p.Error(err, "expected RenewLease owner2 to fail", "formatted")

	err = p.store.ReleaseLease(ctx, inst.ID, "owner1")
	p.NoErrorf(err, "ReleaseLease: %v", "formatted")

	acq3, err := p.store.TryAcquireLease(ctx, inst.ID, "owner2", 100*time.Millisecond)
	p.NoErrorf(err, "TryAcquireLease owner2 after release: %v", "formatted")
	p.True(acq3, "expected owner2 to acquire after release")
}

func (p *PostgresStoreTestSuite) TestSQLiteInstanceStore_LeaseConcurrentAcquireOnlyOne() {
	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	err := p.store.SaveInstance(inst)
	p.NoErrorf(err, "SaveInstance failed: %v", "formatted")

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
			ok, err := p.store.TryAcquireLease(ctx, inst.ID, o, 250*time.Millisecond)
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

	p.EqualValues(1, len(acquired), "expected exactly one acquirer, got %d: %v", len(acquired), acquired)
}

func (p *PostgresStoreTestSuite) TestSQLiteInstanceStore_LeaseExpires() {
	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	err := p.store.SaveInstance(inst)
	p.NoErrorf(err, "SaveInstance failed: %v", "formatted")

	ctx := context.Background()

	acq, err := p.store.TryAcquireLease(ctx, inst.ID, "owner1", 20*time.Millisecond)
	p.NoErrorf(err, "TryAcquireLease owner1: %v", "formatted")
	p.True(acq, "expected owner1 to acquire")

	time.Sleep(30 * time.Millisecond)

	acq2, err := p.store.TryAcquireLease(ctx, inst.ID, "owner2", 20*time.Millisecond)
	p.NoErrorf(err, "TryAcquireLease owner2: %v", "formatted")
	p.True(acq2, "expected owner2 to acquire after expiry")
}
