package fluxo

import (
	"context"
	"testing"
)

// simple helper used by multiple tests
func addConst(c int) StepFunc {
	return TypedStep(func(ctx context.Context, in int) (int, error) {
		return in + c, nil
	})
}

func TestFlowBuilder_BuildAndRegister(t *testing.T) {
	eng := NewInMemoryEngine()

	rb := Retry(3).Immediate() // exercise RetryBuilder + StepWithRetry

	flow := New("builder-sample").
		Step("s1", addConst(1)).
		StepWithRetryBuilder("s2", addConst(2), rb).
		Parallel("par", addConst(1), addConst(2)).
		While("while", func(in any) bool { return in.(int) < 5 }, addConst(1)).
		Loop("loop", 2, addConst(1)).
		If("if", func(in any) bool { return in.(int)%2 == 0 }, addConst(1), addConst(2)).
		Switch("switch", func(in any) string {
			if in.(int) > 0 {
				return "pos"
			}
			return "neg"
		}, map[string]StepFunc{
			"pos": addConst(1),
			"neg": addConst(-1),
		}, addConst(0)).
		WaitForAnySignal("wfa", "noop") // just cover wrapper construction

	if err := flow.Register(eng); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if flow.Name() != "builder-sample" {
		t.Fatalf("unexpected name: %s", flow.Name())
	}

	// sanity: Definition() should not be empty
	def := flow.Definition()
	if def.Name == "" || len(def.Steps) == 0 {
		t.Fatalf("unexpected empty definition")
	}
}

func TestFlowBuilder_MustRegister_PanicOnDuplicate(t *testing.T) {
	eng := NewInMemoryEngine()
	flow := New("dup-flow").Step("s", addConst(1))
	flow.MustRegister(eng)

	// Second registration of the same name should cause panic in MustRegister.
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate registration")
		}
	}()
	flow.MustRegister(eng)
}
