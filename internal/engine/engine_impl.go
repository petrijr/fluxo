package engine

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/api"
)

// engineImpl is a simple, synchronous, in-process engine implementation.
type engineImpl struct {
	workflows persistence.WorkflowStore
	instances persistence.InstanceStore

	engineID string
	leaseTTL time.Duration

	mu       sync.Mutex // only for nextID
	nextID   int64
	observer api.Observer
	queue    taskqueue.Queue
	events   persistence.EventStore
}

// Config describes how to construct an engineImpl.
// Only used inside this package; external callers use the helper functions.
type Config struct {
	Persistence persistence.Persistence
	Observer    api.Observer
	Queue       taskqueue.Queue
	// EngineID identifies this engine/worker for lease ownership.
	// If empty, a random ID is generated.
	EngineID string
	// LeaseTTL controls how long instance execution leases last.
	// If zero, a sensible default is used.
	LeaseTTL time.Duration
}

func randomEngineID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("engine-%d", time.Now().UnixNano())
	}
	return "engine-" + hex.EncodeToString(b)
}

func NewInMemoryEngine() api.Engine {
	return NewInMemoryEngineWithObserver(nil)
}

func NewInMemoryEngineWithObserver(obs api.Observer) api.Engine {
	mem := persistence.NewInMemoryStore()
	return NewEngineWithConfig(Config{
		Persistence: persistence.Persistence{
			Workflows: mem,
			Instances: mem,
			Events:    mem,
		},
		Observer: obs,
	})
}

func NewSQLiteEngine(db *sql.DB) (api.Engine, error) {
	return NewSQLiteEngineWithObserver(db, nil)
}

func NewSQLiteEngineWithObserver(db *sql.DB, obs api.Observer) (api.Engine, error) {
	inst, err := persistence.NewSQLiteInstanceStore(db)
	if err != nil {
		return nil, err
	}
	// Workflow definitions remain in-memory.
	memWF := persistence.NewInMemoryStore()
	evStore, err := persistence.NewSQLiteEventStore(db)
	if err != nil {
		return nil, err
	}

	return NewEngineWithConfig(Config{
		Persistence: persistence.Persistence{
			Workflows: memWF,
			Instances: inst,
			Events:    evStore,
		},
		Observer: obs,
	}), nil
}

// NewEngineWithConfig creates a new Engine using the given configuration.
func NewEngineWithConfig(cfg Config) api.Engine {
	obs := cfg.Observer
	if obs == nil {
		obs = api.NoopObserver{}
	}
	engineID := cfg.EngineID
	if engineID == "" {
		engineID = randomEngineID()
	}
	leaseTTL := cfg.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = 30 * time.Second
	}
	return &engineImpl{
		workflows: cfg.Persistence.Workflows,
		instances: cfg.Persistence.Instances,
		observer:  obs,
		engineID:  engineID,
		leaseTTL:  leaseTTL,
		queue:     cfg.Queue,
		events:    cfg.Persistence.Events,
	}
}

// NewEngine returns an Engine backed by an in-memory registry and
// an in-memory persistence store. External users access this via fluxo.NewInMemoryEngine.
func NewEngine(p persistence.Persistence) api.Engine {
	return NewEngineWithConfig(Config{
		Persistence: p,
	})
}

func (e *engineImpl) RegisterWorkflow(def api.WorkflowDefinition) error {
	if def.Name == "" {
		return errors.New("workflow name is required")
	}
	if len(def.Steps) == 0 {
		return errors.New("workflow must have at least one step")
	}

	// Default version for legacy callers/tests that omit it.
	if def.Version == "" {
		def.Version = "v1"
	}

	// Set fingerprint if not yet set
	if def.Fingerprint == "" {
		def.Fingerprint = api.ComputeWorkflowFingerprintStrict(def)
	}

	// Registration is idempotent per (name, version) as long as the fingerprint matches.
	existing, err := e.workflows.GetWorkflow(def.Name, def.Version)
	if err == nil && existing.Name != "" {
		if existing.Fingerprint == def.Fingerprint {
			return nil
		}
		return fmt.Errorf("%w: workflow %s@%s fingerprint differs (existing=%s new=%s)",
			api.ErrWorkflowDefinitionMismatch, def.Name, def.Version, existing.Fingerprint, def.Fingerprint)
	}
	if err != nil && !errors.Is(err, persistence.ErrWorkflowNotFound) {
		return err
	}

	return e.workflows.SaveWorkflow(def)
}

