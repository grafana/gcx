package oncall_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/oncall"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Compile-time assertions for all 17 OnCall types.
var (
	_ adapter.ResourceIdentity = &oncall.Integration{}
	_ adapter.ResourceIdentity = &oncall.EscalationChain{}
	_ adapter.ResourceIdentity = &oncall.EscalationPolicy{}
	_ adapter.ResourceIdentity = &oncall.Schedule{}
	_ adapter.ResourceIdentity = &oncall.Shift{}
	_ adapter.ResourceIdentity = &oncall.Team{}
	_ adapter.ResourceIdentity = &oncall.IntegrationRoute{}
	_ adapter.ResourceIdentity = &oncall.OutgoingWebhook{}
	_ adapter.ResourceIdentity = &oncall.AlertGroup{}
	_ adapter.ResourceIdentity = &oncall.User{}
	_ adapter.ResourceIdentity = &oncall.PersonalNotificationRule{}
	_ adapter.ResourceIdentity = &oncall.UserGroup{}
	_ adapter.ResourceIdentity = &oncall.SlackChannel{}
	_ adapter.ResourceIdentity = &oncall.Alert{}
	_ adapter.ResourceIdentity = &oncall.ResolutionNote{}
	_ adapter.ResourceIdentity = &oncall.ShiftSwap{}
	_ adapter.ResourceIdentity = &oncall.Organization{}
)

func TestOnCallTypes_ResourceIdentity(t *testing.T) {
	tests := []struct {
		name string
		ri   adapter.ResourceIdentity
	}{
		{"Integration", &oncall.Integration{ID: "XYZ"}},
		{"EscalationChain", &oncall.EscalationChain{ID: "XYZ"}},
		{"EscalationPolicy", &oncall.EscalationPolicy{ID: "XYZ"}},
		{"Schedule", &oncall.Schedule{ID: "XYZ"}},
		{"Shift", &oncall.Shift{ID: "XYZ"}},
		{"Team", &oncall.Team{ID: "XYZ"}},
		{"IntegrationRoute", &oncall.IntegrationRoute{ID: "XYZ"}},
		{"OutgoingWebhook", &oncall.OutgoingWebhook{ID: "XYZ"}},
		{"AlertGroup", &oncall.AlertGroup{ID: "XYZ"}},
		{"User", &oncall.User{ID: "XYZ"}},
		{"PersonalNotificationRule", &oncall.PersonalNotificationRule{ID: "XYZ"}},
		{"UserGroup", &oncall.UserGroup{ID: "XYZ"}},
		{"SlackChannel", &oncall.SlackChannel{ID: "XYZ"}},
		{"Alert", &oncall.Alert{ID: "XYZ"}},
		{"ResolutionNote", &oncall.ResolutionNote{ID: "XYZ"}},
		{"ShiftSwap", &oncall.ShiftSwap{ID: "XYZ"}},
		{"Organization", &oncall.Organization{ID: "XYZ"}},
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
