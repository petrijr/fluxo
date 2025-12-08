package fluxo

import (
	"fmt"

	"github.com/petrijr/fluxo/pkg/api"
)

// FlowBuilder provides a fluent API for defining workflows:
//
//	flow := fluxo.New("OnboardUser").
//	    Step("createAccount", createAccount).
//	    Step("sendWelcomeEmail", sendWelcomeEmail).
//	    Step("waitActivation", fluxo.WaitForSignalStep("activated"))
//
//	if err := flow.Register(engine); err != nil {
//	    log.Fatal(err)
//	}
//
//	inst, err := fluxo.Run(ctx, engine, flow.Name(), input)
type FlowBuilder struct {
	def api.WorkflowDefinition
}

// New creates a new workflow builder with the given name.
func New(name string) *FlowBuilder {
	return &FlowBuilder{
		def: api.WorkflowDefinition{
			Name:  name,
			Steps: make([]api.StepDefinition, 0),
		},
	}
}

// Name returns the workflow name.
func (b *FlowBuilder) Name() string {
	return b.def.Name
}

// Definition returns the underlying WorkflowDefinition.
// Typically used when interacting with lower-level APIs.
func (b *FlowBuilder) Definition() WorkflowDefinition {
	return b.def
}

// Step appends a basic step to the workflow.
func (b *FlowBuilder) Step(name string, fn StepFunc) *FlowBuilder {
	if name == "" {
		panic("fluxo: step name must not be empty")
	}
	if fn == nil {
		panic(fmt.Sprintf("fluxo: step %q has nil function", name))
	}

	b.def.Steps = append(b.def.Steps, api.StepDefinition{
		Name:  name,
		Fn:    fn,
		Retry: nil,
	})
	return b
}

// StepWithRetry appends a step that uses the given retry policy.
func (b *FlowBuilder) StepWithRetry(name string, fn StepFunc, retry RetryPolicy) *FlowBuilder {
	if name == "" {
		panic("fluxo: step name must not be empty")
	}
	if fn == nil {
		panic(fmt.Sprintf("fluxo: step %q has nil function", name))
	}

	// Make a copy so callers can mutate their RetryPolicy after the call
	// without affecting the stored definition.
	r := retry

	b.def.Steps = append(b.def.Steps, api.StepDefinition{
		Name:  name,
		Fn:    fn,
		Retry: &r,
	})
	return b
}

// Parallel is a convenience for adding a step that runs sub-steps in parallel.
func (b *FlowBuilder) Parallel(name string, steps ...StepFunc) *FlowBuilder {
	return b.Step(name, ParallelStep(steps...))
}

// If adds a conditional branching step.
func (b *FlowBuilder) If(name string, cond ConditionFunc, thenStep, elseStep StepFunc) *FlowBuilder {
	return b.Step(name, IfStep(cond, thenStep, elseStep))
}

// Switch adds a multi-branch step based on a selector and branch map.
func (b *FlowBuilder) Switch(
	name string,
	selector SelectorFunc,
	branches map[string]StepFunc,
	defaultStep StepFunc,
) *FlowBuilder {
	return b.Step(name, SwitchStep(selector, branches, defaultStep))
}

// WaitForSignal adds a step that waits for a named signal.
func (b *FlowBuilder) WaitForSignal(stepName, signalName string) *FlowBuilder {
	return b.Step(stepName, WaitForSignalStep(signalName))
}

// WaitForAnySignal adds a step that waits for any of the given signal names.
func (b *FlowBuilder) WaitForAnySignal(stepName string, names ...string) *FlowBuilder {
	return b.Step(stepName, WaitForAnySignalStep(names...))
}

// Register registers the built workflow with the given engine.
func (b *FlowBuilder) Register(eng Engine) error {
	return eng.RegisterWorkflow(b.def)
}

// MustRegister is like Register but panics on error.
// Useful for initialization in main().
func (b *FlowBuilder) MustRegister(eng Engine) {
	if err := b.Register(eng); err != nil {
		panic(err)
	}
}