func (e *engineImpl) Run(ctx context.Context, name string, input any) (*api.WorkflowInstance, error) {
	def, err := e.workflows.GetLatestWorkflow(name)
	if err != nil {
		if errors.Is(err, persistence.ErrWorkflowNotFound) {
			// Distinguish "unknown workflow" from "ambiguous workflow (multiple versions)".
			if versions, verr := e.workflows.ListWorkflowVersions(name); verr == nil && len(versions) > 1 {
				return nil, fmt.Errorf("workflow %s has multiple registered versions %v; use RunVersion", name, versions)
			}
			return nil, fmt.Errorf("unknown workflow: %s", name)
		}
		return nil, err
	}

	inst := &api.WorkflowInstance{
		ID:          e.nextInstanceID(),
		Name:        def.Name,
		Version:     def.Version,
		Fingerprint: def.Fingerprint,
		Status:      api.StatusRunning,
		Input:       input,
		CurrentStep: 0,
		StepResults: make(map[int]any),
	}

	// Notify observer that the instance has started.
	e.observer.OnWorkflowStart(ctx, inst)

	// Persist the instance as soon as it starts.
	if err := e.instances.SaveInstance(inst); err != nil {
		inst.Status = api.StatusFailed
		inst.Err = err
		// Saving failed – treat this as a workflow failure.
		e.observer.OnWorkflowFailed(ctx, inst, err)
		return inst, err
	}

	e.emitEvent(ctx, inst, api.EventWorkflowStarted, -1, "")

	return e.executeSteps(ctx, def, inst, 0, input)
}

func (e *engineImpl) RunVersion(
	ctx context.Context,
	name string,
	version string,
	input any,
) (*api.WorkflowInstance, error) {

	def, err := e.workflows.GetWorkflow(name, version)
	if err != nil {
		return nil, err
	}

	inst := &api.WorkflowInstance{
		ID:          e.nextInstanceID(),
		Name:        def.Name,
		Version:     def.Version,
		Fingerprint: def.Fingerprint,
		Status:      api.StatusRunning,
		Input:       input,
		CurrentStep: 0,
		StepResults: make(map[int]any),
	}

	e.observer.OnWorkflowStart(ctx, inst)

	if err := e.instances.SaveInstance(inst); err != nil {
		inst.Status = api.StatusFailed
		inst.Err = err
		e.observer.OnWorkflowFailed(ctx, inst, err)
		return inst, err
	}

	return e.executeSteps(ctx, def, inst, 0, inst.Input)
}

// Start creates an instance and schedules it for execution if a queue is configured.
// If no queue is configured, it falls back to synchronous Run.
func (e *engineImpl) Start(ctx context.Context, name string, input any) (*api.WorkflowInstance, error) {
	if e.queue == nil {
		return e.Run(ctx, name, input)
	}
	def, err := e.workflows.GetLatestWorkflow(name)
	if err != nil {
		return nil, err
	}
	inst := &api.WorkflowInstance{
		ID:          e.nextInstanceID(),
		Name:        def.Name,
		Version:     def.Version,
		Fingerprint: def.Fingerprint,
		Status:      api.StatusPending,
		CurrentStep: 0,
		Input:       input,
		StepResults: make(map[int]any),
	}
	e.observer.OnWorkflowStart(ctx, inst)
	if err := e.instances.SaveInstance(inst); err != nil {
		inst.Status = api.StatusFailed
		inst.Err = err
		e.observer.OnWorkflowFailed(ctx, inst, err)
		return inst, err
	}
	e.emitEvent(ctx, inst, api.EventWorkflowEnqueued, -1, "start")
	// Enqueue a start task tied to this instance ID.
	err = e.queue.Enqueue(ctx, taskqueue.Task{
		Type:         taskqueue.TaskTypeStartWorkflow,
		WorkflowName: inst.Name,
		InstanceID:   inst.ID,
		Payload:      api.StartWorkflowPayload{Input: input},
		EnqueuedAt:   time.Now(),
	})
	return inst, err
}

