package engine

import (
	"fmt"
	"sync"

	"github.com/petrijr/fluxo/pkg/api"
)

type workflowRegistry struct {
	mu     sync.RWMutex
	byName map[string]map[string]api.WorkflowDefinition
}

func newWorkflowRegistry() *workflowRegistry {
	return &workflowRegistry{
		byName: make(map[string]map[string]api.WorkflowDefinition),
	}
}

func (r *workflowRegistry) Register(def api.WorkflowDefinition) error {
	if def.Version == "" {
		def.Version = "v1"
	}
	if def.Fingerprint == "" {
		def.Fingerprint = api.ComputeWorkflowFingerprint(def)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	versions := r.byName[def.Name]
	if versions == nil {
		versions = make(map[string]api.WorkflowDefinition)
		r.byName[def.Name] = versions
	}

	if _, exists := versions[def.Version]; exists {
		return fmt.Errorf("workflow %q version %q already registered", def.Name, def.Version)
	}

	versions[def.Version] = def
	return nil
}

func (r *workflowRegistry) Get(name, version string) (api.WorkflowDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions := r.byName[name]
	if versions == nil {
		return api.WorkflowDefinition{}, fmt.Errorf("workflow %q not found", name)
	}

	def, ok := versions[version]
	if !ok {
		return api.WorkflowDefinition{}, fmt.Errorf("workflow %q version %q not found", name, version)
	}

	return def, nil
}

func (r *workflowRegistry) Versions(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions := r.byName[name]
	out := make([]string, 0, len(versions))
	for v := range versions {
		out = append(out, v)
	}
	return out
}
