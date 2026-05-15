package vulnobs_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/vulnobs"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Compile-time assertion: Source satisfies ResourceIdentity.
var _ adapter.ResourceIdentity = &vulnobs.Source{}

func TestSource_ResourceIdentity_Roundtrip(t *testing.T) {
	tests := []struct {
		name        string
		sourceName  string
		wantID      string
		setTo       string
		wantAfter   string
		wantSetName string
	}{
		{
			name:        "github owner/repo",
			sourceName:  "grafana/faro-web-sdk",
			wantID:      "grafana--faro-web-sdk",
			setTo:       "grafana--faro-web-sdk",
			wantAfter:   "grafana--faro-web-sdk",
			wantSetName: "grafana/faro-web-sdk",
		},
		{
			name:        "no slash",
			sourceName:  "single-name",
			wantID:      "single-name",
			setTo:       "single-name",
			wantAfter:   "single-name",
			wantSetName: "single-name",
		},
		{
			name:        "set from owner--repo",
			sourceName:  "",
			wantID:      "",
			setTo:       "grafana--mimir",
			wantAfter:   "grafana--mimir",
			wantSetName: "grafana/mimir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &vulnobs.Source{Name: tt.sourceName}
			if got := s.GetResourceName(); got != tt.wantID {
				t.Errorf("GetResourceName() = %q, want %q", got, tt.wantID)
			}
			s.SetResourceName(tt.setTo)
			if s.Name != tt.wantSetName {
				t.Errorf("Name after SetResourceName(%q) = %q, want %q",
					tt.setTo, s.Name, tt.wantSetName)
			}
			if got := s.GetResourceName(); got != tt.wantAfter {
				t.Errorf("GetResourceName() after SetResourceName(%q) = %q, want %q",
					tt.setTo, got, tt.wantAfter)
			}
		})
	}
}
