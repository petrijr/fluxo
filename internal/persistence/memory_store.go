package persistence

import (
	"fmt"
	"sync"

	"github.com/petrijr/fluxo/pkg/api"
)

// InMemoryStore is a simple, goroutine-safe implementation of
// WorkflowStore and InstanceStore backed by maps.
type InMemoryStore struct {
	mu        sync.RWMutex
	workflows map[string]map[string]api.WorkflowDefinition
	instances map[string]*api.WorkflowInstance
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		workflows: make(map[string]map[string]api.WorkflowDefinition),
		instances: make(map[string]*api.WorkflowInstance),
	}
}

// Ensure InMemoryStore implements the interfaces.
var _ WorkflowStore = (*InMemoryStore)(nil)

var _ InstanceStore = (*InMemoryStore)(nil)

func (s *InMemoryStore) SaveWorkflow(def api.WorkflowDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	versions := s.workflows[def.Name]
	if versions == nil {
		versions = make(map[string]api.WorkflowDefinition)
		s.workflows[def.Name] = versions
	}
	if _, exists := versions[def.Version]; exists {
		return fmt.Errorf("workflow %q version %q already exists", def.Name, def.Version)
	}
	versions[def.Version] = def
	return nil
}

func (s *InMemoryStore) GetWorkflow(name string, version string) (api.WorkflowDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions, ok := s.workflows[name]
	if !ok {
		return api.WorkflowDefinition{}, ErrWorkflowNotFound
	}

	def, ok := versions[version]
	if !ok {
		return api.WorkflowDefinition{}, ErrWorkflowNotFound
	}

	return def, nil
}

func (s *InMemoryStore) GetLatestWorkflow(name string) (api.WorkflowDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions, ok := s.workflows[name]
	if !ok || len(versions) > 1 {
		return api.WorkflowDefinition{}, ErrWorkflowNotFound
	}

	var value api.WorkflowDefinition
	for _, v := range versions {
		value = v
		break
	}

	return value, nil
}

func (s *InMemoryStore) ListWorkflowVersions(name string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions, ok := s.workflows[name]
	if !ok {
		return nil, fmt.Errorf("no versions for workflow %q", name)
	}

	var retval = make([]string, len(versions))
	for k := range versions {
		retval = append(retval, k)
		break
	}

	return retval, nil
}

func (s *InMemoryStore) SaveInstance(inst *api.WorkflowInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.instances[inst.ID] = inst
	return nil
}

func (s *InMemoryStore) UpdateInstance(inst *api.WorkflowInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.instances[inst.ID]; !ok {
		return ErrInstanceNotFound
	}

	s.instances[inst.ID] = inst
	return nil
}

func (s *InMemoryStore) GetInstance(id string) (*api.WorkflowInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, ok := s.instances[id]
	if !ok {
		return nil, ErrInstanceNotFound
	}

	return inst, nil
}

func (s *InMemoryStore) ListInstances(filter InstanceFilter) ([]*api.WorkflowInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*api.WorkflowInstance

	for _, inst := range s.instances {
		if filter.WorkflowName != "" && inst.Name != filter.WorkflowName {
			continue
		}
		if filter.Status != "" && inst.Status != filter.Status {
			continue
		}
		result = append(result, inst)
	}

	return result, nil
}
