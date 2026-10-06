package k6

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// TestRunThreshold preserves one returned string, object, or null threshold.
// It contains configured options, not an evaluated threshold result.
type TestRunThreshold struct {
	raw json.RawMessage
}

// UnmarshalJSON validates known field types without normalizing the wire value.
func (t *TestRunThreshold) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 {
		return errors.New("k6 threshold: empty JSON value")
	}
	switch raw[0] {
	case '"':
		var expression string
		if err := json.Unmarshal(raw, &expression); err != nil {
			return err
		}
	case '{':
		var object struct {
			Threshold      *string         `json:"threshold"`
			AbortOnFail    *bool           `json:"abortOnFail"`
			DelayAbortEval json.RawMessage `json:"delayAbortEval"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		if err := validateThresholdDelay(object.DelayAbortEval); err != nil {
			return err
		}
	default:
		if !bytes.Equal(raw, []byte("null")) {
			return errors.New("k6 threshold must be a string, object, or null")
		}
	}
	t.raw = append(t.raw[:0], raw...)
	return nil
}

// MarshalJSON preserves mixed entries, field presence, and abort options.
func (t TestRunThreshold) MarshalJSON() ([]byte, error) {
	if len(t.raw) == 0 {
		return []byte("null"), nil
	}
	return t.raw, nil
}

func validateThresholdDelay(raw json.RawMessage) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '"' {
		var value string
		return json.Unmarshal(raw, &value)
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("k6 threshold delayAbortEval must be a string, number, or null: %w", err)
	}
	return nil
}
