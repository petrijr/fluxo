package main

import (
	"context"
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/petrijr/fluxo"
	_ "modernc.org/sqlite"
)

// ApprovalRequest is the input payload for the approval workflow.
type ApprovalRequest struct {
	RequestID string  `json:"request_id"`
	Requester string  `json:"requester"`
	Amount    float64 `json:"amount"`
}

// We need to register ApprovalRequest so it can be gob-encoded inside
// StartWorkflowPayload and persisted in the SQLite task queue.
func init() {
	gob.Register(ApprovalRequest{})
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Single SQLite DB shared by eng and task queue.
	db, err := sql.Open("sqlite", "file:approval_http.db?mode=memory&cache=shared")
	if err != nil {
		log.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()

	eng, err := fluxo.NewSQLiteEngine(db)
	if err != nil {
		log.Fatalf("NewSQLiteEngine failed: %v", err)
	}

	queue, err := fluxo.NewSQLiteQueue(db)
	if err != nil {
		log.Fatalf("NewSQLiteQueue failed: %v", err)
	}

	// Worker with:
	// - No task-level retries (MaxAttempts=1)
	// - Auto-timeout for approval after 30s
	w := fluxo.NewWorkerWithConfig(eng, queue, fluxo.Config{
		MaxAttempts:          1,
		Backoff:              0,
		DefaultSignalTimeout: 30 * time.Second,
	})

	if err := registerApprovalWorkflow(eng); err != nil {
		log.Fatalf("registerApprovalWorkflow failed: %v", err)
	}

	// Start background worker loop.
	go runWorkerLoop(ctx, w)

	// HTTP routes.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /request", func(wr http.ResponseWriter, r *http.Request) {
		handleCreateRequest(ctx, eng, w, wr, r)
	})
	mux.HandleFunc("POST /approve/", func(wr http.ResponseWriter, r *http.Request) {
		handleApprove(ctx, eng, w, wr, r)
	})
	mux.HandleFunc("GET /instances", func(wr http.ResponseWriter, r *http.Request) {
		handleListInstances(ctx, eng, wr, r)
	})
	mux.HandleFunc("GET /instances/", func(wr http.ResponseWriter, r *http.Request) {
		handleGetInstanceByRequestID(ctx, eng, wr, r)
	})

	addr := ":8080"
	log.Printf("HTTP approval service listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("ListenAndServe failed: %v", err)
	}
}

// registerApprovalWorkflow defines the purchase-approval workflow:
//
//	prepare -> wait-approval -> finalize
//
// finalize distinguishes between timeout vs normal approval.
func registerApprovalWorkflow(engine fluxo.Engine) error {
	wf := fluxo.WorkflowDefinition{
		Name: "purchase-approval",
		Steps: []fluxo.StepDefinition{
			{
				Name: "prepare",
				Fn: func(ctx context.Context, input any) (any, error) {
					req, ok := input.(ApprovalRequest)
					if !ok {
						return nil, fmt.Errorf("expected ApprovalRequest, got %T", input)
					}
					log.Printf("[prepare] request_id=%s requester=%s amount=%.2f",
						req.RequestID, req.Requester, req.Amount)
					return req, nil
				},
			},
			{
				Name: "wait-approval",
				Fn:   fluxo.WaitForSignalStep("approve"),
			},
			{
				Name: "finalize",
				Fn: func(ctx context.Context, input any) (any, error) {
					switch v := input.(type) {
					case fluxo.TimeoutPayload:
						log.Printf("[finalize] request timed out: %s", v.Reason)
						return map[string]any{
							"status": "timeout",
							"reason": v.Reason,
						}, nil
					case string:
						log.Printf("[finalize] request approved: %s", v)
						return map[string]any{
							"status":      "approved",
							"approved_by": v,
						}, nil
					default:
						return nil, fmt.Errorf("unexpected finalize input type %T", input)
					}
				},
			},
		},
	}
	return engine.RegisterWorkflow(wf)
}

