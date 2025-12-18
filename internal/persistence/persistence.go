package persistence

// Persistence bundles the two store interfaces so the engine
// can depend on a single abstraction.
type Persistence struct {
	Workflows WorkflowStore
	Instances InstanceStore
	Events    EventStore
}
