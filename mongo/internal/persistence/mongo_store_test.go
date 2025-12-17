package persistence

import (
	"errors"

	corep "github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
)

func (m *MongoDBStoreTestSuite) TestMongoInstanceStore_SaveGetUpdate() {
	inst := &api.WorkflowInstance{
		ID:          "mongo-test-1",
		Name:        "wf-test",
		Status:      api.StatusPending,
		CurrentStep: 0,
		Input: mongoSamplePayload{
			Msg: "hello",
			N:   42,
		},
	}

	// Save
	err := m.store.SaveInstance(inst)
	m.NoErrorf(err, "SaveInstance failed: %v", "formatted")

	// Get
	got, err := m.store.GetInstance("mongo-test-1")
	m.NoErrorf(err, "GetInstance failed: %v", "formatted")

	if got.ID != inst.ID || got.Name != inst.Name || got.Status != inst.Status || got.CurrentStep != inst.CurrentStep {
		m.Failf("unexpected instance", "unexpected instance after Get: %+v", got)
	}

	inPayload, _ := got.Input.(mongoSamplePayload)
	m.IsType(mongoSamplePayload{}, got.Input)

	if inPayload.Msg != "hello" || inPayload.N != 42 {
		m.Failf("unexpected input", "payload: %+v", inPayload)
	}

	// Update: mark completed with output + error
	got.Status = api.StatusCompleted
	got.CurrentStep = 2
	got.Output = mongoSamplePayload{Msg: "done", N: 99}
	got.Err = errors.New("something happened")

	err = m.store.UpdateInstance(got)
	m.NoError(err, "UpdateInstance failed: %v", "formatted")

	got2, err := m.store.GetInstance(got.ID)
	m.NoError(err, "GetInstance after update failed: %v", "formatted")

	if got2.Status != api.StatusCompleted || got2.CurrentStep != 2 {
		m.Failf("unexpected status/current_step", "unexpected status/current_step after update: %+v", got2)
	}

	outPayload, _ := got2.Output.(mongoSamplePayload)
	m.IsType(mongoSamplePayload{}, got.Input)

	if outPayload.Msg != "done" || outPayload.N != 99 {
		m.Failf("unexpected output ", "payload: %+v", outPayload)
	}
	if got2.Err == nil || got2.Err.Error() != "something happened" {
		m.Failf("unexpected error", "value: %v", got2.Err)
	}
}

func (m *MongoDBStoreTestSuite) TestMongoInstanceStore_ListInstancesFilters() {
	instances := []*api.WorkflowInstance{
		{
			ID:          "mongo-list-1",
			Name:        "wf-A",
			Status:      api.StatusPending,
			CurrentStep: 0,
			Input:       mongoSamplePayload{Msg: "a1"},
		},
		{
			ID:          "mongo-list-2",
			Name:        "wf-A",
			Status:      api.StatusCompleted,
			CurrentStep: 1,
			Input:       mongoSamplePayload{Msg: "a2"},
		},
		{
			ID:          "mongo-list-3",
			Name:        "wf-B",
			Status:      api.StatusCompleted,
			CurrentStep: 1,
			Input:       mongoSamplePayload{Msg: "b1"},
		},
	}

	for _, inst := range instances {
		if err := m.store.SaveInstance(inst); err != nil {
			m.NoError(err, "SaveInstance(%r)", inst.ID, "formatted")
		}
	}

	all, err := m.store.ListInstances(corep.InstanceFilter{})
	m.NoError(err, "ListInstances (no filter) failed: %v", "formatted")

	wfA, err := m.store.ListInstances(corep.InstanceFilter{WorkflowName: "wf-A"})
	m.NoError(err, "ListInstances (wf-A) failed: %v", "formatted")

	completed, err := m.store.ListInstances(corep.InstanceFilter{Status: api.StatusCompleted})
	m.NoErrorf(err, "ListInstances (COMPLETED) failed: %v", "formatted")

	completedA, err := m.store.ListInstances(corep.InstanceFilter{
		WorkflowName: "wf-A",
		Status:       api.StatusCompleted,
	})
	m.NoErrorf(err, "ListInstances (wf-A + COMPLETED) failed: %v", err)

	if len(all) != len(instances) {
		m.Failf("incorrect instance count", "expected %d instances, got %d", len(instances), len(all))
	}
	if len(wfA) != 2 {
		m.Failf("incorrect instance coun t", "expected 2 wf-A instances, got %d", len(wfA))
	}
	if len(completed) != 2 {
		m.Failf("expected 2 COMPLETED instances", "expected 2 COMPLETED instances, got %d", len(completed))
	}
	if len(completedA) != 1 {
		m.Failf("expected 1 COMPLETED", "expected 1 COMPLETED wf-A instance, got %d", len(completedA))
	}
}
