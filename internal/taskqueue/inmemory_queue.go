package taskqueue

import (
	"context"
)

// InMemoryQueue is a simple Queue implementation backed by a buffered channel.
// It is safe for concurrent use.
type InMemoryQueue struct {
	ch chan Task
}

// NewInMemoryQueue creates a new queue with the given capacity.
// For tests and small deployments, a modest capacity (e.g. 1024) is fine.
func NewInMemoryQueue(capacity int) *InMemoryQueue {
	if capacity <= 0 {
		capacity = 1024
	}
	return &InMemoryQueue{
		ch: make(chan Task, capacity),
	}
}

// Ensure InMemoryQueue implements Queue.
var _ Queue = (*InMemoryQueue)(nil)

func (q *InMemoryQueue) Enqueue(ctx context.Context, t Task) error {
	select {
	case q.ch <- t:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *InMemoryQueue) Dequeue(ctx context.Context) (*Task, error) {
	select {
	case t := <-q.ch:
		return &t, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *InMemoryQueue) Len() int {
	return len(q.ch)
}
