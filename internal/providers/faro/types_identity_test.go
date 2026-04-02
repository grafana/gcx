package faro_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Compile-time assertion that *FaroApp satisfies ResourceIdentity.
var _ adapter.ResourceIdentity = &faro.FaroApp{}

func TestFaroApp_ResourceIdentity(t *testing.T) {
	tests := []struct {
		name     string
		app      faro.FaroApp
		wantName string
	}{
		{
			name:     "composite slug with name and ID",
			app:      faro.FaroApp{ID: "42", Name: "my-web-app"},
			wantName: "my-web-app-42",
		},
		{
			name:     "bare ID when name is empty",
			app:      faro.FaroApp{ID: "42"},
			wantName: "resource-42",
		},
		{
			name:     "name with special characters",
			app:      faro.FaroApp{ID: "100", Name: "My Web App!"},
			wantName: "my-web-app-100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.GetResourceName(); got != tt.wantName {
				t.Errorf("GetResourceName() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestFaroApp_SetResourceName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID string
	}{
		{
			name:   "extracts ID from composite slug",
			input:  "my-web-app-42",
			wantID: "42",
		},
		{
			name:   "extracts bare numeric ID",
			input:  "42",
			wantID: "42",
		},
		{
			name:   "non-numeric name leaves ID empty",
			input:  "my-web-app",
			wantID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &faro.FaroApp{}
			app.SetResourceName(tt.input)
			if app.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", app.ID, tt.wantID)
			}
		})
	}
}

func TestFaroApp_RoundTripIdentity(t *testing.T) {
	original := faro.FaroApp{ID: "42", Name: "my-web-app"}
	name := original.GetResourceName()

	restored := &faro.FaroApp{}
	restored.SetResourceName(name)

	if restored.ID != original.ID {
		t.Errorf("round-trip ID = %q, want %q", restored.ID, original.ID)
	}
}
