package annotations_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/annotations"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Compile-time assertion that *Annotation satisfies ResourceIdentity.
var _ adapter.ResourceIdentity = &annotations.Annotation{}

func TestAnnotation_GetResourceName(t *testing.T) {
	tests := []struct {
		name     string
		ann      annotations.Annotation
		wantName string
	}{
		{
			name:     "numeric ID",
			ann:      annotations.Annotation{ID: 42, Text: "deploy"},
			wantName: "42",
		},
		{
			name:     "zero ID returns empty",
			ann:      annotations.Annotation{Text: "new"},
			wantName: "",
		},
		{
			name:     "large ID",
			ann:      annotations.Annotation{ID: 9007199254740991},
			wantName: "9007199254740991",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ann.GetResourceName(); got != tt.wantName {
				t.Errorf("GetResourceName() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestAnnotation_SetResourceName(t *testing.T) {
	tests := []struct {
		name    string
		initial int64
		input   string
		wantID  int64
	}{
		{
			name:    "numeric string sets ID",
			initial: 0,
			input:   "42",
			wantID:  42,
		},
		{
			name:    "non-numeric leaves ID unchanged",
			initial: 7,
			input:   "not-a-number",
			wantID:  7,
		},
		{
			name:    "empty string leaves ID unchanged",
			initial: 3,
			input:   "",
			wantID:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &annotations.Annotation{ID: tt.initial}
			a.SetResourceName(tt.input)
			if a.ID != tt.wantID {
				t.Errorf("ID = %d, want %d", a.ID, tt.wantID)
			}
		})
	}
}

func TestAnnotation_RoundTripIdentity(t *testing.T) {
	original := annotations.Annotation{ID: 42, Text: "deploy"}
	name := original.GetResourceName()

	restored := &annotations.Annotation{}
	restored.SetResourceName(name)

	if restored.ID != original.ID {
		t.Errorf("round-trip ID = %d, want %d", restored.ID, original.ID)
	}
}
