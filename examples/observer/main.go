// examples/observer/main.go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/petrijr/fluxo"
)

type stepStats struct {
	count    int
	lastDur  time.Duration
	totalDur time.Duration
}

type countingObserver struct {
	mu    sync.Mutex
	steps map[string]*stepStats
}

func newCountingObserver() *countingObserver {
	return &countingObserver{steps: map[string]*stepStats{}}
}

func (o *countingObserver) OnWorkflowStart(ctx context.Context, inst *fluxo.WorkflowInstance)     {}
func (o *countingObserver) OnWorkflowCompleted(ctx context.Context, inst *fluxo.WorkflowInstance) {}
func (o *countingObserver) OnWorkflowFailed(ctx context.Context, inst *fluxo.WorkflowInstance, err error) {
}
func (o *countingObserver) OnStepStart(ctx context.Context, inst *fluxo.WorkflowInstance, stepName string, stepIndex int) {
}
func (o *countingObserver) OnStepCompleted(ctx context.Context, inst *fluxo.WorkflowInstance, stepName string, stepIndex int, err error, dur time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()

	s := o.steps[stepName]
	if s == nil {
		s = &stepStats{}
		o.steps[stepName] = s
	}
	s.count++
	s.lastDur = dur
	s.totalDur += dur
}

func (o *countingObserver) Snapshot() map[string]stepStats {
	o.mu.Lock()
	defer o.mu.Unlock()

	out := make(map[string]stepStats, len(o.steps))
	for k, v := range o.steps {
		out[k] = *v
	}
	return out
}

func main() {
	ctx := context.Background()

	obs := newCountingObserver()
	eng := fluxo.NewInMemoryEngineWithObserver(obs)

	flow := fluxo.New("observer-example").
		Step("a", func(ctx context.Context, input any) (any, error) {
			time.Sleep(25 * time.Millisecond)
			return "hello", nil
		}).Step("b", func(ctx context.Context, input any) (any, error) {
		time.Sleep(40 * time.Millisecond)
		return fmt.Sprintf("%s world", input.(string)), nil
	}).Step("c", func(ctx context.Context, input any) (any, error) {
		time.Sleep(10 * time.Millisecond)
		return input.(string) + "!", nil
	})

	flow.MustRegister(eng)

	inst, err := fluxo.Run(ctx, eng, flow.Name(), nil)
	if err != nil {
		panic(err)
	}

	fmt.Printf("output: %v\n", inst.Output)

	fmt.Println("step metrics (from custom observer):")
	for name, s := range obs.Snapshot() {
		fmt.Printf("- %s: count=%d last=%s total=%s\n", name, s.count, s.lastDur, s.totalDur)
	}
}
