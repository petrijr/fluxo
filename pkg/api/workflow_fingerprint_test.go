package api

import (
	"context"
	"testing"
)

func TestComputeWorkflowFingerprint_DifferentStepFunctionContent(t *testing.T) {
	wf1 := WorkflowDefinition{
		Name:    "order",
		Version: "v1",
		Steps: []StepDefinition{
			{Name: "s1", Fn: func(ctx context.Context, input any) (any, error) { return "v1", nil }},
		},
	}
	wf2 := WorkflowDefinition{
		Name:    "order",
		Version: "v1",
		Steps: []StepDefinition{
			{Name: "s1", Fn: func(ctx context.Context, input any) (any, error) { return "v1-changed", nil }},
		},
	}

	fp1 := ComputeWorkflowFingerprint(wf1)
	fp2 := ComputeWorkflowFingerprint(wf2)

	if fp1 != fp2 {
		t.Fatalf("fingerprints do not match")
	}

}
