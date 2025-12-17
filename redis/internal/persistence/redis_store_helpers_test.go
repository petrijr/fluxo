package persistence

import (
	"context"
	"encoding/gob"
	"testing"

	corep "github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/redis/internal/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"
)

const prefix = "fluxo:test:"

type RedisStoreTestSuite struct {
	suite.Suite
	endpoint string
	store    corep.InstanceStore
	client   *redis.Client
	ctx      context.Context
}

func TestRedisTestSuite(t *testing.T) {
	gob.Register(redisSamplePayload{})
	testsuite := new(RedisStoreTestSuite)
	testsuite.endpoint = testutil.GetRedisAddress(t)
	initTestRedisStore(t, testsuite)
	suite.Run(t, testsuite)
}

func (r *RedisStoreTestSuite) SetupTest() {
	ctx := context.Background()

	// Clean up all keys with this prefix.
	iter := r.client.Scan(ctx, 0, prefix+"*", 0).Iterator()
	for iter.Next(ctx) {
		err := r.client.Del(ctx, iter.Val()).Err()
		r.NoErrorf(err, "redis DEL %q failed: %v", iter.Val(), err)
	}
	r.NoError(iter.Err(), "redis SCAN failed")
}

type redisSamplePayload struct {
	Msg string
	N   int
}

// initTestRedisStore connects to Redis using the address given in testSuite-argument.
// If fills the testSuite with InstanceStore backed by Redis, using a test-specific prefix.
// It also clears any keys under that prefix to ensure a clean slate.
func initTestRedisStore(t *testing.T, ts *RedisStoreTestSuite) {
	t.Helper()

	if ts == nil {
		t.FailNow()
	}
	client := redis.NewClient(&redis.Options{
		Addr: ts.endpoint,
	})
	t.Cleanup(func() {
		_ = client.Close()
	})
	ts.client = client

	ctx := context.Background()
	ts.ctx = ctx
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping failed: %v", err)
	}

	store := NewRedisInstanceStore(client, prefix)
	ts.store = store
}
