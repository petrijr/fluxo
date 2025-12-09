package fluxo

import (
	"context"
	"time"

	"github.com/petrijr/fluxo/pkg/api"
)

// SleepStep returns a step that sleeps for the given duration
// and passes the input through.
func SleepStep(d time.Duration) StepFunc {
	return api.SleepStep(d)
}

// SleepUntilStep sleeps until a given timestamp or returns ctx.Err.
func SleepUntilStep(t time.Time) StepFunc {
	return api.SleepUntilStep(t)
}

// ParallelStep runs all provided step funcs in parallel and
// returns a []any of their outputs.
func ParallelStep(steps ...StepFunc) StepFunc {
	return api.ParallelStep(steps...)
}

// ParallelMapStep runs a mapping step over a slice input in parallel.
func ParallelMapStep(mapper StepFunc) StepFunc {
	return api.ParallelMapStep(mapper)
}

// IfStep creates a conditional step composed of then/else branches.
func IfStep(cond ConditionFunc, thenStep, elseStep StepFunc) StepFunc {
	return api.IfStep(cond, thenStep, elseStep)
}

// SwitchStep dispatches to a branch based on a selector.
func SwitchStep(selector SelectorFunc, branches map[string]StepFunc, defaultStep StepFunc) StepFunc {
	return api.SwitchStep(selector, branches, defaultStep)
}

// WaitForSignalStep waits for a single named signal, returning its payload.
func WaitForSignalStep(name string) StepFunc {
	return api.WaitForSignalStep(name)
}

// WaitForAnySignalStep waits for one of the allowed signal names.
func WaitForAnySignalStep(names ...string) StepFunc {
	return api.WaitForAnySignalStep(names...)
}

// StartChildrenStep starts child workflows and returns their IDs.
func StartChildrenStep(specsFn func(input any) ([]api.ChildWorkflowSpec, error)) StepFunc {
	return api.StartChildrenStep(specsFn)
}

// WaitForChildrenStep waits for all given child workflow IDs to complete.
func WaitForChildrenStep(getIDs func(input any) []string, pollInterval time.Duration) StepFunc {
	return api.WaitForChildrenStep(getIDs, pollInterval)
}

// WaitForAnyChildStep waits until any of the children completes.
func WaitForAnyChildStep(getIDs func(input any) []string, pollInterval time.Duration) StepFunc {
	return api.WaitForAnyChildStep(getIDs, pollInterval)
}

// While returns a step that repeatedly executes body while cond(input) is true.
// The entire loop is treated as a single engine step.
func While(cond ConditionFunc, body StepFunc) StepFunc {
	return api.WhileStep(cond, body)
}

// LoopStep returns a step that executes body a fixed number of times.
// The entire loop is treated as a single engine step.
func LoopStep(times int, body StepFunc) StepFunc {
	return api.LoopStep(times, body)
}

// TypedStep wraps a strongly-typed function into a StepFunc.
// Example:
//
//	fluxo.TypedStep(func(ctx context.Context, s MyState) (MyState, error) { ... })
func TypedStep[I, O any](fn func(context.Context, I) (O, error)) StepFunc {
	return api.TypedStep(fn)
}

// TypedWhile returns a step that repeatedly executes a strongly-typed body
// while cond(input) is true.
func TypedWhile[I any](cond func(I) bool, body func(context.Context, I) (I, error)) StepFunc {
	return api.TypedWhile(cond, body)
}

// TypedLoop returns a step that executes a strongly-typed body a fixed number
// of times.
func TypedLoop[I any](times int, body func(context.Context, I) (I, error)) StepFunc {
	return api.TypedLoop(times, body)
}
