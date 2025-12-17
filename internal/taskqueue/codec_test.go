package taskqueue

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
	"time"
)

// custom struct used as a payload in tests
type testPayload struct {
	A string
	B int
}

func init() {
	// Register concrete types that may flow through the interface Payload.
	gob.Register(map[string]any{})
	gob.Register(testPayload{})
}

func TestEncodeDecodeTask_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	later := now.Add(5 * time.Minute)

	cases := []struct {
		name    string
		payload any
		base    Task
	}{
		{
			name:    "nil payload",
			payload: nil,
		},
		{
			name:    "string payload",
			payload: "hello",
		},
		{
			name:    "bytes payload",
			payload: []byte("abc"),
		},
		{
			name:    "map payload",
			payload: map[string]any{"x": 1, "y": "z"},
		},
		{
			name:    "struct payload",
			payload: testPayload{A: "foo", B: 42},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			orig := Task{
				ID:           "id-123",
				Type:         TaskTypeSignal,
				WorkflowName: "wf",
				InstanceID:   "inst-1",
				SignalName:   "sig",
				Payload:      tc.payload,
				EnqueuedAt:   now,
				NotBefore:    later,
				Attempts:     3,
			}

			data, err := EncodeTask(orig)
			if err != nil {
				t.Fatalf("EncodeTask error: %v", err)
			}
			if len(data) == 0 {
				t.Fatalf("EncodeTask returned empty bytes")
			}

			got, err := DecodeTask(data)
			if err != nil {
				t.Fatalf("DecodeTask error: %v", err)
			}
			if got == nil {
				t.Fatalf("DecodeTask returned nil task")
			}

			// Field-by-field assertions (avoid direct struct equality due to time monotonic data)
			if got.ID != orig.ID {
				t.Fatalf("ID mismatch: got %q want %q", got.ID, orig.ID)
			}
			if got.Type != orig.Type {
				t.Fatalf("Type mismatch: got %q want %q", got.Type, orig.Type)
			}
			if got.WorkflowName != orig.WorkflowName {
				t.Fatalf("WorkflowName mismatch: got %q want %q", got.WorkflowName, orig.WorkflowName)
			}
			if got.InstanceID != orig.InstanceID {
				t.Fatalf("InstanceID mismatch: got %q want %q", got.InstanceID, orig.InstanceID)
			}
			if got.SignalName != orig.SignalName {
				t.Fatalf("SignalName mismatch: got %q want %q", got.SignalName, orig.SignalName)
			}
			// Compare payloads by encoding them again and byte-comparing, which works across interface boundaries
			if !payloadEqual(got.Payload, orig.Payload) {
				t.Fatalf("Payload mismatch: got %#v want %#v", got.Payload, orig.Payload)
			}
			if !got.EnqueuedAt.Equal(orig.EnqueuedAt) {
				t.Fatalf("EnqueuedAt mismatch: got %v want %v", got.EnqueuedAt, orig.EnqueuedAt)
			}
			if !got.NotBefore.Equal(orig.NotBefore) {
				t.Fatalf("NotBefore mismatch: got %v want %v", got.NotBefore, orig.NotBefore)
			}
			if got.Attempts != orig.Attempts {
				t.Fatalf("Attempts mismatch: got %d want %d", got.Attempts, orig.Attempts)
			}
		})
	}
}

func TestDecodeTask_InvalidData_ReturnsError(t *testing.T) {
	// Random bytes that are unlikely to be valid gob for our Task
	bad := []byte{0x00, 0x01, 0x02, 0x03, 0xFF}
	if task, err := DecodeTask(bad); err == nil {
		t.Fatalf("expected error, got task: %#v", task)
	}
}

// payloadEqual compares two arbitrary payloads by gob-encoding them as interface values
// to sidestep type assertion issues on interface fields.
func payloadEqual(a, b any) bool {
	// quick path for []byte which is commonly compared as bytes
	ab, aok := a.([]byte)
	bb, bok := b.([]byte)
	if aok && bok {
		return bytes.Equal(ab, bb)
	}

	// Prefer reflect.DeepEqual which properly handles maps (order-independent)
	if reflect.DeepEqual(a, b) {
		return true
	}

	da, ea := persistenceLikeEncodeAny(a)
	db, eb := persistenceLikeEncodeAny(b)
	if ea != nil || eb != nil {
		return false
	}
	return bytes.Equal(da, db)
}

// minimal encoding like internal/persistence.EncodeValue does (encode as interface{})
func persistenceLikeEncodeAny(v any) ([]byte, error) {
	// Reuse EncodeTask by wrapping v in a Task payload to ensure the gob
	// configuration matches production paths.
	t := Task{Payload: v}
	return EncodeTask(t)
}
