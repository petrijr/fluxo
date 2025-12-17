package api

import (
	"context"
	"testing"
)

func TestComputeWorkflowFingerprintStrict_DifferentStepFunctionIdentity(t *testing.T) {
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

	fp1 := ComputeWorkflowFingerprintStrict(wf1)
	fp2 := ComputeWorkflowFingerprintStrict(wf2)

	if fp1 == fp2 {
		t.Fatalf("expected strict fingerprints to differ")
	}
}
