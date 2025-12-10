package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	mongoOnce      sync.Once
	mongoContainer testcontainers.Container
	mongoURI       string
	mongoErr       error
)

func GetMongoURI(t *testing.T) string {
	t.Helper()
	startMongoContainerOnce(t)
	return mongoURI
}

func startMongoContainerOnce(t *testing.T) string {
	t.Helper()

	mongoOnce.Do(func() {
		// Give generous timeout in CI environments
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		mongoC, err := testcontainers.Run(
			ctx, "mongo:7",
			testcontainers.WithExposedPorts("27017/tcp"),
			testcontainers.WithWaitStrategy(
				wait.ForListeningPort("27017/tcp"),
				wait.ForLog("mongod startup complete"),
			),
		)

		if err != nil {
			mongoErr = err
			return
		}

		t.Cleanup(func() {
			testcontainers.CleanupContainer(t, mongoC)
		})

		endpoint, err := mongoC.Endpoint(ctx, "")
		if err != nil {
			_ = mongoC.Terminate(context.Background()) // best-effort cleanup
			mongoErr = err
			return
		}

		mongoURI = fmt.Sprintf("mongodb://%s", endpoint)
	})

	return mongoURI
}