// StartVersion schedules an explicit workflow version.
func (e *engineImpl) StartVersion(ctx context.Context, name, version string, input any) (*api.WorkflowInstance, error) {
	if e.queue == nil {
		return e.RunVersion(ctx, name, version, input)
	}
	def, err := e.workflows.GetWorkflow(name, version)
	if err != nil {
		return nil, err
	}
	inst := &api.WorkflowInstance{
		ID:          e.nextInstanceID(),
		Name:        def.Name,
		Version:     def.Version,
		Fingerprint: def.Fingerprint,
		Status:      api.StatusPending,
		CurrentStep: 0,
		Input:       input,
		StepResults: make(map[int]any),
	}
	e.observer.OnWorkflowStart(ctx, inst)
	if err := e.instances.SaveInstance(inst); err != nil {
		inst.Status = api.StatusFailed
		inst.Err = err
		e.observer.OnWorkflowFailed(ctx, inst, err)
		return inst, err
	}
	e.emitEvent(ctx, inst, api.EventWorkflowEnqueued, -1, "start")
	err = e.queue.Enqueue(ctx, taskqueue.Task{
		Type:         taskqueue.TaskTypeStartWorkflow,
		WorkflowName: inst.Name,
		InstanceID:   inst.ID,
		Payload:      api.StartWorkflowPayload{Input: input},
		EnqueuedAt:   time.Now(),
	})
	return inst, err
}

// RunInstance runs an existing (previously created) instance immediately.
// This is used by workers to avoid creating a new instance when processing queued start tasks.
func (e *engineImpl) RunInstance(ctx context.Context, instanceID string) (*api.WorkflowInstance, error) {
	inst, err := e.instances.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	def, err := e.workflows.GetWorkflow(inst.Name, inst.Version)
	if err != nil {
		return inst, err
	}
	if err := e.validateWorkflowFingerprint(def); err != nil {
		inst.Status = api.StatusFailed
		inst.Err = err
		_ = e.instances.UpdateInstance(inst)
		e.emitEvent(ctx, inst, api.EventWorkflowFailed, -1, err.Error())
		return inst, err
	}
	if err := e.acquireInstanceLease(ctx, inst.ID); err != nil {
		return inst, err
	}
	inst.Status = api.StatusRunning
	inst.Err = nil
	inst.Output = nil
	inst.CurrentStep = 0
	if err := e.instances.UpdateInstance(inst); err != nil {
		return inst, err
	}
	e.emitEvent(ctx, inst, api.EventWorkflowStarted, -1, "")
	return e.executeSteps(ctx, def, inst, 0, inst.Input)
}

func (e *engineImpl) Resume(ctx context.Context, id string) (*api.WorkflowInstance, error) {
	if e.queue == nil {
		return e.resumeInline(ctx, id)
	}

	inst, err := e.instances.GetInstance(id)
	if err != nil {
		return nil, err
	}
	if inst.Status != api.StatusFailed {
		return nil, fmt.Errorf("cannot resume instance %s in status %s", id, inst.Status)
	}
	e.emitEvent(ctx, inst, api.EventWorkflowEnqueued, -1, "resume")
	if err := e.queue.Enqueue(ctx, taskqueue.Task{
		Type:       taskqueue.TaskTypeResume,
		InstanceID: id,
		EnqueuedAt: time.Now(),
	}); err != nil {
		return inst, err
	}
	return inst, nil
}

// SignalNow delivers a signal and continues execution immediately (no enqueue).
func (e *engineImpl) SignalNow(ctx context.Context, id string, name string, payload any) (*api.WorkflowInstance, error) {
	// This is the original synchronous behavior.
	return e.signalInline(ctx, id, name, payload)
}

