package taskqueue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

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
	client       *redis.Client
	readyKey     string
	inflightKey  string
	payloadKey   string
	notBeforeKey string
	ownerKey     string
}

// NewRedisQueue constructs a Redis-backed Queue.
// prefix is optional but recommended (e.g. "fluxo:").
func NewRedisQueue(client *redis.Client, prefix string) *RedisQueue {
	if prefix == "" {
		prefix = "fluxo:"
	}
	return &RedisQueue{
		client:       client,
		readyKey:     prefix + "tasks:ready",
		inflightKey:  prefix + "tasks:inflight",
		payloadKey:   prefix + "tasks:payload",
		notBeforeKey: prefix + "tasks:notbefore",
		ownerKey:     prefix + "tasks:owner",
	}
}

// Ensure RedisQueue implements Queue.
var _ coreq.Queue = (*RedisQueue)(nil)

// Enqueue pushes a task onto the Redis list (LPUSH).
func (q *RedisQueue) Enqueue(ctx context.Context, t coreq.Task) error {
	if t.ID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		t.ID = hex.EncodeToString(b)
	}
	data, err := coreq.EncodeTask(t)
	if err != nil {
		return err
	}
	nb := t.NotBefore
	if nb.IsZero() {
		nb = time.Now()
	}
	nbScore := float64(nb.UnixNano())

	pipe := q.client.TxPipeline()
	pipe.HSet(ctx, q.payloadKey, t.ID, data)
	pipe.HSet(ctx, q.notBeforeKey, t.ID, nb.UnixNano())
	pipe.ZAdd(ctx, q.readyKey, redis.Z{Score: nbScore, Member: t.ID})
	_, err = pipe.Exec(ctx)
	return err
}

// Dequeue blocks on BRPOP until a task is available or ctx is cancelled.
func (q *RedisQueue) Dequeue(ctx context.Context, owner string, leaseTTL time.Duration) (*coreq.Task, error) {
	if leaseTTL <= 0 {
		return nil, errors.New("leaseTTL must be > 0")
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		now := time.Now().UnixNano()
		leaseMs := leaseTTL.Milliseconds()

		res, err := q.client.Eval(
			ctx,
			redisDequeueLua,
			[]string{q.readyKey, q.inflightKey, q.payloadKey, q.notBeforeKey, q.ownerKey},
			now, leaseMs, owner,
		).Result()

		if err != nil {
			if errors.Is(err, redis.Nil) {
				// nothing runnable yet — wait/poll
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ticker.C:
					continue
				}
			}
			return nil, err
		}

		arr, ok := res.([]any)
		if !ok || len(arr) != 2 {
			// defensive: treat as empty
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
				continue
			}
		}

		id, _ := arr[0].(string)
		payloadStr, _ := arr[1].(string)
		if id == "" || payloadStr == "" {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
				continue
			}
		}

		task, err := coreq.DecodeTask([]byte(payloadStr))
		if err != nil {
			return nil, err
		}
		task.ID = id
		return task, nil
	}
}

var redisDequeueLua = `
-- KEYS: readyZ, inflightZ, payloadH, notBeforeH, ownerH
-- ARGV: nowNanos, leaseMs, owner
local readyZ = KEYS[1]
local inflightZ = KEYS[2]
local payloadH = KEYS[3]
local notBeforeH = KEYS[4]
local ownerH = KEYS[5]

local now = tonumber(ARGV[1])
local leaseMs = tonumber(ARGV[2])
local owner = ARGV[3]

-- Requeue up to 100 expired inflight tasks.
local expired = redis.call('ZRANGEBYSCORE', inflightZ, '-inf', now, 'LIMIT', 0, 100)
for i=1,#expired do
  local id = expired[i]
  redis.call('ZREM', inflightZ, id)
  redis.call('HDEL', ownerH, id)
  local nb = redis.call('HGET', notBeforeH, id)
  if nb then
    redis.call('ZADD', readyZ, tonumber(nb), id)
  else
    redis.call('ZADD', readyZ, now, id)
  end
end

-- Claim next runnable task.
local ids = redis.call('ZRANGEBYSCORE', readyZ, '-inf', now, 'LIMIT', 0, 1)
if #ids == 0 then
  return nil
end
local id = ids[1]
redis.call('ZREM', readyZ, id)
redis.call('ZADD', inflightZ, now + (leaseMs*1000000), id)
redis.call('HSET', ownerH, id, owner)

local payload = redis.call('HGET', payloadH, id)
return {id, payload}
`

