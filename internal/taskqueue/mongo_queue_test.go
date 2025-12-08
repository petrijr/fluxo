package taskqueue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/petrijr/fluxo/internal/testutil"
	"github.com/stretchr/testify/suite"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoQueueTestSuite struct {
	suite.Suite
	endpoint string
	client   *mongo.Client
	ctx      context.Context
	queue    *MongoQueue
	dbName   string
	collName string
}

func TestMongoQueueTestSuite(t *testing.T) {
	testsuite := new(MongoQueueTestSuite)
	testsuite.endpoint = testutil.StartMongoDBContainer(t)
	initTestMongoQueue(t, testsuite)
	suite.Run(t, testsuite)
}

func (m *MongoQueueTestSuite) SetupTest() {
	ctx := context.Background()
	coll := m.client.Database(m.dbName).Collection(m.collName)
	if err := coll.Drop(ctx); err != nil && !errors.Is(err, mongo.ErrNilDocument) {
		// Drop returns ErrNilDocument only in some odd cases; usually nil.
	}
}

func initTestMongoQueue(t *testing.T, ts *MongoQueueTestSuite) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(ts.endpoint))
	if err != nil {
		t.Fatalf("mongo.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
	})
	ts.client = client
	ts.dbName = "fluxo_test"
	ts.collName = "queue_tasks_test"
	ts.queue = NewMongoQueue(client, ts.dbName, ts.collName)
}

func (m *MongoQueueTestSuite) TestMongoQueue_EnqueueDequeue() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tasksCh := make(chan *Task, 1)
	errCh := make(chan error, 1)

	// Worker goroutine: blocks on Dequeue until it gets a task or error.
	go func() {
		task, err := m.queue.Dequeue(ctx)
		if err != nil {
			errCh <- err
			return
		}
		tasksCh <- task
	}()

	// Give worker a moment to start and block on Dequeue.
	time.Sleep(100 * time.Millisecond)

	// Enqueue a minimal Task; we don't rely on any specific fields here.
	enqTask := Task{}
	if err := m.queue.Enqueue(ctx, enqTask); err != nil {
		m.NoErrorf(err, "Enqueue failed: %v", "formatted")
	}

	select {
	case err := <-errCh:
		m.Failf("Dequeue returned error", "Dequeue returned error: %v", err)
	case task := <-tasksCh:
		m.NotNil(task, "expected non-nil task from Dequeue")
	case <-time.After(3 * time.Second):
		m.Failf("timed out waiting for dequeued task", "timed out waiting for dequeued task")
	}

	if got := m.queue.Len(); got != 0 {
		m.Failf("invalid queue length", "expected queue length 0 after dequeue, got %d", got)
	}
}
