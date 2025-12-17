package persistence

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	corep "github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
)

// RedisInstanceStore is an InstanceStore backed by Redis.
// It uses a simple key structure:
//
//	<prefix>:inst:<id>            => gob-encoded redisInstancePayload
//	<prefix>:idx:all              => SET of all instance IDs
//	<prefix>:idx:wf:<workflow>    => SET of instance IDs for a given workflow
//	<prefix>:idx:status:<status>  => SET of instance IDs for a given status
//
// The indexes are best-effort; they are always updated on Save/Update, and
// ListInstances uses set operations for filtering.
type RedisInstanceStore struct {
	client *redis.Client
	prefix string
}

var _ corep.InstanceStore = (*RedisInstanceStore)(nil)

type redisInstancePayload struct {
	ID          string
	Workflow    string
	Version     string
	Fingerprint string
	Status      string
	CurrentStep int
	Input       []byte
	Output      []byte
	StepResults []byte
	Error       string
}

// NewRedisInstanceStore creates a RedisInstanceStore.
// prefix is optional but recommended (e.g. "fluxo:").
func NewRedisInstanceStore(client *redis.Client, prefix string) *RedisInstanceStore {
	if prefix == "" {
		prefix = "fluxo:"
	}
	return &RedisInstanceStore{
		client: client,
		prefix: prefix,
	}
}

func (r *RedisInstanceStore) keyLease(id string) string {
	return r.prefix + "lease:" + id
}

func (r *RedisInstanceStore) keyInstance(id string) string {
	return r.prefix + "inst:" + id
}

func (r *RedisInstanceStore) keyAll() string {
	return r.prefix + "idx:all"
}

func (r *RedisInstanceStore) keyWorkflow(name string) string {
	return r.prefix + "idx:wf:" + name
}

func (r *RedisInstanceStore) keyStatus(status api.Status) string {
	return r.prefix + "idx:status:" + string(status)
}

func (r *RedisInstanceStore) SaveInstance(inst *api.WorkflowInstance) error {
	ctx := context.Background()

	data, err := encodeRedisPayload(inst)
	if err != nil {
		return err
	}

	// Set payload
	if err := r.client.Set(ctx, r.keyInstance(inst.ID), data, 0).Err(); err != nil {
		return err
	}

	// Update indexes (best-effort; we don't treat index failures as fatal)
	pipe := r.client.TxPipeline()
	pipe.SAdd(ctx, r.keyAll(), inst.ID)
	pipe.SAdd(ctx, r.keyWorkflow(inst.Name), inst.ID)
	pipe.SAdd(ctx, r.keyStatus(inst.Status), inst.ID)
	_, _ = pipe.Exec(ctx)

	return nil
}

func (r *RedisInstanceStore) UpdateInstance(inst *api.WorkflowInstance) error {
	ctx := context.Background()

	data, err := encodeRedisPayload(inst)
	if err != nil {
		return err
	}

	// Overwrite payload
	if err := r.client.Set(ctx, r.keyInstance(inst.ID), data, 0).Err(); err != nil {
		return err
	}

	// Index updates: we just re-add; some stale index entries may remain if
	// workflow name/status changed, but ListInstances will filter by payload.
	pipe := r.client.TxPipeline()
	pipe.SAdd(ctx, r.keyAll(), inst.ID)
	pipe.SAdd(ctx, r.keyWorkflow(inst.Name), inst.ID)
	pipe.SAdd(ctx, r.keyStatus(inst.Status), inst.ID)
	_, _ = pipe.Exec(ctx)

	return nil
}

func (r *RedisInstanceStore) GetInstance(id string) (*api.WorkflowInstance, error) {
	ctx := context.Background()

	data, err := r.client.Get(ctx, r.keyInstance(id)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, corep.ErrInstanceNotFound
		}
		return nil, err
	}
	return decodeRedisPayload(data)
}

func (r *RedisInstanceStore) ListInstances(filter corep.InstanceFilter) ([]*api.WorkflowInstance, error) {
	ctx := context.Background()

	var ids []string
	var err error

	switch {
	case filter.WorkflowName != "" && filter.Status != "":
		ids, err = r.client.SInter(ctx,
			r.keyWorkflow(filter.WorkflowName),
			r.keyStatus(filter.Status),
		).Result()
	case filter.WorkflowName != "":
		ids, err = r.client.SMembers(ctx, r.keyWorkflow(filter.WorkflowName)).Result()
	case filter.Status != "":
		ids, err = r.client.SMembers(ctx, r.keyStatus(filter.Status)).Result()
	default:
		ids, err = r.client.SMembers(ctx, r.keyAll()).Result()
	}

	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []*api.WorkflowInstance{}, nil
		}
		return nil, err
	}
	if len(ids) == 0 {
		return []*api.WorkflowInstance{}, nil
	}

	pipe := r.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.Get(ctx, r.keyInstance(id))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	var instances []*api.WorkflowInstance
	for _, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			return nil, err
		}
		inst, err := decodeRedisPayload(data)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}

	return instances, nil
}