// ResumeNow resumes a failed workflow immediately (no enqueue).
func (e *engineImpl) ResumeNow(ctx context.Context, id string) (*api.WorkflowInstance, error) {
	return e.resumeInline(ctx, id)
}

func (e *engineImpl) GetInstance(ctx context.Context, id string) (*api.WorkflowInstance, error) {
	inst, err := e.instances.GetInstance(id)
	if err != nil {
		if errors.Is(err, persistence.ErrInstanceNotFound) {
			return nil, fmt.Errorf("instance not found: %s", id)
		}
		return nil, err
	}

	// Backward compatibility: instances created before versioning may have empty Version.
	version := inst.Version
	if version == "" {
		version = "v1"
	}

	def, err := e.workflows.GetWorkflow(inst.Name, version)
	if err != nil {
		// GetInstance is an inspection API: allow reading instances even if the
		// workflow definition is not registered/available (e.g. tests that seed
		// instances directly, or older persisted instances).
		if errors.Is(err, persistence.ErrWorkflowNotFound) {
			e.releaseInstanceLease(context.Background(), inst.ID)
			return inst, nil
		}
		return nil, err
	}

	if inst.Fingerprint != "" && inst.Fingerprint != def.Fingerprint {
		return nil, api.ErrWorkflowDefinitionMismatch
	}

	e.releaseInstanceLease(context.Background(), inst.ID)
	return inst, nil
}

func (e *engineImpl) ListInstances(ctx context.Context, opts api.InstanceListOptions) ([]*api.WorkflowInstance, error) {
	filter := persistence.InstanceFilter{
		WorkflowName: opts.WorkflowName,
		Status:       opts.Status,
	}
	return e.instances.ListInstances(filter)
}

func (e *engineImpl) ListEvents(ctx context.Context, instanceID string) ([]api.WorkflowEvent, error) {
	if e.events == nil {
		return nil, nil
	}
	return e.events.ListEvents(ctx, instanceID)
}

func (e *engineImpl) Signal(ctx context.Context, id string, name string, payload any) (*api.WorkflowInstance, error) {
	if e.queue == nil {
		return e.signalInline(ctx, id, name, payload)
	}

	inst, err := e.instances.GetInstance(id)
	if err != nil {
		return nil, err
	}
	if inst.Status != api.StatusWaiting {
		return nil, fmt.Errorf("cannot signal instance %s in status %s", id, inst.Status)
	}

	// Record the signal payload and enqueue work for a worker to resume execution.
	inst.Err = nil
	inst.Input = api.SignalPayload{Name: name, Data: payload}
	if err := e.instances.UpdateInstance(inst); err != nil {
		return inst, err
	}
	e.emitEvent(ctx, inst, api.EventSignalReceived, inst.CurrentStep, name)
	e.emitEvent(ctx, inst, api.EventWorkflowEnqueued, -1, "signal")
	if err := e.queue.Enqueue(ctx, taskqueue.Task{
		Type:       taskqueue.TaskTypeSignal,
		InstanceID: id,
		SignalName: name,
		Payload:    payload,
		EnqueuedAt: time.Now(),
	}); err != nil {
		return inst, err
	}
	return inst, nil
}

