package redis

import (
	"github.com/petrijr/fluxo/internal/engine"
	"github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
	"github.com/redis/go-redis/v9"

	coree "github.com/petrijr/fluxo/internal/engine"
	corep "github.com/petrijr/fluxo/redis/internal/persistence"
)

// NewRedisEngine returns an Engine that persists instances in Redis.
func NewRedisEngine(client *redis.Client) api.Engine {
	return NewRedisEngineWithObserver(client, nil)
}

// NewRedisEngineWithObserver returns a Redis-backed Engine with the given Observer.
func NewRedisEngineWithObserver(client *redis.Client, obs api.Observer) api.Engine {
	instStore := corep.NewRedisInstanceStore(client, "fluxo:")

	return engine.NewEngineWithConfig(coree.Config{
		Persistence: persistence.Persistence{
			Workflows: persistence.NewInMemoryStore(),
			Instances: instStore,
		},
		Observer: obs,
	})
}
