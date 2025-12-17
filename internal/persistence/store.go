package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/petrijr/fluxo/pkg/api"
)

var (
	// ErrWorkflowNotFound is returned when a workflow definition is not found.
	ErrWorkflowNotFound = errors.New("workflow not found")

	// ErrInstanceNotFound is returned when a workflow instance is not found.
	ErrInstanceNotFound = errors.New("instance not found")
)

// WorkflowStore handles storage of workflow definitions.
type WorkflowStore interface {
	SaveWorkflow(def api.WorkflowDefinition) error
	// GetWorkflow returns the workflow definition for a name+version.
	GetWorkflow(name string, version string) (api.WorkflowDefinition, error)
	// GetLatestWorkflow returns the workflow if exactly one version exists.
	// Errors if zero or multiple versions are present.
	GetLatestWorkflow(name string) (api.WorkflowDefinition, error)
	ListWorkflowVersions(name string) ([]string, error)
}

// InstanceFilter is used to select instances from the store.
// Empty string / zero status mean "no filter" for that field.
type InstanceFilter struct {
	WorkflowName string
	Status       api.Status
}

// InstanceStore handles storage of workflow instances.
type InstanceStore interface {
	SaveInstance(inst *api.WorkflowInstance) error
	UpdateInstance(inst *api.WorkflowInstance) error
	GetInstance(id string) (*api.WorkflowInstance, error)
	ListInstances(filter InstanceFilter) ([]*api.WorkflowInstance, error)
	// TryAcquireLease attempts to acquire (or re-acquire) a lease on an instance.
	// If the instance is currently leased by another owner and the lease has not expired,
	// it returns acquired=false, err=nil.
	//
	// Implementations should treat a lease owned by the same owner as re-entrant.
	TryAcquireLease(ctx context.Context, instanceID, owner string, ttl time.Duration) (acquired bool, err error)
	// RenewLease extends an existing lease owned by 'owner' for the given ttl.
	RenewLease(ctx context.Context, instanceID, owner string, ttl time.Duration) error
	// ReleaseLease releases a lease if it is owned by 'owner'. It is idempotent.
	ReleaseLease(ctx context.Context, instanceID, owner string) error
}
