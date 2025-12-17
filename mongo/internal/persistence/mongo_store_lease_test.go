package persistence

import (
	"context"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

func (m *MongoDBStoreTestSuite) TestMongoInstanceStore_LeaseAcquireRenewRelease() {
	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	err := m.store.SaveInstance(inst)
	m.NoErrorf(err, "SaveInstance failed: %v", "formatted")

	ctx := context.Background()

	acq, err := m.store.TryAcquireLease(ctx, inst.ID, "owner1", 100*time.Millisecond)
	m.NoErrorf(err, "TryAcquireLease owner1: %v", "formatted")
	m.True(acq, "expected owner1 to acquire")

	acq2, err := m.store.TryAcquireLease(ctx, inst.ID, "owner2", 100*time.Millisecond)
	m.NoErrorf(err, "TryAcquireLease owner2: %v", "formatted")
	m.False(acq2, "expected owner2 not to acquire while active")

	err = m.store.RenewLease(ctx, inst.ID, "owner1", 100*time.Millisecond)
	m.NoErrorf(err, "RenewLease owner1: %v", "formatted")

	err = m.store.RenewLease(ctx, inst.ID, "owner2", 100*time.Millisecond)
	m.Error(err, "expected RenewLease owner2 to fail", "formatted")

	err = m.store.ReleaseLease(ctx, inst.ID, "owner1")
	m.NoErrorf(err, "ReleaseLease: %v", "formatted")

	acq3, err := m.store.TryAcquireLease(ctx, inst.ID, "owner2", 100*time.Millisecond)
	m.NoErrorf(err, "TryAcquireLease owner2 after release: %v", "formatted")
	m.True(acq3, "expected owner2 to acquire after release")
}

func (m *MongoDBStoreTestSuite) TestMongoInstanceStore_LeaseConcurrentAcquireOnlyOne() {
	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	err := m.store.SaveInstance(inst)
	m.NoErrorf(err, "SaveInstance failed: %v", "formatted")

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
			ok, err := m.store.TryAcquireLease(ctx, inst.ID, o, 250*time.Millisecond)
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

	m.EqualValues(1, len(acquired), "expected exactly one acquirer, got %d: %v", len(acquired), acquired)
}

func (m *MongoDBStoreTestSuite) TestMongoInstanceStore_LeaseExpires() {
	inst := &api.WorkflowInstance{ID: "i1", Name: "wf", Status: api.StatusWaiting}
	err := m.store.SaveInstance(inst)
	m.NoErrorf(err, "SaveInstance failed: %v", "formatted")

	ctx := context.Background()

	acq, err := m.store.TryAcquireLease(ctx, inst.ID, "owner1", 20*time.Millisecond)
	m.NoErrorf(err, "TryAcquireLease owner1: %v", "formatted")
	m.True(acq, "expected owner1 to acquire")

	time.Sleep(30 * time.Millisecond)

	acq2, err := m.store.TryAcquireLease(ctx, inst.ID, "owner2", 20*time.Millisecond)
	m.NoErrorf(err, "TryAcquireLease owner2: %v", "formatted")
	m.True(acq2, "expected owner2 to acquire after expiry")
}
