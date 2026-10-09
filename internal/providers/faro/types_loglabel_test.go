package faro_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/faro"
)

// The Faro API names this field "label". A "key" tag makes the server store an
// empty label name, and Loki then rejects every write for the app while traces
// keep flowing — so the corruption is invisible from gcx alone.
func TestLogLabel_WireFieldName(t *testing.T) {
	encoded, err := json.Marshal(faro.LogLabel{Label: "is_mobile", Value: "true"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(encoded), `{"label":"is_mobile","value":"true"}`; got != want {
		t.Errorf("LogLabel JSON = %s, want %s", got, want)
	}

	// The API also returns an "id" the CLI does not model; it must not disturb decoding.
	var decoded faro.LogLabel
	if err := json.Unmarshal([]byte(`{"id":76,"label":"is_mobile","value":"true"}`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Label != "is_mobile" {
		t.Errorf("Label = %q, want %q", decoded.Label, "is_mobile")
	}
	if decoded.Value != "true" {
		t.Errorf("Value = %q, want %q", decoded.Value, "true")
	}
}
