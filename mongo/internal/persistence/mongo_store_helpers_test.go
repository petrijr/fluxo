package persistence

import (
	"context"
	"encoding/gob"
	"errors"
	"testing"
	"time"

	"github.com/petrijr/fluxo/mongo/internal/testutil"
	"github.com/stretchr/testify/suite"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDBStoreTestSuite struct {
	suite.Suite
	endpoint string
	store    *MongoInstanceStore
	client   *mongo.Client
	dbName   string
	collName string
}

func TestMongoDBTestSuite(t *testing.T) {
	gob.Register(mongoSamplePayload{})
	testsuite := new(MongoDBStoreTestSuite)
	testsuite.endpoint = testutil.GetMongoURI(t)
	newTestMongoStore(t, testsuite)
	suite.Run(t, testsuite)
}

func (m *MongoDBStoreTestSuite) SetupTest() {
	ctx := context.Background()
	coll := m.client.Database(m.dbName).Collection(m.collName)
	if err := coll.Drop(ctx); err != nil && !errors.Is(err, mongo.ErrNilDocument) {
		// Drop returns ErrNilDocument only in some odd cases; usually nil.
	}
}

type mongoSamplePayload struct {
	Msg string
	N   int
}

func newTestMongoStore(t *testing.T, ts *MongoDBStoreTestSuite) {
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
	ts.collName = "instances_test"

	ts.store = NewMongoInstanceStore(client, ts.dbName, ts.collName)
}
