package testutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	redisOnce      sync.Once
	redisContainer testcontainers.Container
	redisURI       string
	redisErr       error
)

func GetRedisAddress(t *testing.T) string {
	t.Helper()
	startRedisContainer(t)
	return redisURI
}

func startRedisContainer(t *testing.T) string {
	t.Helper()

	// Give generous timeout in CI environments
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	redisOnce.Do(func() {

		redisC, err := testcontainers.Run(
			ctx, "redis:latest",
			testcontainers.WithExposedPorts("6379/tcp"),
			testcontainers.WithWaitStrategy(
				wait.ForListeningPort("6379/tcp"),
				wait.ForLog("Ready to accept connections"),
			),
		)

		if err != nil {
			redisErr = err
			return
		}

		t.Cleanup(func() {
			testcontainers.CleanupContainer(t, redisC)
		})

		endpoint, err := redisC.Endpoint(ctx, "")
		if err != nil {
			_ = redisC.Terminate(context.Background()) // best-effort cleanup
			redisErr = err
			return
		}

		redisURI = endpoint
	})

	return redisURI
}
