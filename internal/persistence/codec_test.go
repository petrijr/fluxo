package persistence

import (
	"errors"
	"testing"
)

// mustRetryAsConcrete should detect the specific gob interface/concrete mismatch message.
func TestMustRetryAsConcrete_MatchingGobMessage(t *testing.T) {
	// Match both substrings used in the heuristic.
	msg := "gob: value can only be decoded from remote interface type; received concrete type main.MyType"
	err := errors.New(msg)

	if !mustRetryAsConcrete(err) {
		t.Fatalf("expected mustRetryAsConcrete to return true for gob interface/concrete mismatch message")
	}
}

// Errors that do not contain both substrings should return false.
func TestMustRetryAsConcrete_NonMatchingErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "unrelated error",
			err:  errors.New("some other failure"),
		},
		{
			name: "only interface substring",
			err:  errors.New("gob: value can only be decoded from remote interface type"),
		},
		{
			name: "only concrete substring",
			err:  errors.New("gob: received concrete type main.MyType"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if mustRetryAsConcrete(tc.err) {
				t.Fatalf("expected mustRetryAsConcrete to return false for case %q", tc.name)
			}
		})
	}
}
