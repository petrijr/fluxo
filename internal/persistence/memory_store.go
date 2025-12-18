package persistence

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/petrijr/fluxo/pkg/api"
)

// InMemoryStore is a simple, goroutine-safe implementation of
// WorkflowStore and InstanceStore backed by maps.
type InMemoryStore struct {
	mu        sync.RWMutex
	workflows map[string]map[string]api.WorkflowDefinition
	instances map[string]*api.WorkflowInstance
	events    map[string][]api.WorkflowEvent
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		workflows: make(map[string]map[string]api.WorkflowDefinition),
		instances: make(map[string]*api.WorkflowInstance),
		events:    make(map[string][]api.WorkflowEvent),
	}
}

// Ensure InMemoryStore implements the interfaces.
var _ WorkflowStore = (*InMemoryStore)(nil)
var _ InstanceStore = (*InMemoryStore)(nil)
var _ EventStore = (*InMemoryStore)(nil)

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

// TryAcquireLease implements a simple in-memory lease using timestamps.
func (s *InMemoryStore) TryAcquireLease(ctx context.Context, instanceID, owner string, ttl time.Duration) (bool, error) {
	_ = ctx
	now := time.Now()
	expires := now.Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[instanceID]
	if !ok {
		return false, ErrInstanceNotFound
	}

	if inst.LeaseOwner == "" || inst.LeaseExpiresAt.IsZero() || !inst.LeaseExpiresAt.After(now) || inst.LeaseOwner == owner {
		inst.LeaseOwner = owner
		inst.LeaseExpiresAt = expires
		return true, nil
	}

	return false, nil
}

func (s *InMemoryStore) RenewLease(ctx context.Context, instanceID, owner string, ttl time.Duration) error {
	_ = ctx
	now := time.Now()
	expires := now.Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[instanceID]
	if !ok {
		return ErrInstanceNotFound
	}
	if inst.LeaseOwner != owner {
		return api.ErrWorkflowInstanceLocked
	}
	inst.LeaseExpiresAt = expires
	return nil
}

func (s *InMemoryStore) ReleaseLease(ctx context.Context, instanceID, owner string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[instanceID]
	if !ok {
		return ErrInstanceNotFound
	}
	if inst.LeaseOwner != "" && inst.LeaseOwner != owner {
		return api.ErrWorkflowInstanceLocked
	}
	inst.LeaseOwner = ""
	inst.LeaseExpiresAt = time.Time{}
	return nil
}

// AppendEvent appends an event to the in-memory history.
func (s *InMemoryStore) AppendEvent(ctx context.Context, ev api.WorkflowEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[ev.InstanceID] = append(s.events[ev.InstanceID], ev)
	return nil
}

// ListEvents returns all events for an instance in chronological order.
func (s *InMemoryStore) ListEvents(ctx context.Context, instanceID string) ([]api.WorkflowEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	evs := s.events[instanceID]
	out := make([]api.WorkflowEvent, len(evs))
	copy(out, evs)
	return out, nil
}
