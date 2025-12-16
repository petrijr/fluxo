package persistence

import (
	"errors"

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
	// ListInstances returns instances matching the given filter.
	// If filter fields are zero-valued, all instances are returned.
	ListInstances(filter InstanceFilter) ([]*api.WorkflowInstance, error)
}
