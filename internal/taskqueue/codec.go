package taskqueue

import (
	"bytes"
	"encoding/gob"
)

// encodeTask gob-encodes a Task.
func encodeTask(t Task) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&t); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeTask gob-decodes a Task.
func decodeTask(data []byte) (*Task, error) {
	var t Task
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}
