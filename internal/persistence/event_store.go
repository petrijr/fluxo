package persistence

import (
	"context"

	"github.com/petrijr/fluxo/pkg/api"
)

// EventStore is an append-only history store for workflow execution events.
type EventStore interface {
	AppendEvent(ctx context.Context, ev api.WorkflowEvent) error
	ListEvents(ctx context.Context, instanceID string) ([]api.WorkflowEvent, error)
}

// NoopEventStore discards all events.
type NoopEventStore struct{}

func (NoopEventStore) AppendEvent(ctx context.Context, ev api.WorkflowEvent) error { return nil }
func (NoopEventStore) ListEvents(ctx context.Context, instanceID string) ([]api.WorkflowEvent, error) {
	return nil, nil
}
