package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/petrijr/fluxo"
)

var apiAttempts int
var startTime time.Time

func main() {
	ctx := context.Background()

	eng := fluxo.NewInMemoryEngine()

	// Define a workflow that calls a flaky API with retries.
	flow := fluxo.New("flaky-api-call").
		StepWithRetryBuilder(
			"call-external-api",
			callFlakyAPI,
			fluxo.Retry(6).WithExponentialBackoff(
				100*time.Millisecond, // initial delay
				2.0,                  // multiplier
				2*time.Second,        // max backoff cap
			),
		)

	flow.MustRegister(eng)

	log.Println("Running workflow flaky-api-call with exponential backoff…")

	startTime = time.Now()
	inst, err := fluxo.Run(ctx, eng, flow.Name(), nil)
	if err != nil {
		log.Fatalf("workflow failed: %v", err)
	}

	log.Printf("workflow status: %v", inst.Status)

	resp, ok := inst.Output.(string)
	if !ok {
		log.Fatalf("unexpected output type %T (%v)", inst.Output, inst.Output)
	}

	log.Printf("final response: %s", resp)
	log.Printf("total attempts: %d", apiAttempts)
}

// callFlakyAPI simulates an external API that fails a few times before
// eventually succeeding.
func callFlakyAPI(ctx context.Context, input any) (any, error) {
	apiAttempts++
	attempt := apiAttempts

	log.Printf("[callFlakyAPI] attempt %d (time from start %dms)", attempt, time.Now().Sub(startTime).Milliseconds())

	// Fail the first 4 attempts to demonstrate retry backoff.
	if attempt <= 4 {
		err := errors.New("transient network error")
		log.Printf("[callFlakyAPI] attempt %d failed: %v", attempt, err)
		return nil, err
	}

	// Succeed on later attempts.
	log.Printf("[callFlakyAPI] attempt %d succeeded", attempt)
	return fmt.Sprintf("OK (after %d attempts)", attempt), nil
}
