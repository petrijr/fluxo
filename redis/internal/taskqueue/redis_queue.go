package taskqueue

import (
	"context"
	"log"

	coreq "github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/redis/go-redis/v9"
)

// RedisQueue implements the Queue interface using Redis.
//
// It uses a single Redis list with key:
//
//	<prefix>:tasks
//
// Values are gob-encoded Task structs.
type RedisQueue struct {
	client *redis.Client
	key    string
}

// NewRedisQueue constructs a Redis-backed Queue.
// prefix is optional but recommended (e.g. "fluxo:").
func NewRedisQueue(client *redis.Client, prefix string) *RedisQueue {
	if prefix == "" {
		prefix = "fluxo:"
	}
	return &RedisQueue{
		client: client,
		key:    prefix + "tasks",
	}
}

// Ensure RedisQueue implements Queue.
var _ coreq.Queue = (*RedisQueue)(nil)

// Enqueue pushes a task onto the Redis list (LPUSH).
func (q *RedisQueue) Enqueue(ctx context.Context, t coreq.Task) error {
	data, err := coreq.EncodeTask(t)
	if err != nil {
		return err
	}
	return q.client.LPush(ctx, q.key, data).Err()
}

// Dequeue blocks on BRPOP until a task is available or ctx is cancelled.
func (q *RedisQueue) Dequeue(ctx context.Context) (*coreq.Task, error) {
	// BRPop returns [key, value]
	res, err := q.client.BRPop(ctx, 0, q.key).Result()
	if err != nil {
		// If ctx is cancelled, BRPop should return an error with ctx.Err().
		return nil, err
	}
	if len(res) != 2 {
		// Unexpected shape; log and try again.
		log.Printf("RedisQueue: BRPop returned unexpected result: %#v", res)
		return nil, nil
	}

	data := []byte(res[1])
	return coreq.DecodeTask(data)
}

// Len returns the approximate number of tasks queued (LLEN).
func (q *RedisQueue) Len() int {
	n, err := q.client.LLen(context.Background(), q.key).Result()
	if err != nil {
		// For a Len() helper, it's better to log and return 0 than panic.
		log.Printf("RedisQueue: Len failed: %v", err)
		return 0
	}
	// n is int64 from Redis, but Queue.Len() returns int.
	return int(n)
}
