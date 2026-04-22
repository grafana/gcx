package publicdashboards_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/publicdashboards"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Compile-time assertion that *PublicDashboard satisfies ResourceIdentity.
var _ adapter.ResourceIdentity = &publicdashboards.PublicDashboard{}

func TestPublicDashboard_GetResourceName(t *testing.T) {
	tests := []struct {
		name     string
		pd       publicdashboards.PublicDashboard
		wantName string
	}{
		{
			name:     "returns UID",
			pd:       publicdashboards.PublicDashboard{UID: "pd-abc123", DashboardUID: "dash-xyz"},
			wantName: "pd-abc123",
		},
		{
			name:     "empty UID returns empty",
			pd:       publicdashboards.PublicDashboard{DashboardUID: "dash-xyz"},
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pd.GetResourceName(); got != tt.wantName {
				t.Errorf("GetResourceName() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestPublicDashboard_SetResourceName(t *testing.T) {
	pd := &publicdashboards.PublicDashboard{DashboardUID: "dash-xyz"}
	pd.SetResourceName("pd-abc123")
	if pd.UID != "pd-abc123" {
		t.Errorf("UID = %q, want %q", pd.UID, "pd-abc123")
	}
	// Parent UID must be preserved.
	if pd.DashboardUID != "dash-xyz" {
		t.Errorf("DashboardUID = %q, want %q", pd.DashboardUID, "dash-xyz")
	}
}

func TestPublicDashboard_RoundTripIdentity(t *testing.T) {
	original := publicdashboards.PublicDashboard{UID: "pd-abc123", DashboardUID: "dash-xyz"}
	name := original.GetResourceName()

	restored := &publicdashboards.PublicDashboard{}
	restored.SetResourceName(name)

	if restored.UID != original.UID {
		t.Errorf("round-trip UID = %q, want %q", restored.UID, original.UID)
	}
}
