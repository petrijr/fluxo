package persistence

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// EncodeValue serializes arbitrary Go values using encoding/gob.
// Callers must ensure that values are gob-encodable.
func EncodeValue(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	// Important: encode as interface{} so we can safely decode into interface{}.
	var iv = v
	if err := enc.Encode(&iv); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeValue tries both interface-first (legacy) and concrete decoding.
func DecodeValue[T any](data []byte) (T, error) {
	var zero T
	if len(data) == 0 {
		return zero, nil
	}

	// 1) Try legacy interface-encoded payloads first
	if v, ok, err := tryDecodeAsAny[T](data); err == nil && ok {
		return v, nil
	} else if err != nil && !mustRetryAsConcrete(err) {
		// A hard error not related to interface/concrete mismatch
		return zero, err
	}

	// 2) Try to decode directly into T (works when payload was encoded as concrete T)
	if v, err := tryDecodeAsT[T](data); err == nil {
		return v, nil
	} else if !isInterfaceType[T]() {
		// If T is not interface, return the error
		return zero, err
	}

	// 3) If T is interface (any) and concrete-encoded payload was used, try common types
	if v, ok, err := tryDecodeCommonConcreteAsAny[T](data); err == nil && ok {
		return v, nil
	} else if err != nil {
		return zero, err
	}

	return zero, errors.New("gob: unable to decode into target type")
}

func tryDecodeAsAny[T any](data []byte) (T, bool, error) {
	var zero T
	var iv any
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	if err := dec.Decode(&iv); err != nil {
		return zero, false, err
	}
	// If T is any or matches the dynamic type, return it
	if v, ok := iv.(T); ok {
		return v, true, nil
	}
	// Special case: allow returning interface payload for T=any via reflection
	var _ T
	if isInterfaceType[T]() {
		return any(iv).(T), true, nil
	}
	return zero, false, fmt.Errorf("gob: decoded interface payload of type %T not assignable to target", iv)
}

func tryDecodeAsT[T any](data []byte) (T, error) {
	var v T
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	if err := dec.Decode(&v); err != nil {
		return v, err
	}
	return v, nil
}

func tryDecodeCommonConcreteAsAny[T any](data []byte) (T, bool, error) {
	var zero T
	// Try a set of common types you expect in Input/Output
	try := func(dst any) (any, bool, error) {
		buf := bytes.NewBuffer(data)
		dec := gob.NewDecoder(buf)
		if err := dec.Decode(dst); err != nil {
			return nil, false, err
		}
		return reflect.ValueOf(dst).Elem().Interface(), true, nil
	}

	// Add or remove types based on what your workflows actually use
	candidates := []any{
		new(string), new([]byte), new(int), new(int64), new(float64), new(bool),
		new(map[string]any), new(map[int]any), new([]any), new([]string), new([]int),
	}
	for _, c := range candidates {
		if val, ok, _ := try(c); ok {
			// If T is any, we can safely box it
			if isInterfaceType[T]() {
				return any(val).(T), true, nil
			}
			if v, ok := val.(T); ok {
				return v, true, nil
			}
		}
	}
	return zero, false, errors.New("no matching common concrete type for interface target")
}

func mustRetryAsConcrete(err error) bool {
	// Heuristic: detect the specific gob message for interface-vs-concrete mismatch
	s := err.Error()
	return strings.Contains(s, "can only be decoded from remote interface") &&
		strings.Contains(s, "received concrete type")
}

func isInterfaceType[T any]() bool {
	var t T
	return reflect.TypeOf((*T)(nil)).Elem().Kind() == reflect.Interface || reflect.TypeOf(t) == nil
}
