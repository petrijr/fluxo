package taskqueue

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

// InMemoryQueue is a simple Queue implementation that supports task leases.
// It is safe for concurrent use.
//
// Semantics:
//   - Enqueue appends a task.
//   - Dequeue claims a runnable task by leasing it to an owner for leaseTTL.
//   - Ack removes a leased task.
//   - Nack releases a leased task back to the ready queue (optionally rescheduling).
type InMemoryQueue struct {
	mu sync.Mutex

	nextID int64

	ready    []Task
	inflight map[string]leasedTask
}

// Ensure InMemoryQueue implements Queue.
var _ Queue = (*InMemoryQueue)(nil)

type leasedTask struct {
	task   Task
	owner  string
	expiry time.Time
}

// NewInMemoryQueue creates a new queue with the given capacity.
// Capacity is kept for API compatibility but is not a hard limit in this implementation.
func NewInMemoryQueue() *InMemoryQueue {
	return &InMemoryQueue{
		ready:    make([]Task, 0, 128),
		inflight: make(map[string]leasedTask),
	}
}

func (q *InMemoryQueue) Enqueue(ctx context.Context, t Task) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if t.ID == "" {
		q.nextID++
		t.ID = strconv.FormatInt(q.nextID, 10)
	}
	q.ready = append(q.ready, t)
	return nil
}

func (q *InMemoryQueue) Dequeue(ctx context.Context, owner string, leaseTTL time.Duration) (*Task, error) {
	if leaseTTL <= 0 {
		return nil, errors.New("leaseTTL must be > 0")
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		now := time.Now()

		q.mu.Lock()

		// Requeue expired inflight tasks.
		for id, lt := range q.inflight {
			if !lt.expiry.After(now) {
				delete(q.inflight, id)
				q.ready = append(q.ready, lt.task)
			}
		}

		// Find first runnable task.
		idx := -1
		for i := range q.ready {
			if q.ready[i].NotBefore.IsZero() || !q.ready[i].NotBefore.After(now) {
				idx = i
				break
			}
		}
		if idx == -1 {
			q.mu.Unlock()
			// Nothing runnable right now.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Millisecond):
				continue
			}
		}

		t := q.ready[idx]
		// remove from ready
		q.ready = append(q.ready[:idx], q.ready[idx+1:]...)

		q.inflight[t.ID] = leasedTask{
			task:   t,
			owner:  owner,
			expiry: now.Add(leaseTTL),
		}

		q.mu.Unlock()
		return &t, nil
	}
}

func (q *InMemoryQueue) RenewLease(ctx context.Context, taskID, owner string, leaseTTL time.Duration) error {
	_ = ctx
	if leaseTTL <= 0 {
		return errors.New("leaseTTL must be > 0")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	lt, ok := q.inflight[taskID]
	if !ok {
		return errors.New("task not inflight")
	}
	if lt.owner != owner {
		return errors.New("task leased by another owner")
	}
	lt.expiry = time.Now().Add(leaseTTL)
	q.inflight[taskID] = lt
	return nil
}

func (q *InMemoryQueue) Ack(ctx context.Context, taskID string, owner string) error {
	_ = ctx
	q.mu.Lock()
	defer q.mu.Unlock()

	lt, ok := q.inflight[taskID]
	if !ok {
		return nil // idempotent
	}
	if lt.owner != owner {
		return errors.New("task leased by another owner")
	}
	delete(q.inflight, taskID)
	return nil
}

func (q *InMemoryQueue) Nack(ctx context.Context, taskID string, owner string, notBefore time.Time, attempts int) error {
	_ = ctx
	q.mu.Lock()
	defer q.mu.Unlock()

	lt, ok := q.inflight[taskID]
	if !ok {
		return nil
	}
	if lt.owner != owner {
		return errors.New("task leased by another owner")
	}
	delete(q.inflight, taskID)

	t := lt.task
	t.NotBefore = notBefore
	t.Attempts = attempts
	q.ready = append(q.ready, t)
	return nil
}

func (q *InMemoryQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.ready) + len(q.inflight)
}
