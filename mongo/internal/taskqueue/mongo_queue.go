package taskqueue

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"log"
	"time"

	coreq "github.com/petrijr/fluxo/internal/taskqueue"

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

func newTaskID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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
var _ coreq.Queue = (*MongoQueue)(nil)

type mongoQueueDoc struct {
	ID             string    `bson:"_id"`
	Payload        []byte    `bson:"payload"`
	CreatedAt      time.Time `bson:"created_at"`
	NotBefore      int64     `bson:"not_before"`
	Attempts       int       `bson:"attempts"`
	LeasedBy       string    `bson:"leased_by,omitempty"`
	LeaseExpiresAt int64     `bson:"lease_expires_at,omitempty"`
}

// These are named differently to avoid clashing with any other encodeTask/decodeTask.
func mongoEncodeTask(t coreq.Task) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&t); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func mongoDecodeTask(data []byte) (*coreq.Task, error) {
	var t coreq.Task
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Enqueue inserts a document for the given Task.
func (q *MongoQueue) Enqueue(ctx context.Context, t coreq.Task) error {
	data, err := mongoEncodeTask(t)
	if err != nil {
		return err
	}

	if t.ID == "" {
		t.ID = newTaskID()
	}
	nb := t.NotBefore
	if nb.IsZero() {
		nb = time.Now()
	}
	doc := mongoQueueDoc{
		ID:        t.ID,
		Payload:   data,
		CreatedAt: time.Now().UTC(),
		NotBefore: nb.UnixNano(),
		Attempts:  t.Attempts,
	}

	_, err = q.coll.InsertOne(ctx, doc)
	return err
}

// Dequeue blocks (via polling) until a task is available or ctx is cancelled.
func (q *MongoQueue) Dequeue(ctx context.Context, owner string, leaseTTL time.Duration) (*coreq.Task, error) {
	if leaseTTL <= 0 {
		return nil, errors.New("leaseTTL must be > 0")
	}

	// Reusable timer for polling when no tasks are available.
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

		now := time.Now().UTC()
		nowInt := now.UnixNano()
		expires := now.Add(leaseTTL).UnixNano()

		filter := bson.M{
			"not_before": bson.M{"$lte": nowInt},
			"$or": []bson.M{
				{"leased_by": bson.M{"$exists": false}},
				{"leased_by": ""},
				{"lease_expires_at": bson.M{"$lte": nowInt}},
				{"leased_by": owner},
			},
		}

		update := bson.M{"$set": bson.M{"leased_by": owner, "lease_expires_at": expires}}

		var doc mongoQueueDoc
		opts := options.FindOneAndUpdate().
			SetSort(bson.D{{Key: "not_before", Value: 1}, {Key: "created_at", Value: 1}}).
			SetReturnDocument(options.After)
		err := q.coll.FindOneAndUpdate(
			ctx,
			filter,
			update,
			opts,
		).Decode(&doc)

		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				// No tasks yet, wait a bit.
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

		task, err := mongoDecodeTask(doc.Payload)
		if err != nil {
			return nil, err
		}
		task.ID = doc.ID
		task.Attempts = doc.Attempts
		task.NotBefore = time.Unix(0, doc.NotBefore)
		return task, nil
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

func (q *MongoQueue) RenewLease(ctx context.Context, taskID, owner string, leaseTTL time.Duration) error {
	expiry := time.Now().Add(leaseTTL).UnixNano()
	res, err := q.coll.UpdateOne(
		ctx,
		bson.M{"_id": taskID, "leased_by": owner},
		bson.M{"$set": bson.M{"lease_expires_at": expiry}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errors.New("task leased by another owner")
	}
	return nil
}

func (q *MongoQueue) Ack(ctx context.Context, taskID string, owner string) error {
	_, err := q.coll.DeleteOne(ctx, bson.M{"_id": taskID, "leased_by": owner})
	return err
}

func (q *MongoQueue) Nack(ctx context.Context, taskID string, owner string, notBefore time.Time, attempts int) error {
	_, err := q.coll.UpdateOne(ctx,
		bson.M{"_id": taskID, "leased_by": owner},
		bson.M{"$set": bson.M{
			"leased_by":        "",
			"lease_expires_at": int64(0),
			"not_before":       notBefore.UnixNano(),
			"attempts":         attempts,
		}},
	)
	return err
}
