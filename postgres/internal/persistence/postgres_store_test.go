package persistence

import (
	"errors"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	corep "github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
)

func (p *PostgresStoreTestSuite) TestPostgresInstanceStore_SaveGetUpdate() {
	inst := &api.WorkflowInstance{
		ID:          "pg-test-1",
		Name:        "wf-test",
		Status:      api.StatusPending,
		CurrentStep: 0,
		Input: pgSamplePayload{
			Msg: "hello",
			N:   42,
		},
	}

	// Save
	err := p.store.SaveInstance(inst)
	p.NoErrorf(err, "SaveInstance failed: %v", "formatted")

	// Get
	got, err := p.store.GetInstance("pg-test-1")
	p.NoErrorf(err, "GetInstance failed: %v", "formatted")

	if got.ID != inst.ID || got.Name != inst.Name || got.Status != inst.Status || got.CurrentStep != inst.CurrentStep {
		p.Failf("unexpected instance", "unexpected instance after Get: %+v", got)
	}

	inPayload, ok := got.Input.(pgSamplePayload)
	if !ok {
		p.Failf("expected Input of type pgSamplePayload", "got %T", got.Input)
	}
	if inPayload.Msg != "hello" || inPayload.N != 42 {
		p.Failf("unexpected input", "payload: %+v", inPayload)
	}

	// Update: mark completed with output + error
	got.Status = api.StatusCompleted
	got.CurrentStep = 2
	got.Output = pgSamplePayload{Msg: "done", N: 99}
	got.Err = errors.New("something happened")

	err = p.store.UpdateInstance(got)
	p.NoError(err, "UpdateInstance failed: %v", "formatted")

	got2, err := p.store.GetInstance(got.ID)
	p.NoError(err, "GetInstance after update failed: %v", "formatted")

	if got2.Status != api.StatusCompleted || got2.CurrentStep != 2 {
		p.Failf("unexpected status/current_step", "unexpected status/current_step after update: %+v", got2)
	}

	outPayload, ok := got2.Output.(pgSamplePayload)
	if !ok {
		p.Failf("expected Output of type pgSamplePayload", "got %T", got2.Output)
	}
	if outPayload.Msg != "done" || outPayload.N != 99 {
		p.Failf("unexpected output ", "payload: %+v", outPayload)
	}
	if got2.Err == nil || got2.Err.Error() != "something happened" {
		p.Failf("unexpected error", "value: %v", got2.Err)
	}
}

func (p *PostgresStoreTestSuite) TestPostgresInstanceStore_ListInstancesFilters() {
	instances := []*api.WorkflowInstance{
		{
			ID:          "pg-list-1",
			Name:        "wf-A",
			Status:      api.StatusPending,
			CurrentStep: 0,
			Input:       pgSamplePayload{Msg: "a1"},
		},
		{
			ID:          "pg-list-2",
			Name:        "wf-A",
			Status:      api.StatusCompleted,
			CurrentStep: 1,
			Input:       pgSamplePayload{Msg: "a2"},
		},
		{
			ID:          "pg-list-3",
			Name:        "wf-B",
			Status:      api.StatusCompleted,
			CurrentStep: 1,
			Input:       pgSamplePayload{Msg: "b1"},
		},
	}

	for _, inst := range instances {
		err := p.store.SaveInstance(inst)
		p.NoError(err, "SaveInstance(%r)", inst.ID, "formatted")
	}

	// Unfiltered
	all, err := p.store.ListInstances(corep.InstanceFilter{})
	p.NoError(err, "ListInstances (no filter) failed: %v", "formatted")

	if len(all) != len(instances) {
		p.Failf("incorrect instance count", "expected %d instances, got %d", len(instances), len(all))
	}

	// Filter by workflow name
	wfA, err := p.store.ListInstances(corep.InstanceFilter{WorkflowName: "wf-A"})
	p.NoError(err, "ListInstances (wf-A) failed: %v", "formatted")

	if len(wfA) != 2 {
		p.Failf("incorrect instance count", "expected 2 wf-A instances, got %d", len(wfA))
	}
	for _, inst := range wfA {
		if inst.Name != "wf-A" {
			p.Failf("unexpected workflow", "unexpected workflow in wf-A filter: %+v", inst)
		}
	}

	// Filter by status
	completed, err := p.store.ListInstances(corep.InstanceFilter{Status: api.StatusCompleted})
	p.NoErrorf(err, "ListInstances (COMPLETED) failed: %v", "formatted")

	if len(completed) != 2 {
		p.Failf("expected 2 COMPLETED instances", "expected 2 COMPLETED instances, got %d", len(completed))
	}
	for _, inst := range completed {
		if inst.Status != api.StatusCompleted {
			p.Failf("unexpected instance", "unexpected instance in COMPLETED filter: %+v", inst)
		}
	}

	// Combined filter
	completedA, err := p.store.ListInstances(corep.InstanceFilter{
		WorkflowName: "wf-A",
		Status:       api.StatusCompleted,
	})
	p.NoErrorf(err, "ListInstances (wf-A + COMPLETED) failed: %v", err)

	if len(completedA) != 1 {
		p.Failf("expected 1 COMPLETED", "expected 1 COMPLETED wf-A instance, got %d", len(completedA))
	}
	for _, inst := range completedA {
		if inst.Name != "wf-A" || inst.Status != api.StatusCompleted {
			p.Failf("unexpected instance", "unexpected instance in combined filter: %+v", inst)
		}
	}
}
