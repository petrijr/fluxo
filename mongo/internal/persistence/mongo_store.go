package persistence

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	corep "github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
)

type MongoInstanceStore struct {
	coll *mongo.Collection
}

// Ensure it implements InstanceStore.
var _ corep.InstanceStore = (*MongoInstanceStore)(nil)

// NewMongoInstanceStore creates a Mongo-backed instance store.
// dbName defaults to "fluxo" if empty, collName defaults to "instances".
func NewMongoInstanceStore(client *mongo.Client, dbName, collName string) *MongoInstanceStore {
	if dbName == "" {
		dbName = "fluxo"
	}
	if collName == "" {
		collName = "instances"
	}

	return &MongoInstanceStore{
		coll: client.Database(dbName).Collection(collName),
	}
}

type mongoInstanceDoc struct {
	ID          string `bson:"_id"`
	Workflow    string `bson:"workflow_name"`
	Status      string `bson:"status"`
	CurrentStep int    `bson:"current_step"`
	Input       []byte `bson:"input,omitempty"`
	Output      []byte `bson:"output,omitempty"`
	StepResults []byte `bson:"step_results,omitempty"`
	Error       string `bson:"error,omitempty"`
}

func (s *MongoInstanceStore) SaveInstance(inst *api.WorkflowInstance) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	inBytes, err := corep.EncodeValue(inst.Input)
	if err != nil {
		return err
	}
	outBytes, err := corep.EncodeValue(inst.Output)
	if err != nil {
		return err
	}
	stepResultsBytes, err := corep.EncodeValue(inst.StepResults)
	if err != nil {
		return err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	doc := mongoInstanceDoc{
		ID:          inst.ID,
		Workflow:    inst.Name,
		Status:      string(inst.Status),
		CurrentStep: inst.CurrentStep,
		Input:       inBytes,
		Output:      outBytes,
		StepResults: stepResultsBytes,
		Error:       errStr,
	}

	_, err = s.coll.InsertOne(ctx, doc)
	// If duplicate ID happens, caller may treat it as an error; we just return it.
	return err
}

func (s *MongoInstanceStore) UpdateInstance(inst *api.WorkflowInstance) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	inBytes, err := corep.EncodeValue(inst.Input)
	if err != nil {
		return err
	}
	outBytes, err := corep.EncodeValue(inst.Output)
	if err != nil {
		return err
	}
	stepResultsBytes, err := corep.EncodeValue(inst.StepResults)
	if err != nil {
		return err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	update := bson.M{
		"$set": bson.M{
			"workflow_name": inst.Name,
			"status":        string(inst.Status),
			"current_step":  inst.CurrentStep,
			"input":         inBytes,
			"output":        outBytes,
			"step_results":  stepResultsBytes,
			"error":         errStr,
		},
	}

	res, err := s.coll.UpdateByID(ctx, inst.ID, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return corep.ErrInstanceNotFound
	}
	return nil
}

func (s *MongoInstanceStore) GetInstance(id string) (*api.WorkflowInstance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var doc mongoInstanceDoc
	err := s.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, corep.ErrInstanceNotFound
		}
		return nil, err
	}

	inVal, err := corep.DecodeValue[any](doc.Input)
	if err != nil {
		return nil, err
	}
	outVal, err := corep.DecodeValue[any](doc.Output)
	if err != nil {
		return nil, err
	}
	stepResultsVal, err := corep.DecodeValue[map[int]any](doc.StepResults)
	if err != nil {
		return nil, err
	}

	inst := &api.WorkflowInstance{
		ID:          doc.ID,
		Name:        doc.Workflow,
		Status:      api.Status(doc.Status),
		CurrentStep: doc.CurrentStep,
		Input:       inVal,
		Output:      outVal,
		StepResults: stepResultsVal,
	}
	if doc.Error != "" {
		inst.Err = errors.New(doc.Error)
	}
	return inst, nil
}

func (s *MongoInstanceStore) ListInstances(filter corep.InstanceFilter) ([]*api.WorkflowInstance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bfilter := bson.M{}
	if filter.WorkflowName != "" {
		bfilter["workflow_name"] = filter.WorkflowName
	}
	if filter.Status != "" {
		bfilter["status"] = string(filter.Status)
	}

	cur, err := s.coll.Find(ctx, bfilter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []*api.WorkflowInstance

	for cur.Next(ctx) {
		var doc mongoInstanceDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}

		inVal, err := corep.DecodeValue[any](doc.Input)
		if err != nil {
			return nil, err
		}
		outVal, err := corep.DecodeValue[any](doc.Output)
		if err != nil {
			return nil, err
		}
		stepResultsVal, err := corep.DecodeValue[map[int]any](doc.StepResults)
		if err != nil {
			return nil, err
		}

		inst := &api.WorkflowInstance{
			ID:          doc.ID,
			Name:        doc.Workflow,
			Status:      api.Status(doc.Status),
			CurrentStep: doc.CurrentStep,
			Input:       inVal,
			Output:      outVal,
			StepResults: stepResultsVal,
		}
		if doc.Error != "" {
			inst.Err = errors.New(doc.Error)
		}
		results = append(results, inst)
	}

	if err := cur.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
