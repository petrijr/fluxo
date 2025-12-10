package mongo

import (
	"github.com/petrijr/fluxo/internal/engine"
	"github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
	"go.mongodb.org/mongo-driver/mongo"

	coree "github.com/petrijr/fluxo/internal/engine"
	mstore "github.com/petrijr/fluxo/mongo/internal/persistence"
)

// NewMongoEngine returns an Engine that persists instances in MongoDB, using
// the default database/collection names from the store (e.g. "fluxo"/"instances").
func NewMongoEngine(client *mongo.Client) api.Engine {
	return NewMongoEngineWithObserver(client, nil)
}

// NewMongoEngineWithObserver is the Mongo-backed engine constructor that accepts an Observer.
func NewMongoEngineWithObserver(client *mongo.Client, obs api.Observer) api.Engine {
	instStore := mstore.NewMongoInstanceStore(client, "", "")

	return engine.NewEngineWithConfig(coree.Config{
		Persistence: persistence.Persistence{
			Workflows: persistence.NewInMemoryStore(),
			Instances: instStore,
		},
		Observer: obs,
	})
}
