package persistence

import (
	"database/sql"
	"encoding/gob"
	"testing"

	"github.com/petrijr/fluxo/postgres/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type pgSamplePayload struct {
	Msg string
	N   int
}

type PostgresStoreTestSuite struct {
	suite.Suite
	endpoint string
	store    *PostgresInstanceStore
	db       *sql.DB
}

func TestPostgresLeaseTestSuite(t *testing.T) {
	gob.Register(pgSamplePayload{})
	testsuite := new(PostgresStoreTestSuite)
	testsuite.endpoint = testutil.GetPostgresEndpoint(t)
	initTestPostgresStore(t, testsuite)
	suite.Run(t, testsuite)
}

func (p *PostgresStoreTestSuite) SetupTest() {
	_, err := p.db.Exec("TRUNCATE TABLE instances")
	p.NoErrorf(err, "TRUNCATE instances failed %v", "formatted")
}

func initTestPostgresStore(t *testing.T, ts *PostgresStoreTestSuite) {
	t.Helper()

	db, err := sql.Open("pgx", ts.endpoint)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	ts.db = db

	store, err := NewPostgresInstanceStore(db)
	if err != nil {
		t.Fatalf("NewPostgresInstanceStore failed: %v", err)
	}
	ts.store = store
}
