package mongo

import (
	"github.com/petrijr/fluxo"
	"go.mongodb.org/mongo-driver/mongo"

	mqueue "github.com/petrijr/fluxo/mongo/internal/taskqueue"
)

func NewMongoQueue(client *mongo.Client, dbName, collName string) fluxo.Queue {
	return mqueue.NewMongoQueue(client, dbName, collName)
}
