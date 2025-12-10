package redis

import (
	"github.com/petrijr/fluxo"
	"github.com/redis/go-redis/v9"

	rqueue "github.com/petrijr/fluxo/redis/internal/taskqueue"
)

func NewRedisQueue(client *redis.Client, prefix string) fluxo.Queue {
	return rqueue.NewRedisQueue(client, prefix)
}
