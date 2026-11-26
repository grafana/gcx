package alert_test

import (
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListRejectsNegativeLimit(t *testing.T) {
	setAgentMode(t, false)

	cases := []struct {
		name string
		cmd  func(alert.GrafanaConfigLoader) *cobra.Command
	}{
		{"rules", alert.NewRulesListCommandForTest},
		{"groups", alert.NewGroupsListCommandForTest},
		{"templates", alert.NewTemplatesListCommandForTest},
		{"contact-points", alert.NewContactPointsListCommandForTest},
		{"mute-timings", alert.NewMuteTimingsListCommandForTest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loader := newAlertFixture(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected API call on invalid input: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			})
			stdout, _, err := runCmdSplit(t, tc.cmd(loader), []string{"list", "--limit", "-1"}, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--limit must not be negative")
			assert.Empty(t, stdout)
		})
	}
}