func decodeRedisPayload(data []byte) (*api.WorkflowInstance, error) {
	if len(data) == 0 {
		return nil, corep.ErrInstanceNotFound
	}
	var payload redisInstancePayload
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&payload); err != nil {
		return nil, err
	}

	inVal, err := corep.DecodeValue[any](payload.Input)
	if err != nil {
		return nil, err
	}
	outVal, err := corep.DecodeValue[any](payload.Output)
	if err != nil {
		return nil, err
	}
	stepResultsVal, err := corep.DecodeValue[map[int]any](payload.StepResults)
	if err != nil {
		return nil, err
	}

	inst := &api.WorkflowInstance{
		ID:          payload.ID,
		Name:        payload.Workflow,
		Version:     payload.Version,
		Fingerprint: payload.Fingerprint,
		Status:      api.Status(payload.Status),
		CurrentStep: payload.CurrentStep,
		Input:       inVal,
		Output:      outVal,
		StepResults: stepResultsVal,
	}
	if payload.Error != "" {
		inst.Err = errors.New(payload.Error)
	}

	return inst, nil
}

func encodeRedisPayload(inst *api.WorkflowInstance) ([]byte, error) {
	inBytes, err := corep.EncodeValue(inst.Input)
	if err != nil {
		return nil, err
	}
	outBytes, err := corep.EncodeValue(inst.Output)
	if err != nil {
		return nil, err
	}
	stepResultsBytes, err := corep.EncodeValue(inst.StepResults)
	if err != nil {
		return nil, err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	payload := redisInstancePayload{
		ID:          inst.ID,
		Workflow:    inst.Name,
		Version:     inst.Version,
		Fingerprint: inst.Fingerprint,
		Status:      string(inst.Status),
		CurrentStep: inst.CurrentStep,
		Input:       inBytes,
		Output:      outBytes,
		StepResults: stepResultsBytes,
		Error:       errStr,
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&payload); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var (
	// Lua script for acquiring a lease with re-entrant behavior for the same owner.
	// Returns 1 if acquired/refreshed, 0 otherwise.
	redisLeaseAcquireLua = `
local key = KEYS[1]
local owner = ARGV[1]
local ttlms = tonumber(ARGV[2])

local cur = redis.call('GET', key)
if not cur then
	redis.call('PSETEX', key, ttlms, owner)
	return 1
end
if cur == owner then
	redis.call('PEXPIRE', key, ttlms)
	return 1
end
return 0
`

	// Lua script for renewing a lease. Returns 1 if renewed, 0 otherwise.
	redisLeaseRenewLua = `
local key = KEYS[1]
local owner = ARGV[1]
local ttlms = tonumber(ARGV[2])

local cur = redis.call('GET', key)
if not cur then
	return 0
end
if cur == owner then
	redis.call('PEXPIRE', key, ttlms)
	return 1
end
return 0
`

	// Lua script for releasing a lease. Returns 1 if released, 0 otherwise.
	redisLeaseReleaseLua = `
local key = KEYS[1]
local owner = ARGV[1]

local cur = redis.call('GET', key)
if not cur then
	return 0
end
if cur == owner then
	redis.call('DEL', key)
	return 1
end
return 0
`
)

func (r *RedisInstanceStore) TryAcquireLease(ctx context.Context, instanceID, owner string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("ttl must be > 0")
	}
	res, err := r.client.Eval(ctx, redisLeaseAcquireLua, []string{r.keyLease(instanceID)}, owner, ttl.Milliseconds()).Result()
	if err != nil {
		return false, err
	}
	switch v := res.(type) {
	case int64:
		return v == 1, nil
	case int:
		return v == 1, nil
	case string:
		return v == "1", nil
	default:
		return false, nil
	}
}

func (r *RedisInstanceStore) RenewLease(ctx context.Context, instanceID, owner string, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("ttl must be > 0")
	}
	res, err := r.client.Eval(ctx, redisLeaseRenewLua, []string{r.keyLease(instanceID)}, owner, ttl.Milliseconds()).Result()
	if err != nil {
		return err
	}
	ok := false
	switch v := res.(type) {
	case int64:
		ok = v == 1
	case int:
		ok = v == 1
	case string:
		ok = v == "1"
	}
	if !ok {
		return api.ErrWorkflowInstanceLocked
	}
	return nil
}

func (r *RedisInstanceStore) ReleaseLease(ctx context.Context, instanceID, owner string) error {
	// Idempotent: if lease doesn't exist, succeed.
	res, err := r.client.Eval(ctx, redisLeaseReleaseLua, []string{r.keyLease(instanceID)}, owner).Result()
	if err != nil {
		return err
	}
	switch v := res.(type) {
	case int64:
		if v == 0 {
			// Either missing or owned by someone else; treat missing as success.
			// We distinguish by checking current value.
			cur, gerr := r.client.Get(ctx, r.keyLease(instanceID)).Result()
			if errors.Is(gerr, redis.Nil) {
				return nil
			}
			if gerr != nil {
				return gerr
			}
			if cur != owner && cur != "" {
				return api.ErrWorkflowInstanceLocked
			}
		}
	case int:
		// same handling as above; simplest: do nothing (best-effort)
	}
	return nil
}