func (e *engineImpl) RecoverStuckInstances(ctx context.Context) (int, error) {
	// We treat any instance that is still StatusRunning as "stuck".
	// This should be safe if called on startup before any workers are
	// processing tasks, because nothing should be actively running then.
	instances, err := e.instances.ListInstances(persistence.InstanceFilter{
		Status: api.StatusRunning})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, inst := range instances {
		// Sanity check; defensive only.
		if inst.Status != api.StatusRunning {
			continue
		}

		inst.Status = api.StatusFailed
		inst.Err = fmt.Errorf("workflow engine recovered stuck running instance after crash")

		if err := e.instances.UpdateInstance(inst); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

func (e *engineImpl) emitEvent(ctx context.Context, inst *api.WorkflowInstance, typ api.EventType, step int, detail string) {
	if e.events == nil || inst == nil {
		return
	}
	_ = e.events.AppendEvent(ctx, api.WorkflowEvent{
		InstanceID:      inst.ID,
		At:              time.Now(),
		Type:            typ,
		WorkflowName:    inst.Name,
		WorkflowVersion: inst.Version,
		Step:            step,
		Detail:          detail,
	})
}

func (e *engineImpl) resumeInline(ctx context.Context, id string) (*api.WorkflowInstance, error) {
	inst, err := e.instances.GetInstance(id)
	if err != nil {
		if errors.Is(err, persistence.ErrInstanceNotFound) {
			return nil, fmt.Errorf("instance not found: %s", id)
		}
		return nil, err
	}

	if inst.Status != api.StatusFailed {
		return nil, fmt.Errorf("cannot resume instance %s in status %s", id, inst.Status)
	}

	if err := e.acquireInstanceLease(ctx, id); err != nil {
		return nil, err
	}
	defer func() {
		// Release on exit; executeSteps will keep renewing while running.
		e.releaseInstanceLease(context.Background(), id)
	}()

	version := inst.Version
	if version == "" {
		version = "v1"
	}

	def, err := e.workflows.GetWorkflow(inst.Name, version)
	if err != nil {
		return nil, err
	}

	if inst.Fingerprint != "" && inst.Fingerprint != def.Fingerprint {
		return nil, api.ErrWorkflowDefinitionMismatch
	}

	// Reset runtime fields and replay from the beginning.
	inst.Status = api.StatusRunning
	inst.Err = nil
	inst.Output = nil
	inst.CurrentStep = 0

	if err := e.instances.UpdateInstance(inst); err != nil {
		return inst, err
	}
	e.emitEvent(ctx, inst, api.EventWorkflowResumed, -1, "")

	return e.executeSteps(ctx, def, inst, 0, inst.Input)
}

func (e *engineImpl) signalInline(ctx context.Context, id string, name string, payload any) (*api.WorkflowInstance, error) {
	inst, err := e.instances.GetInstance(id)
	if err != nil {
		if errors.Is(err, persistence.ErrInstanceNotFound) {
			return nil, fmt.Errorf("instance not found: %s", id)
		}
		return nil, err
	}

	if inst.Status != api.StatusWaiting {
		return nil, fmt.Errorf("cannot signal instance %s in status %s", id, inst.Status)
	}

	if err := e.acquireInstanceLease(ctx, id); err != nil {
		return nil, err
	}
	defer func() {
		e.releaseInstanceLease(context.Background(), id)
	}()

	version := inst.Version
	if version == "" {
		version = "v1"
	}

	def, err := e.workflows.GetWorkflow(inst.Name, version)
	if err != nil {
		return nil, err
	}

	if inst.Fingerprint != "" && inst.Fingerprint != def.Fingerprint {
		return nil, api.ErrWorkflowDefinitionMismatch
	}

	// Prepare signal payload; resume AT the waiting step, passing the SignalPayload
	// as input so the step itself can validate the signal name and decide whether to
	// continue or request to wait again. This makes mismatched signals safely ignored.
	inst.Status = api.StatusRunning
	inst.Err = nil

	sp := api.SignalPayload{Name: name, Data: payload}
	inst.Input = sp

	// Clear any previous WaitMeta; the step will decide how to proceed.
	inst.Output = nil

	if err := e.instances.UpdateInstance(inst); err != nil {
		return inst, err
	}
	e.emitEvent(ctx, inst, api.EventWorkflowResumed, inst.CurrentStep, "signal")

	// Resume execution starting from the waiting step, providing the SignalPayload
	// as the current input. The step function (WaitForSignalStep/WaitForAnySignalStep)
	// will either accept it and advance, or return a wait error to keep waiting.
	return e.executeSteps(ctx, def, inst, inst.CurrentStep, sp)
}

func (e *engineImpl) nextInstanceID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextID++
	return fmt.Sprintf("wf-%d", e.nextID)
}

func (e *engineImpl) acquireInstanceLease(ctx context.Context, instanceID string) error {
	acquired, err := e.instances.TryAcquireLease(ctx, instanceID, e.engineID, e.leaseTTL)
	if err != nil {
		return err
	}
	if !acquired {
		return api.ErrWorkflowInstanceLocked
	}
	return nil
}

func (e *engineImpl) renewInstanceLease(ctx context.Context, instanceID string) {
	_ = e.instances.RenewLease(ctx, instanceID, e.engineID, e.leaseTTL)
}

func (e *engineImpl) releaseInstanceLease(ctx context.Context, instanceID string) {
	_ = e.instances.ReleaseLease(ctx, instanceID, e.engineID)
}

// validateWorkflowFingerprint ensures the registered workflow definition's fingerprint
// matches the strict fingerprint computed from its deterministic metadata.
// This protects against accidental non-deterministic changes between registration
// and execution. Returns ErrWorkflowDefinitionMismatch on mismatch.
func (e *engineImpl) validateWorkflowFingerprint(def api.WorkflowDefinition) error {
	if def.Fingerprint == "" {
		// Should not happen because RegisterWorkflow sets it, but guard anyway.
		def.Fingerprint = api.ComputeWorkflowFingerprintStrict(def)
		return nil
	}
	strict := api.ComputeWorkflowFingerprintStrict(def)
	if strict != def.Fingerprint {
		return api.ErrWorkflowDefinitionMismatch
	}
	return nil
}

func (e *engineImpl) executeSteps(
	ctx context.Context,
	def api.WorkflowDefinition,
	inst *api.WorkflowInstance,
	startIndex int,
	input any,
) (*api.WorkflowInstance, error) {
	current := input

	// Iterate through all steps
	for i := startIndex; i < len(def.Steps); i++ {
		// Extend lease while we work.
		e.renewInstanceLease(ctx, inst.ID)
		step := def.Steps[i]

		// Idempotency: if we already have a cached result for this step,
		// skip invoking the step function and reuse the cached value.
		if inst.StepResults != nil {
			if cached, ok := inst.StepResults[i]; ok {
				current = cached
				inst.CurrentStep = i + 1
				_ = e.instances.UpdateInstance(inst)
				continue
			}
		}

		inst.CurrentStep = i
		_ = e.instances.UpdateInstance(inst)
		e.emitEvent(ctx, inst, api.EventStepStarted, i, step.Name)

		// Determine max attempts for this step.
		maxAttempts := 1
		var (
			backoff    time.Duration // current backoff value
			maxBackoff time.Duration
			multiplier float64
		)

		if step.Retry != nil {
			if step.Retry.MaxAttempts > 0 {
				maxAttempts = step.Retry.MaxAttempts
			}
			// Determine initial backoff:
			//   1) Prefer InitialBackoff if set.
			//   2) Fall back to deprecated Backoff.
			backoff = step.Retry.InitialBackoff
			if backoff <= 0 {
				backoff = step.Retry.Backoff
			}

			maxBackoff = step.Retry.MaxBackoff

			// Backoff multiplier:
			//   - If explicitly set to > 0, use it.
			//   - Otherwise default to 2.0 (standard exponential backoff).
			multiplier = step.Retry.BackoffMultiplier
			if multiplier <= 0 {
				multiplier = 2.0
			}
		}

		var lastErr error

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			select {
			case <-ctx.Done():
				inst.Status = api.StatusFailed
				inst.Err = ctx.Err()
				_ = e.instances.UpdateInstance(inst)
				e.emitEvent(ctx, inst, api.EventStepFailed, i, step.Name+": "+ctx.Err().Error())
				e.emitEvent(ctx, inst, api.EventWorkflowFailed, -1, ctx.Err().Error())
				e.observer.OnWorkflowFailed(ctx, inst, ctx.Err())
				return inst, ctx.Err()
			default:
			}

			// Attach engine to the context for this step invocation so that
			// StartChildrenStep / WaitForChildrenStep (and similar helpers)
			// can use api.EngineFromContext(ctx).
			stepCtx := api.WithEngine(ctx, e)

			startTime := time.Now()
			e.observer.OnStepStart(stepCtx, inst, step.Name, i)

			// Execute step
			next, err := step.Fn(stepCtx, current)

			duration := time.Since(startTime)
			e.observer.OnStepCompleted(stepCtx, inst, step.Name, i, err, duration)

			if err == nil {
				// Success: advance to next step with new value.
				current = next
				lastErr = nil

				// Cache successful result for idempotency.
				if inst.StepResults == nil {
					inst.StepResults = make(map[int]any)
				}
				inst.StepResults[i] = next
				_ = e.instances.UpdateInstance(inst)
				e.emitEvent(stepCtx, inst, api.EventStepCompleted, i, step.Name)

				break
			}

			// NEW: wait-for-signal special case (sigName = _)
			if _, returnPayload, ok := api.WaitForSignalMeta(err); ok {
				inst.Status = api.StatusWaiting
				inst.Err = nil
				inst.Output = api.WaitMeta{Kind: "signal", ReturnPayload: returnPayload}
				_ = e.instances.UpdateInstance(inst)
				e.emitEvent(stepCtx, inst, api.EventWorkflowWaiting, i, step.Name)
				e.releaseInstanceLease(context.Background(), inst.ID)
				return inst, err
			}

			// NEW: wait-for-children special case (durable join)
			var waitChildren *api.WaitForChildrenError
			if errors.As(err, &waitChildren) {
				inst.Status = api.StatusWaiting
				inst.Err = nil
				_ = e.instances.UpdateInstance(inst)
				e.emitEvent(stepCtx, inst, api.EventWorkflowWaiting, i, step.Name)
				e.releaseInstanceLease(context.Background(), inst.ID)
				return inst, err
			}

			if api.IsWaitForAnyChildError(err) {
				inst.Status = api.StatusWaiting
				inst.Err = nil
				_ = e.instances.UpdateInstance(inst)
				e.releaseInstanceLease(context.Background(), inst.ID)
				return inst, err
			}

			lastErr = err

			// If this was the last allowed attempt, mark failed.
			if attempt == maxAttempts {
				inst.Status = api.StatusFailed
				inst.Err = lastErr
				_ = e.instances.UpdateInstance(inst)
				e.emitEvent(stepCtx, inst, api.EventStepFailed, i, step.Name+": "+lastErr.Error())
				e.emitEvent(stepCtx, inst, api.EventWorkflowFailed, -1, lastErr.Error())
				e.observer.OnWorkflowFailed(ctx, inst, lastErr)
				e.releaseInstanceLease(context.Background(), inst.ID)
				return inst, lastErr
			}

			// Wait before next attempt, if backoff is configured.
			if backoff > 0 {
				// Apply per-attempt delay with optional cap.
				delay := backoff
				if maxBackoff > 0 && delay > maxBackoff {
					delay = maxBackoff
				}

				select {
				case <-ctx.Done():
					inst.Status = api.StatusFailed
					inst.Err = ctx.Err()
					_ = e.instances.UpdateInstance(inst)
					e.emitEvent(ctx, inst, api.EventStepFailed, i, step.Name+": "+ctx.Err().Error())
					e.emitEvent(ctx, inst, api.EventWorkflowFailed, -1, ctx.Err().Error())
					return inst, ctx.Err()
				case <-time.After(delay):
					// continue to next attempt
				}

				// Increase backoff for the next retry.
				if multiplier > 0 {
					nextBackoff := time.Duration(float64(backoff) * multiplier)
					if maxBackoff > 0 && nextBackoff > maxBackoff {
						backoff = maxBackoff
					} else {
						backoff = nextBackoff
					}
				}
			}
		}
	}

	inst.Status = api.StatusCompleted
	inst.Output = current
	inst.CurrentStep = len(def.Steps)
	_ = e.instances.UpdateInstance(inst)

	e.emitEvent(ctx, inst, api.EventWorkflowCompleted, -1, "")
	e.observer.OnWorkflowCompleted(ctx, inst)
	e.releaseInstanceLease(context.Background(), inst.ID)

	return inst, nil
}
