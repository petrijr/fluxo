package persistence

import (
	"errors"

	corep "github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
)

func (r *RedisStoreTestSuite) TestRedisInstanceStore_SaveGetUpdate() {
	inst := &api.WorkflowInstance{
		ID:          "redis-test-1",
		Name:        "wf-test",
		Status:      api.StatusPending,
		CurrentStep: 0,
		Input: redisSamplePayload{
			Msg: "hello",
			N:   42,
		},
	}

	// Save
	err := r.store.SaveInstance(inst)
	r.NoErrorf(err, "SaveInstance failed: %s", "formatted")

	// Get
	got, err := r.store.GetInstance("redis-test-1")
	r.NoErrorf(err, "GetInstance failed: %s", "formatted")

	if got.ID != inst.ID || got.Name != inst.Name || got.Status != inst.Status || got.CurrentStep != inst.CurrentStep {
		r.Failf("unexpected instance", "unexpected instance after Get: %+v", got)
	}

	inPayload, ok := got.Input.(redisSamplePayload)
	if !ok {
		r.Failf("expected Input of type redisSamplePayload", "got %T", got.Input)
	}
	if inPayload.Msg != "hello" || inPayload.N != 42 {
		r.Failf("unexpected input", "payload: %+v", inPayload)
	}

	// Update: mark completed with output + error
	got.Status = api.StatusCompleted
	got.CurrentStep = 2
	got.Output = redisSamplePayload{Msg: "done", N: 99}
	got.Err = errors.New("something happened")

	err = r.store.UpdateInstance(got)
	r.NoError(err, "UpdateInstance failed: %v", "formatted")

	got2, err := r.store.GetInstance(got.ID)
	r.NoError(err, "GetInstance after update failed: %v", "formatted")

	if got2.Status != api.StatusCompleted || got2.CurrentStep != 2 {
		r.Failf("unexpected status/current_step", "unexpected status/current_step after update: %+v", got2)
	}

	outPayload, ok := got2.Output.(redisSamplePayload)
	if !ok {
		r.Failf("expected Output of type redisSamplePayload", "got %T", got2.Output)
	}
	if outPayload.Msg != "done" || outPayload.N != 99 {
		r.Failf("unexpected output ", "payload: %+v", outPayload)
	}
	if got2.Err == nil || got2.Err.Error() != "something happened" {
		r.Failf("unexpected error", "value: %v", got2.Err)
	}
}

func (r *RedisStoreTestSuite) TestRedisInstanceStore_ListInstancesFilters() {
	instances := []*api.WorkflowInstance{
		{
			ID:          "redis-list-1",
			Name:        "wf-A",
			Status:      api.StatusPending,
			CurrentStep: 0,
			Input:       redisSamplePayload{Msg: "a1"},
		},
		{
			ID:          "redis-list-2",
			Name:        "wf-A",
			Status:      api.StatusCompleted,
			CurrentStep: 1,
			Input:       redisSamplePayload{Msg: "a2"},
		},
		{
			ID:          "redis-list-3",
			Name:        "wf-B",
			Status:      api.StatusCompleted,
			CurrentStep: 1,
			Input:       redisSamplePayload{Msg: "b1"},
		},
	}

	for _, inst := range instances {
		err := r.store.SaveInstance(inst)
		r.NoError(err, "SaveInstance(%r)", inst.ID, "formatted")
	}

	// Unfiltered
	all, err := r.store.ListInstances(corep.InstanceFilter{})
	r.NoError(err, "ListInstances (no filter) failed: %v", "formatted")

	// Filter by workflow name
	wfA, err := r.store.ListInstances(corep.InstanceFilter{WorkflowName: "wf-A"})
	r.NoError(err, "ListInstances (wf-A) failed: %v", "formatted")

	// Filter by status
	completed, err := r.store.ListInstances(corep.InstanceFilter{Status: api.StatusCompleted})
	r.NoError(err, "ListInstances (COMPLETED) failed: %v", "formatted")

	// Combined filter
	completedA, err := r.store.ListInstances(corep.InstanceFilter{
		WorkflowName: "wf-A",
		Status:       api.StatusCompleted,
	})
	r.NoError(err, "ListInstances (wf-A + COMPLETED) failed: %v", "formatted")

	if len(all) != len(instances) {
		r.Failf("incorrect instance count", "expected %d instances, got %d", len(instances), len(all))
	}
	if len(wfA) != 2 {
		r.Failf("incorrect instance count", "expected 2 wf-A instances, got %d", len(wfA))
	}
	if len(completed) != 2 {
		r.Failf("incorrect completed instance count", "expected 2 COMPLETED instances, got %d", len(completed))
	}
	if len(completedA) != 1 {
		r.Failf("incorrect completed instance count", "expected 1 COMPLETED wf-A instance, got %d", len(completedA))
	}
}
