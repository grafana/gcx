package remote //nolint:testpackage // White-box test for the dry-run allowlist internals.

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestDryRunAllowlist_Honors(t *testing.T) {
	dashboards := schema.GroupResource{Group: "dashboard.grafana.app", Resource: "dashboards"}
	folders := schema.GroupResource{Group: "folder.grafana.app", Resource: "folders"}
	playlists := schema.GroupResource{Group: "playlist.grafana.app", Resource: "playlists"}
	alertRules := schema.GroupResource{Group: "rules.alerting.grafana.app", Resource: "alertrules"}

	tests := []struct {
		name        string
		assumed     []string
		gr          schema.GroupResource
		wantHonored bool
		wantStatic  bool
	}{
		{name: "static dashboards", gr: dashboards, wantHonored: true, wantStatic: true},
		{name: "static folders", gr: folders, wantHonored: true, wantStatic: true},
		{name: "static playlists", gr: playlists, wantHonored: true, wantStatic: true},
		{name: "unknown alert rules denied by default", gr: alertRules, wantHonored: false},
		{
			name:        "user-asserted alert rules",
			assumed:     []string{"alertrules.rules.alerting.grafana.app"},
			gr:          alertRules,
			wantHonored: true,
			wantStatic:  false,
		},
		{
			name:        "static entry is never reported as user-asserted",
			assumed:     []string{"dashboards.dashboard.grafana.app"},
			gr:          dashboards,
			wantHonored: true,
			wantStatic:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, invalid := newDryRunAllowlist(tc.assumed)
			require.Empty(t, invalid)
			honored, static := a.classify(tc.gr)
			require.Equal(t, tc.wantHonored, honored)
			require.Equal(t, tc.wantStatic, static)
		})
	}
}

func TestParseGroupResource(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    schema.GroupResource
		wantErr bool
	}{
		{
			name: "resource and group",
			in:   "alertrules.rules.alerting.grafana.app",
			want: schema.GroupResource{Group: "rules.alerting.grafana.app", Resource: "alertrules"},
		},
		{name: "bare resource without group is rejected", in: "dashboards", wantErr: true},
		{name: "empty string is rejected", in: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGroupResource(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNewDryRunAllowlist_InvalidValueIsSkippedNotFatal(t *testing.T) {
	// A malformed value is reported as invalid and simply ignored, rather than failing the
	// whole operation.
	a, invalid := newDryRunAllowlist([]string{"not-a-group-resource", "alertrules.rules.alerting.grafana.app"})

	require.Equal(t, []string{"not-a-group-resource"}, invalid)

	honored, _ := a.classify(schema.GroupResource{Group: "rules.alerting.grafana.app", Resource: "alertrules"})
	require.True(t, honored, "the valid entry still takes effect")
}
