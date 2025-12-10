package taskqueue

import (
	"context"
	"testing"
	"time"

	coreq "github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/redis/internal/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"
)

type RedisQueueTestSuite struct {
	suite.Suite
	endpoint string
	client   *redis.Client
	ctx      context.Context
	queue    *RedisQueue
}

func TestRedisQueueSuite(t *testing.T) {
	testsuite := new(RedisQueueTestSuite)
	testsuite.endpoint = testutil.GetRedisAddress(t)
	initTestRedisQueue(t, testsuite)
	suite.Run(t, testsuite)
}

func (r *RedisQueueTestSuite) SetupTest() {
	ctx := context.Background()
	err := r.client.Del(ctx, r.queue.key).Err()
	r.NoErrorf(err, "redis DEL failed: %s", "formatted")
}

// newTestRedisQueue connects to Redis and creates a queue.
func initTestRedisQueue(t *testing.T, ts *RedisQueueTestSuite) {
	t.Helper()

	client := redis.NewClient(&redis.Options{
		Addr: ts.endpoint,
	})
	ts.client = client

	// quick ping
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ts.ctx = ctx

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping failed: %v", err)
	}

	q := NewRedisQueue(client, "fluxo:test:")

	t.Cleanup(func() {
		_ = client.Close()
	})

	ts.queue = q
}

func (r *RedisQueueTestSuite) TestRedisQueue_EnqueueDequeue() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start a simple worker goroutine that pulls one task.
	tasksCh := make(chan *coreq.Task, 1)
	errCh := make(chan error, 1)

	go func() {
		task, err := r.queue.Dequeue(ctx)
		if err != nil {
			errCh <- err
			return
		}
		tasksCh <- task
	}()

	// Allow worker to start and block on BRPop.
	time.Sleep(100 * time.Millisecond)

	// Enqueue a zero-valued Task (we don't depend on any fields here).
	err := r.queue.Enqueue(ctx, coreq.Task{})
	r.NoErrorf(err, "Enqueue failed: %v", "formatted")

	select {
	case err := <-errCh:
		r.Failf("Dequeue returned error", "Dequeue returned error: %v", err)
	case task := <-tasksCh:
		r.NotNil(task, "expected non-nil task from Dequeue")
	case <-time.After(3 * time.Second):
		r.Failf("timed out waiting for dequeued task", "timed out waiting for dequeued task")
	}

	if got := r.queue.Len(); got != 0 {
		r.Failf("invalid queue length", "expected queue length 0 after dequeue, got %d", got)
	}
}
