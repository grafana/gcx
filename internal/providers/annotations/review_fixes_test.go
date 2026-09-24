package annotations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/annotations"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteDeclined(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_AUTO_APPROVE", "false")
	for _, tt := range []struct {
		name  string
		build func(annotations.RESTConfigLoader) *cobra.Command
		args  []string
	}{
		{"delete", annotations.NewDeleteCommandForTest, []string{"42"}},
		{"delete-many", annotations.NewMassDeleteCommandForTest, []string{"--dashboard-uid", "dashboard", "--panel-id", "3"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.build(mutationLoader{host: "http://127.0.0.1:1"})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader("no\n"))
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			var detailed gcxerrors.DetailedError
			require.ErrorAs(t, err, &detailed)
			require.NotNil(t, detailed.ExitCode)
			assert.Equal(t, gcxerrors.ExitCancelled, *detailed.ExitCode)
			assert.Empty(t, stdout.String())
		})
	}
}

func TestUpdatePreservesExplicitEmptyFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/api/annotations/42", r.URL.Path)
		data, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"tags":[],"text":"","timeEnd":0}`, string(data))
		_, _ = w.Write([]byte(`{"message":"updated"}`))
	}))
	defer srv.Close()
	cmd := annotations.NewUpdateCommandForTest(mutationLoader{host: srv.URL})
	cmd.SetIn(strings.NewReader(`{"tags":[],"text":"","timeEnd":0,"id":99}`))
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"42", "-f", "-", "-o", "json"})
	require.NoError(t, cmd.Execute())
}

func TestProviderHasNoResourceRegistration(t *testing.T) {
	require.Empty(t, annotations.NewAnnotationsProvider().TypedRegistrations())
}

func TestListTimeFlags(t *testing.T) {
	for _, tt := range []struct {
		name             string
		args             []string
		wantFrom, wantTo string
		wantError        bool
	}{
		{name: "days", args: []string{"--since", "7d"}},
		{name: "RFC3339", args: []string{"--from", "2024-01-01T00:00:00Z", "--to", "2024-01-02T00:00:00Z"}, wantFrom: "1704067200000", wantTo: "1704153600000"},
		{name: "empty from", args: []string{"--from", ""}, wantError: true},
		{name: "empty to", args: []string{"--to", ""}, wantError: true},
		{name: "empty since", args: []string{"--since", ""}, wantError: true},
		{name: "negative since", args: []string{"--since", "-1h"}, wantError: true},
		{name: "reversed range", args: []string{"--from", "2000", "--to", "1000"}, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rs := newRecordingServer(t)
			err := runListCmd(t, rs, tt.args...)
			if tt.wantError {
				require.Error(t, err)
				require.Nil(t, rs.request)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, rs.request)
			if tt.wantFrom != "" {
				assert.Equal(t, tt.wantFrom, rs.request.URL.Query().Get("from"))
				assert.Equal(t, tt.wantTo, rs.request.URL.Query().Get("to"))
			}
		})
	}
}

func TestListRetainsServerFields(t *testing.T) {
	const payload = `[{"id":42,"dashboardId":7,"userId":8,"login":"user","email":"user@example.com","created":1,"updated":2,"alertId":9,"type":"annotation","data":{"key":"value"}}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer srv.Close()
	client := newClient(t, srv.URL)
	rows, err := client.List(context.Background(), annotations.ListOptions{})
	require.NoError(t, err)
	encoded, err := json.Marshal(rows)
	require.NoError(t, err)
	assert.JSONEq(t, payload, string(encoded))
}

func TestMassDeleteSelector(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		wantError bool
	}{
		{name: "trimmed dashboard UID", args: []string{"--dashboard-uid", " dash ", "--panel-id", "3"}},
		{name: "dashboard UID", args: []string{"--dashboard-uid", "dash", "--panel-id", "3"}},
		{name: "dashboard ID", args: []string{"--dashboard-id", "7", "--panel-id", "3"}},
		{name: "duplicate selectors", args: []string{"--dashboard-uid", "dash", "--dashboard-id", "7", "--panel-id", "3"}, wantError: true},
		{name: "missing panel", args: []string{"--dashboard-uid", "dash"}, wantError: true},
		{name: "empty UID", args: []string{"--dashboard-uid", "", "--panel-id", "3"}, wantError: true},
		{name: "blank UID", args: []string{"--dashboard-uid", " ", "--panel-id", "3"}, wantError: true},
		{name: "old annotation flag", args: []string{"--annotation-id", "42"}, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/annotations/mass-delete", r.URL.Path)
				var payload map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				assert.NotContains(t, payload, "annotationId")
				assert.EqualValues(t, 3, payload["panelId"])
				if tt.name == "dashboard UID" || tt.name == "trimmed dashboard UID" {
					assert.Equal(t, "dash", payload["dashboardUID"])
				} else {
					assert.EqualValues(t, 7, payload["dashboardId"])
				}
				_, _ = w.Write([]byte(`{"message":"deleted"}`))
			}))
			defer srv.Close()
			cmd := annotations.NewMassDeleteCommandForTest(mutationLoader{host: srv.URL})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(append(tt.args, "--force", "-o", "json"))
			err := cmd.Execute()
			if tt.wantError {
				require.Error(t, err)
				assert.False(t, called)
			} else {
				require.NoError(t, err)
				assert.True(t, called)
			}
		})
	}
}

func TestListEmptyAndLimitWarning(t *testing.T) {
	for _, tt := range []struct {
		name, payload string
		truncated     bool
	}{{"empty", "[]", false}, {"limit", `[{"id":42}]`, true}} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tt.payload)) }))
			defer srv.Close()
			cmd := annotations.NewListCommandForTest(mutationLoader{host: srv.URL})
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"--limit", "1", "-o", "json"})
			require.NoError(t, cmd.Execute())
			if tt.truncated {
				assert.Contains(t, stderr.String(), "may be incomplete")
			} else {
				assert.JSONEq(t, "[]", stdout.String())
				assert.Empty(t, stderr.String())
			}
		})
	}
}

func TestUpdateRejectsIDOnlyPatch(t *testing.T) {
	cmd := annotations.NewUpdateCommandForTest(mutationLoader{host: "http://127.0.0.1:1"})
	cmd.SetIn(strings.NewReader(`{"id":42}`))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"42", "-f", "-"})
	require.ErrorContains(t, cmd.Execute(), "field other than id")
}
