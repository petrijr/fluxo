package taskqueue

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"github.com/petrijr/fluxo/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type PostgresQueueTestSuite struct {
	suite.Suite
	endpoint string
	queue    *PostgresQueue
	db       *sql.DB
}

func TestPostgresQueueSuite(t *testing.T) {
	testsuite := new(PostgresQueueTestSuite)
	testsuite.endpoint = testutil.StartPostgresContainer(t)
	initTestPostgresQueue(t, testsuite)
	suite.Run(t, testsuite)
}

func (p *PostgresQueueTestSuite) SetupTest() {
	_, err := p.db.Exec("TRUNCATE TABLE queue_tasks")
	p.NoError(err, "TRUNCATE queue_tasks failed: %v", "formatted")
}

// initTestPostgresQueue connects to Postgres and configures the test suite.
func initTestPostgresQueue(t *testing.T, ts *PostgresQueueTestSuite) {
	t.Helper()

	db, err := sql.Open("pgx", ts.endpoint)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	ts.db = db

	q, err := NewPostgresQueue(db)
	if err != nil {
		t.Fatalf("NewPostgresQueue failed: %v", err)
	}
	ts.queue = q
}

func (p *PostgresQueueTestSuite) TestPostgresQueue_EnqueueDequeue() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tasksCh := make(chan *Task, 1)
	errCh := make(chan error, 1)

	// Worker goroutine: blocks on Dequeue until it gets a task or error.
	go func() {
		task, err := p.queue.Dequeue(ctx)
		if err != nil {
			errCh <- err
			return
		}
		tasksCh <- task
	}()

	// Give worker a moment to start and block on Dequeue.
	time.Sleep(100 * time.Millisecond)

	// Enqueue a minimal Task. We don't rely on any particular fields here.
	enqTask := Task{}
	err := p.queue.Enqueue(ctx, enqTask)
	p.NoErrorf(err, "Enqueue failed: %v", "formatted")

	select {
	case err := <-errCh:
		p.Failf("Dequeue returned error", "Dequeue returned error: %v", err)
	case task := <-tasksCh:
		p.NotNil(task, "expected non-nil task from Dequeue")
	case <-time.After(3 * time.Second):
		p.Failf("timed out waiting for dequeued task", "timed out waiting for dequeued task")
	}

	if got := p.queue.Len(); got != 0 {
		p.Failf("invalid queue length", "expected queue length 0 after dequeue, got %d", got)
	}
}
