package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	pgOnce      sync.Once
	pgContainer testcontainers.Container
	pgDSN       string
	pgErr       error
)

func GetPostgresEndpoint(t *testing.T) string {
	t.Helper()
	startPostgresOnce(t)
	return pgDSN
}

func startPostgresOnce(t *testing.T) string {
	t.Helper()

	pgOnce.Do(func() {
		// Give generous timeout in CI environments
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		postgresC, err := testcontainers.Run(
			ctx, "postgres:16",
			testcontainers.WithExposedPorts("5432/tcp"),
			testcontainers.WithWaitStrategy(
				wait.ForAll(
					// Container is listening
					wait.ForListeningPort("5432/tcp"),
					// Postgres reports readiness in logs
					wait.ForLog("ready to accept connections"),
					// Actively verify SQL connectivity with a simple query using DSN built from mapped host:port
					wait.ForSQL("5432/tcp", "pgx", func(host string, port nat.Port) string {
						return fmt.Sprintf("postgres://fluxo:fluxo@%s:%s/fluxo_test?sslmode=disable", host, port.Port())
					}).WithQuery("SELECT 1"),
				).WithDeadline(2*time.Minute),
			),
			testcontainers.WithEnv(map[string]string{
				"POSTGRES_USER":     "fluxo",
				"POSTGRES_PASSWORD": "fluxo",
				"POSTGRES_DB":       "fluxo_test",
			}),
		)

		if err != nil {
			pgErr = err
			return
		}

		t.Cleanup(func() {
			testcontainers.CleanupContainer(t, postgresC)
		})

		endpoint, err := postgresC.Endpoint(ctx, "")
		if err != nil {
			_ = postgresC.Terminate(context.Background()) // best-effort cleanup
			pgErr = err
			return
		}

		pgContainer = postgresC
		pgDSN = fmt.Sprintf("postgres://fluxo:fluxo@%s/fluxo_test?sslmode=disable", endpoint)
	})

	return pgDSN
}
