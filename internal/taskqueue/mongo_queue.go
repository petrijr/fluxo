package taskqueue

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoQueue implements Queue on top of MongoDB.
//
// Collection schema:
//
//	{
//	  _id:        string,    // task ID (if you use it)
//	  payload:    []byte,    // gob-encoded Task
//	  created_at: time.Time,
//	}
type MongoQueue struct {
	coll *mongo.Collection
}

// NewMongoQueue creates a Mongo-backed queue.
// dbName defaults to "fluxo", collName to "queue_tasks".
func NewMongoQueue(client *mongo.Client, dbName, collName string) *MongoQueue {
	if dbName == "" {
		dbName = "fluxo"
	}
	if collName == "" {
		collName = "queue_tasks"
	}
	return &MongoQueue{
		coll: client.Database(dbName).Collection(collName),
	}
}

// Ensure MongoQueue implements Queue.
var _ Queue = (*MongoQueue)(nil)

type mongoQueueDoc struct {
	ID        string    `bson:"_id"`
	Payload   []byte    `bson:"payload"`
	CreatedAt time.Time `bson:"created_at"`
}

// These are named differently to avoid clashing with any other encodeTask/decodeTask.
func mongoEncodeTask(t Task) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&t); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func mongoDecodeTask(data []byte) (*Task, error) {
	var t Task
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Enqueue inserts a document for the given Task.
func (q *MongoQueue) Enqueue(ctx context.Context, t Task) error {
	data, err := mongoEncodeTask(t)
	if err != nil {
		return err
	}

	doc := mongoQueueDoc{
		ID:        t.ID,
		Payload:   data,
		CreatedAt: time.Now().UTC(),
	}

	_, err = q.coll.InsertOne(ctx, doc)
	return err
}

// Dequeue blocks (via polling) until a task is available or ctx is cancelled.
func (q *MongoQueue) Dequeue(ctx context.Context) (*Task, error) {
	// Use a reusable timer to avoid allocating a new timer on every idle poll.
	// Initialize stopped; reset only when needed.
	tmr := time.NewTimer(0)
	if !tmr.Stop() {
		select {
		case <-tmr.C:
		default:
		}
	}
	defer tmr.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var doc mongoQueueDoc
		err := q.coll.FindOneAndDelete(
			ctx,
			bson.M{},
			&options.FindOneAndDeleteOptions{
				Sort: bson.D{{Key: "created_at", Value: 1}},
			},
		).Decode(&doc)

		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				// No tasks yet, wait a bit using a reusable timer.
				tmr.Reset(100 * time.Millisecond)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-tmr.C:
				}
				continue
			}
			return nil, err
		}

		return mongoDecodeTask(doc.Payload)
	}
}

// Len returns an approximate number of queued tasks.
func (q *MongoQueue) Len() int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	n, err := q.coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Printf("MongoQueue: Len failed: %v", err)
		return 0
	}
	return int(n)
}
