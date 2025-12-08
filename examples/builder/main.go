// examples/builder/main.go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/petrijr/fluxo"
	"github.com/petrijr/fluxo/pkg/api"
)

type State struct {
	OnboardInput
	UserID string
}

type OnboardInput struct {
	Email string
	Name  string
}

type OnboardOutput struct {
	UserID        string
	WelcomeSentAt time.Time
}

func main() {
	ctx := context.Background()

	// Use the simple in-memory engine for local dev / tests.
	eng := fluxo.NewInMemoryEngine()

	// Define the workflow using the builder-style API.
	flow := fluxo.New("onboard-user").
		Step("create-account", createAccount).
		Parallel("send-welcome-and-log", // runs in parallel
			sendWelcomeEmail,
			writeAuditLog,
		).
		Step("finalize", finalizeOnboarding)

	// Register the workflow with the engine.
	flow.MustRegister(eng)

	// Run the workflow synchronously.
	input := OnboardInput{
		Email: "user@example.com",
		Name:  "Example User",
	}

	instance, err := fluxo.Run(ctx, eng, flow.Name(), input)
	if err != nil {
		panic(err)
	}

	if instance.Status != fluxo.StatusCompleted {
		panic(fmt.Sprintf("unexpected status: %v", instance.Status))
	}

	out, ok := instance.Output.(OnboardOutput)
	if !ok {
		panic(fmt.Sprintf("unexpected output type %T", instance.Output))
	}

	fmt.Printf("Onboarding complete: userID=%s welcomeSentAt=%s\n",
		out.UserID, out.WelcomeSentAt.Format(time.RFC3339))
}

// --- Step implementations ---

func createAccount(ctx context.Context, input any) (any, error) {
	in, ok := input.(OnboardInput)
	if !ok {
		return nil, fmt.Errorf("create-account: expected OnboardInput, got %T", input)
	}

	// In a real service, you'd hit a DB or external system here.
	userID := fmt.Sprintf("user-%s", in.Email)

	fmt.Printf("[create-account] created user %s for %s\n", userID, in.Email)

	// Pass along an enriched value to the next steps.
	return State{
		OnboardInput: in,
		UserID:       userID,
	}, nil
}

func sendWelcomeEmail(ctx context.Context, input any) (any, error) {
	state, ok := input.(State)
	if !ok {
		return nil, fmt.Errorf("send-welcome-email: unexpected input %T", input)
	}

	fmt.Printf("[send-welcome-email] sending welcome email to %s (%s)\n",
		state.Email, state.UserID)

	// Simulate some work.
	time.Sleep(50 * time.Millisecond)

	return input, nil // keep passing the combined state through
}

func writeAuditLog(ctx context.Context, input any) (any, error) {
	state, ok := input.(State)
	if !ok {
		return nil, fmt.Errorf("write-audit-log: unexpected input %T", input)
	}

	fmt.Printf("[write-audit-log] audit entry for user %s (%s)\n",
		state.UserID, state.Email)

	return input, nil
}

func finalizeOnboarding(ctx context.Context, input any) (any, error) {
	states, ok := input.([]api.ParallelResult)
	if !ok {
		return nil, fmt.Errorf("finalize-onboarding: unexpected input %T", input)
	}

	state, ok := states[1].Value.(State)
	if !ok {
		return nil, fmt.Errorf("finalize-onboarding: unexpected state %T", states[1].Value)
	}

	out := OnboardOutput{
		UserID:        state.UserID,
		WelcomeSentAt: time.Now().UTC(),
	}

	fmt.Printf("[finalize-onboarding] done for user %s\n", state.UserID)

	return out, nil
}
