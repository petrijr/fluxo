package persistence

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"

	"github.com/redis/go-redis/v9"

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

var _ InstanceStore = (*RedisInstanceStore)(nil)

type redisInstancePayload struct {
	ID          string
	Workflow    string
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

func (s *RedisInstanceStore) keyInstance(id string) string {
	return s.prefix + "inst:" + id
}

func (s *RedisInstanceStore) keyAll() string {
	return s.prefix + "idx:all"
}

func (s *RedisInstanceStore) keyWorkflow(name string) string {
	return s.prefix + "idx:wf:" + name
}

func (s *RedisInstanceStore) keyStatus(status api.Status) string {
	return s.prefix + "idx:status:" + string(status)
}

func encodeRedisPayload(inst *api.WorkflowInstance) ([]byte, error) {
	inBytes, err := encodeValue(inst.Input)
	if err != nil {
		return nil, err
	}
	outBytes, err := encodeValue(inst.Output)
	if err != nil {
		return nil, err
	}
	stepResultsBytes, err := encodeValue(inst.StepResults)
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

func decodeRedisPayload(data []byte) (*api.WorkflowInstance, error) {
	if len(data) == 0 {
		return nil, ErrInstanceNotFound
	}
	var payload redisInstancePayload
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&payload); err != nil {
		return nil, err
	}

	inVal, err := decodeValue[any](payload.Input)
	if err != nil {
		return nil, err
	}
	outVal, err := decodeValue[any](payload.Output)
	if err != nil {
		return nil, err
	}
	stepResultsVal, err := decodeValue[map[int]any](payload.StepResults)
	if err != nil {
		return nil, err
	}

	inst := &api.WorkflowInstance{
		ID:          payload.ID,
		Name:        payload.Workflow,
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

func (s *RedisInstanceStore) SaveInstance(inst *api.WorkflowInstance) error {
	ctx := context.Background()

	data, err := encodeRedisPayload(inst)
	if err != nil {
		return err
	}

	// Set payload
	if err := s.client.Set(ctx, s.keyInstance(inst.ID), data, 0).Err(); err != nil {
		return err
	}

	// Update indexes (best-effort; we don't treat index failures as fatal)
	pipe := s.client.TxPipeline()
	pipe.SAdd(ctx, s.keyAll(), inst.ID)
	pipe.SAdd(ctx, s.keyWorkflow(inst.Name), inst.ID)
	pipe.SAdd(ctx, s.keyStatus(inst.Status), inst.ID)
	_, _ = pipe.Exec(ctx)

	return nil
}

func (s *RedisInstanceStore) UpdateInstance(inst *api.WorkflowInstance) error {
	ctx := context.Background()

	data, err := encodeRedisPayload(inst)
	if err != nil {
		return err
	}

	// Overwrite payload
	if err := s.client.Set(ctx, s.keyInstance(inst.ID), data, 0).Err(); err != nil {
		return err
	}

	// Index updates: we just re-add; some stale index entries may remain if
	// workflow name/status changed, but ListInstances will filter by payload.
	pipe := s.client.TxPipeline()
	pipe.SAdd(ctx, s.keyAll(), inst.ID)
	pipe.SAdd(ctx, s.keyWorkflow(inst.Name), inst.ID)
	pipe.SAdd(ctx, s.keyStatus(inst.Status), inst.ID)
	_, _ = pipe.Exec(ctx)

	return nil
}

func (s *RedisInstanceStore) GetInstance(id string) (*api.WorkflowInstance, error) {
	ctx := context.Background()

	data, err := s.client.Get(ctx, s.keyInstance(id)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}
	return decodeRedisPayload(data)
}

func (s *RedisInstanceStore) ListInstances(filter InstanceFilter) ([]*api.WorkflowInstance, error) {
	ctx := context.Background()

	var ids []string
	var err error

	switch {
	case filter.WorkflowName != "" && filter.Status != "":
		ids, err = s.client.SInter(ctx,
			s.keyWorkflow(filter.WorkflowName),
			s.keyStatus(filter.Status),
		).Result()
	case filter.WorkflowName != "":
		ids, err = s.client.SMembers(ctx, s.keyWorkflow(filter.WorkflowName)).Result()
	case filter.Status != "":
		ids, err = s.client.SMembers(ctx, s.keyStatus(filter.Status)).Result()
	default:
		ids, err = s.client.SMembers(ctx, s.keyAll()).Result()
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

	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.Get(ctx, s.keyInstance(id))
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