var redisAckLua = `
-- KEYS: inflightZ, payloadH, notBeforeH, ownerH
-- ARGV: id, owner
local inflightZ = KEYS[1]
local payloadH = KEYS[2]
local notBeforeH = KEYS[3]
local ownerH = KEYS[4]

local id = ARGV[1]
local owner = ARGV[2]
local cur = redis.call('HGET', ownerH, id)
if not cur then
  return 1
end
if cur ~= owner then
  return 0
end

redis.call('ZREM', inflightZ, id)
redis.call('HDEL', ownerH, id)
redis.call('HDEL', payloadH, id)
redis.call('HDEL', notBeforeH, id)
return 1
`

var redisNackLua = `
-- KEYS: readyZ, inflightZ, notBeforeH, ownerH
-- ARGV: id, owner, notBeforeNanos
local readyZ = KEYS[1]
local inflightZ = KEYS[2]
local notBeforeH = KEYS[3]
local ownerH = KEYS[4]

local id = ARGV[1]
local owner = ARGV[2]
local nb = tonumber(ARGV[3])

local cur = redis.call('HGET', ownerH, id)
if not cur then
  return 1
end
if cur ~= owner then
  return 0
end

redis.call('ZREM', inflightZ, id)
redis.call('HDEL', ownerH, id)
redis.call('HSET', notBeforeH, id, nb)
redis.call('ZADD', readyZ, nb, id)
return 1
`
var redisRenewLua = `
-- KEYS: inflightZ, ownerH
-- ARGV: id, owner, newExpiryNanos
local inflightZ = KEYS[1]
local ownerH = KEYS[2]

local id = ARGV[1]
local owner = ARGV[2]
local expiry = tonumber(ARGV[3])

local cur = redis.call('HGET', ownerH, id)
if not cur then return 0 end
if cur ~= owner then return 0 end

redis.call('ZADD', inflightZ, expiry, id)
return 1
`

func (q *RedisQueue) RenewLease(ctx context.Context, taskID, owner string, leaseTTL time.Duration) error {
	expiry := time.Now().Add(leaseTTL).UnixNano()
	res, err := q.client.Eval(ctx, redisRenewLua,
		[]string{q.inflightKey, q.ownerKey},
		taskID, owner, expiry,
	).Result()
	if err != nil {
		return err
	}
	if n, ok := res.(int64); ok && n == 1 {
		return nil
	}
	return errors.New("task leased by another owner")
}

func (q *RedisQueue) Ack(ctx context.Context, taskID string, owner string) error {
	res, err := q.client.Eval(ctx, redisAckLua, []string{q.inflightKey, q.payloadKey, q.notBeforeKey, q.ownerKey}, taskID, owner).Result()
	if err != nil {
		return err
	}
	if n, ok := res.(int64); ok && n == 0 {
		return errors.New("task leased by another owner")
	}
	return nil
}

func (q *RedisQueue) Nack(ctx context.Context, taskID string, owner string, notBefore time.Time, attempts int) error {
	// Update payload attempts and schedule, then requeue via Lua.
	payloadStr, err := q.client.HGet(ctx, q.payloadKey, taskID).Result()
	if err != nil {
		// if missing, treat as success
		if errors.Is(err, redis.Nil) {
			return nil
		}
		return err
	}
	task, err := coreq.DecodeTask([]byte(payloadStr))
	if err != nil {
		return err
	}
	task.ID = taskID
	task.Attempts = attempts
	task.NotBefore = notBefore
	data, err := coreq.EncodeTask(*task)
	if err != nil {
		return err
	}
	if err := q.client.HSet(ctx, q.payloadKey, taskID, data).Err(); err != nil {
		return err
	}
	nb := notBefore.UnixNano()
	res, err := q.client.Eval(ctx, redisNackLua, []string{q.readyKey, q.inflightKey, q.notBeforeKey, q.ownerKey}, taskID, owner, nb).Result()
	if err != nil {
		return err
	}
	if n, ok := res.(int64); ok && n == 0 {
		return errors.New("task leased by another owner")
	}
	return nil
}

func (q *RedisQueue) Len() int {
	ctx := context.Background()
	r1, _ := q.client.ZCard(ctx, q.readyKey).Result()
	r2, _ := q.client.ZCard(ctx, q.inflightKey).Result()
	return int(r1 + r2)
}