// runWorkerLoop continuously processes tasks from the queue.
func runWorkerLoop(ctx context.Context, w *fluxo.Worker) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		processed, err := w.ProcessOne(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("[worker] ProcessOne error: %v", err)
			continue
		}
		if !processed {
			// ProcessOne with SQLiteQueue should block until a task is available,
			// so !processed should be rare. Avoid tight spin.
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// handleCreateRequest: POST /request
//
// Body JSON:
//
//	{
//	  "requester": "Alice",
//	  "amount": 123.45
//	}
//
// Response:
//
//	202 Accepted
//	{
//	  "request_id": "REQ-<timestamp>",
//	  "message": "request accepted"
//	}
func handleCreateRequest(ctx context.Context, engine fluxo.Engine, w *fluxo.Worker, wr http.ResponseWriter, r *http.Request) {
	type input struct {
		Requester string  `json:"requester"`
		Amount    float64 `json:"amount"`
	}
	var in input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(wr, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if in.Requester == "" {
		http.Error(wr, "requester is required", http.StatusBadRequest)
		return
	}
	if in.Amount <= 0 {
		http.Error(wr, "amount must be positive", http.StatusBadRequest)
		return
	}

	reqID := fmt.Sprintf("REQ-%d", time.Now().UnixNano())
	req := ApprovalRequest{
		RequestID: reqID,
		Requester: in.Requester,
		Amount:    in.Amount,
	}

	if err := w.EnqueueStartWorkflow(ctx, "purchase-approval", req); err != nil {
		log.Printf("EnqueueStartWorkflow failed: %v", err)
		http.Error(wr, "failed to enqueue workflow", http.StatusInternalServerError)
		return
	}

	wr.Header().Set("Content-Type", "application/json")
	wr.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(wr).Encode(map[string]any{
		"request_id": reqID,
		"message":    "request accepted",
	})
}

// handleApprove: POST /approve/{requestID}
//
// Body JSON:
//
//	{ "approved_by": "ManagerName" }
//
// It finds a WAITING instance for that requestID and enqueues an approval signal.
//
// Response:
//
//	202 Accepted or 404 Not Found
func handleApprove(ctx context.Context, engine fluxo.Engine, w *fluxo.Worker, wr http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/approve/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.Error(wr, "missing request ID", http.StatusBadRequest)
		return
	}
	requestID := parts[0]

	type input struct {
		ApprovedBy string `json:"approved_by"`
	}
	var in input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(wr, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if in.ApprovedBy == "" {
		http.Error(wr, "approved_by is required", http.StatusBadRequest)
		return
	}

	inst, err := findWaitingInstanceByRequestID(ctx, engine, requestID)
	if err != nil {
		if errors.Is(err, errInstanceNotFound) {
			http.Error(wr, "no waiting instance for this request ID", http.StatusNotFound)
			return
		}
		log.Printf("findWaitingInstanceByRequestID failed: %v", err)
		http.Error(wr, "internal error", http.StatusInternalServerError)
		return
	}

	if err := w.EnqueueSignal(ctx, inst.ID, "approve", in.ApprovedBy); err != nil {
		log.Printf("EnqueueSignal failed: %v", err)
		http.Error(wr, "failed to enqueue approval signal", http.StatusInternalServerError)
		return
	}

	wr.Header().Set("Content-Type", "application/json")
	wr.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(wr).Encode(map[string]any{
		"request_id":    requestID,
		"instance_id":   inst.ID,
		"message":       "approval signal enqueued",
		"current_state": inst.Status,
	})
}

// handleListInstances: GET /instances
//
// Optional query params:
//
//	?limit=10
//
// Response: list of all purchase-approval instances with basic fields.
func handleListInstances(ctx context.Context, engine fluxo.Engine, wr http.ResponseWriter, r *http.Request) {
	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	instances, err := engine.ListInstances(ctx, fluxo.InstanceListOptions{
		WorkflowName: "purchase-approval",
	})
	if err != nil {
		log.Printf("ListInstances failed: %v", err)
		http.Error(wr, "internal error", http.StatusInternalServerError)
		return
	}

	if len(instances) > limit {
		instances = instances[:limit]
	}

	type view struct {
		InstanceID  string       `json:"instance_id"`
		RequestID   string       `json:"request_id,omitempty"`
		Status      fluxo.Status `json:"status"`
		CurrentStep int          `json:"current_step"`
		Output      interface{}  `json:"output,omitempty"`
	}

	out := make([]view, 0, len(instances))
	for _, inst := range instances {
		v := view{
			InstanceID:  inst.ID,
			Status:      inst.Status,
			CurrentStep: inst.CurrentStep,
			Output:      inst.Output,
		}
		if req, ok := inst.Input.(ApprovalRequest); ok {
			v.RequestID = req.RequestID
		}
		out = append(out, v)
	}

	wr.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(wr).Encode(out)
}

// handleGetInstanceByRequestID: GET /instances/{requestID}
//
// Response:
//
//	200 with instance info, or 404.
func handleGetInstanceByRequestID(ctx context.Context, engine fluxo.Engine, wr http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/instances/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.Error(wr, "missing request ID", http.StatusBadRequest)
		return
	}
	requestID := parts[0]

	inst, err := findLatestInstanceByRequestID(ctx, engine, requestID)
	if err != nil {
		if errors.Is(err, errInstanceNotFound) {
			http.Error(wr, "no instance for this request ID", http.StatusNotFound)
			return
		}
		log.Printf("findLatestInstanceByRequestID failed: %v", err)
		http.Error(wr, "internal error", http.StatusInternalServerError)
		return
	}

	type view struct {
		InstanceID  string       `json:"instance_id"`
		RequestID   string       `json:"request_id,omitempty"`
		Status      fluxo.Status `json:"status"`
		CurrentStep int          `json:"current_step"`
		Output      interface{}  `json:"output,omitempty"`
	}
	v := view{
		InstanceID:  inst.ID,
		Status:      inst.Status,
		CurrentStep: inst.CurrentStep,
		Output:      inst.Output,
	}
	if req, ok := inst.Input.(ApprovalRequest); ok {
		v.RequestID = req.RequestID
	}

	wr.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(wr).Encode(v)
}

// Helper functions to locate instances by business key (RequestID).

var errInstanceNotFound = errors.New("instance not found")

// findWaitingInstanceByRequestID finds a WAITING purchase-approval instance whose
// Input is an ApprovalRequest with the given RequestID.
func findWaitingInstanceByRequestID(ctx context.Context, engine fluxo.Engine, requestID string) (*fluxo.WorkflowInstance, error) {
	instances, err := engine.ListInstances(ctx, fluxo.InstanceListOptions{
		WorkflowName: "purchase-approval",
	})
	if err != nil {
		return nil, err
	}

	for _, inst := range instances {
		if inst.Status != fluxo.StatusWaiting {
			continue
		}
		req, ok := inst.Input.(ApprovalRequest)
		if !ok {
			continue
		}
		if req.RequestID == requestID {
			return inst, nil
		}
	}

	return nil, errInstanceNotFound
}

// findLatestInstanceByRequestID finds the most recent instance for the given RequestID.
// (For this example we just return the first match; you can refine selection if needed.)
func findLatestInstanceByRequestID(ctx context.Context, engine fluxo.Engine, requestID string) (*fluxo.WorkflowInstance, error) {
	instances, err := engine.ListInstances(ctx, fluxo.InstanceListOptions{
		WorkflowName: "purchase-approval",
	})
	if err != nil {
		return nil, err
	}

	for _, inst := range instances {
		req, ok := inst.Input.(ApprovalRequest)
		if !ok {
			continue
		}
		if req.RequestID == requestID {
			return inst, nil
		}
	}

	return nil, errInstanceNotFound
}
