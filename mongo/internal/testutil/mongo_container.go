package testutil

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	mongoOnce sync.Once
	mongoURI  string
	mongoErr  error
)

// GetMongoURI returns the MongoDB URI for a shared Testcontainers Mongo instance.
// If the container cannot be started (e.g. Docker not available), tests are skipped.
func GetMongoURI(t *testing.T) string {
	t.Helper()

	mongoOnce.Do(func() {
		mongoURI, mongoErr = startMongoContainer()
	})

	if mongoErr != nil {
		t.Skipf("skipping Mongo tests: %v", mongoErr)
	}

	println(mongoURI)

	return mongoURI
}

func startMongoContainer() (string, error) {
	// On Windows, Testcontainers requires Docker Desktop (rootless is not supported).
	if runtime.GOOS == "windows" {
		// We still try; if it panics, we catch it below.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Guard against Testcontainers panicking (e.g. rootless Docker on Windows).
	defer func() {
		if r := recover(); r != nil {
			mongoErr = fmt.Errorf("starting MongoDB testcontainer panicked: %v", r)
		}
	}()

	mongoC, err := testcontainers.Run(
		ctx, "mongo:7",
		testcontainers.WithExposedPorts("27017/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("27017/tcp").
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		return "", fmt.Errorf("failed to start MongoDB testcontainer: %w", err)
	}

	// IMPORTANT: we DO NOT tie cleanup to any specific test via t.Cleanup.
	// Let Docker/Testcontainers clean up at process exit, or add a TestMain
	// if you want explicit teardown.

	host, err := mongoC.Host(ctx)
	if err != nil {
		_ = mongoC.Terminate(context.Background())
		return "", fmt.Errorf("failed to get MongoDB container host: %w", err)
	}

	port, err := mongoC.MappedPort(ctx, "27017/tcp")
	if err != nil {
		_ = mongoC.Terminate(context.Background())
		return "", fmt.Errorf("failed to get MongoDB container mapped port: %w", err)
	}

	// Force IPv4 loopback if host is localhost/::1 to avoid [::1]:port problems.
	if host == "" || host == "localhost" || host == "::1" {
		host = "127.0.0.1"
	}

	uri := fmt.Sprintf("mongodb://%s:%s", host, port.Port())
	return uri, nil
}
