package oncall

import (
	"testing"

	"github.com/grafana/grafanactl/internal/resources/adapter"
)

// Compile-time assertions for all 17 OnCall types.
var (
	_ adapter.ResourceIdentity = &Integration{}
	_ adapter.ResourceIdentity = &EscalationChain{}
	_ adapter.ResourceIdentity = &EscalationPolicy{}
	_ adapter.ResourceIdentity = &Schedule{}
	_ adapter.ResourceIdentity = &Shift{}
	_ adapter.ResourceIdentity = &Team{}
	_ adapter.ResourceIdentity = &IntegrationRoute{}
	_ adapter.ResourceIdentity = &OutgoingWebhook{}
	_ adapter.ResourceIdentity = &AlertGroup{}
	_ adapter.ResourceIdentity = &User{}
	_ adapter.ResourceIdentity = &PersonalNotificationRule{}
	_ adapter.ResourceIdentity = &UserGroup{}
	_ adapter.ResourceIdentity = &SlackChannel{}
	_ adapter.ResourceIdentity = &Alert{}
	_ adapter.ResourceIdentity = &ResolutionNote{}
	_ adapter.ResourceIdentity = &ShiftSwap{}
	_ adapter.ResourceIdentity = &Organization{}
)

func TestOnCallTypes_ResourceIdentity(t *testing.T) {
	tests := []struct {
		name string
		ri   adapter.ResourceIdentity
	}{
		{"Integration", &Integration{ID: "XYZ"}},
		{"EscalationChain", &EscalationChain{ID: "XYZ"}},
		{"EscalationPolicy", &EscalationPolicy{ID: "XYZ"}},
		{"Schedule", &Schedule{ID: "XYZ"}},
		{"Shift", &Shift{ID: "XYZ"}},
		{"Team", &Team{ID: "XYZ"}},
		{"IntegrationRoute", &IntegrationRoute{ID: "XYZ"}},
		{"OutgoingWebhook", &OutgoingWebhook{ID: "XYZ"}},
		{"AlertGroup", &AlertGroup{ID: "XYZ"}},
		{"User", &User{ID: "XYZ"}},
		{"PersonalNotificationRule", &PersonalNotificationRule{ID: "XYZ"}},
		{"UserGroup", &UserGroup{ID: "XYZ"}},
		{"SlackChannel", &SlackChannel{ID: "XYZ"}},
		{"Alert", &Alert{ID: "XYZ"}},
		{"ResolutionNote", &ResolutionNote{ID: "XYZ"}},
		{"ShiftSwap", &ShiftSwap{ID: "XYZ"}},
		{"Organization", &Organization{ID: "XYZ"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ri.GetResourceName(); got != "XYZ" {
				t.Errorf("%s.GetResourceName() = %q, want %q", tt.name, got, "XYZ")
			}
			tt.ri.SetResourceName("ABC")
			if got := tt.ri.GetResourceName(); got != "ABC" {
				t.Errorf("%s after SetResourceName: GetResourceName() = %q, want %q", tt.name, got, "ABC")
			}
		})
	}
}
