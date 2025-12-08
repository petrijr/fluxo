package testutil

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func StartPostgreSQLContainer(t *testing.T) string {
	ctx := context.Background()
	postgresC, err := testcontainers.Run(
		ctx, "postgres:16",
		testcontainers.WithExposedPorts("5432/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp"),
			wait.ForLog("ready to accept connections"),
		),
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_USER":     "fluxo",
			"POSTGRES_PASSWORD": "fluxo",
			"POSTGRES_DB":       "fluxo_test",
		}),
	)
	defer testcontainers.CleanupContainer(t, postgresC)
	require.NoError(t, err)

	endpoint, err := postgresC.Endpoint(ctx, "")
	if err != nil {
		t.Error(err)
	}

	return fmt.Sprintf("postgres://fluxo:fluxo@%s/fluxo_test?sslmode=disable", endpoint)
}

func StartMongoDBContainer(t *testing.T) string {
	ctx := context.Background()
	mongoC, err := testcontainers.Run(
		ctx, "mongo:7",
		testcontainers.WithExposedPorts("27017/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("27017/tcp"),
			wait.ForLog("mongod startup complete"),
		),
	)
	defer testcontainers.CleanupContainer(t, mongoC)
	require.NoError(t, err)

	endpoint, err := mongoC.Endpoint(ctx, "")
	if err != nil {
		t.Error(err)
	}

	return fmt.Sprintf("mongodb://%s", endpoint)
}

func StartRedisContainer(t *testing.T) string {
	ctx := context.Background()
	redisC, err := testcontainers.Run(
		ctx, "redis:latest",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("6379/tcp"),
			wait.ForLog("Ready to accept connections"),
		),
	)
	defer testcontainers.CleanupContainer(t, redisC)
	require.NoError(t, err)

	endpoint, err := redisC.Endpoint(ctx, "")
	if err != nil {
		t.Error(err)
	}

	return endpoint
}
