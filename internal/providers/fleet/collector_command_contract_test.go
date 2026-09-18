package fleet //nolint:testpackage // Tests unexported Fleet command constructors.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorCommandsDiscoverSchemaFieldsWithoutAPICall(t *testing.T) {
	tests := []struct {
		name string
		cmd  func(*fleetHelper) *cobra.Command
		args []string
	}{
		{
			name: "list",
			cmd:  func(h *fleetHelper) *cobra.Command { return h.newCollectorListCommand() },
			args: []string{"--json", "list"},
		},
		{
			name: "get",
			cmd:  func(h *fleetHelper) *cobra.Command { return h.newCollectorGetCommand() },
			args: []string{"collector-1", "--json", "list"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmd(&fleetHelper{})
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.Execute())

			fields := strings.Fields(stdout.String())
			assert.Contains(t, fields, "spec.local_attributes")
			assert.Contains(t, fields, "spec.remote_attributes")
			assert.Contains(t, fields, "spec.collector_type")
			assert.Contains(t, fields, "spec.created_at")
			assert.Contains(t, fields, "spec.updated_at")
			assert.Contains(t, fields, "spec.marked_inactive_at")
		})
	}
}

func TestCollectorListSelectsHealthFields(t *testing.T) {
	server := newCollectorReadServer(t)
	t.Cleanup(server.Close)

	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newCollectorListCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{
		"--limit", "0",
		"--json", "spec.id,spec.local_attributes,spec.remote_attributes,spec.updated_at",
	})
	require.NoError(t, cmd.Execute())

	var items []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &items))
	require.Len(t, items, 1)
	assert.Equal(t, "collector-1", items[0]["spec.id"])
	assert.Equal(t, map[string]any{"collector.os": "linux", "collector.version": "1.10.2"}, items[0]["spec.local_attributes"])
	assert.Equal(t, map[string]any{"env": "production"}, items[0]["spec.remote_attributes"])
	assert.Equal(t, "2026-09-18T11:12:13Z", items[0]["spec.updated_at"])
}

func TestCollectorGetSupportsWideOutput(t *testing.T) {
	server := newCollectorReadServer(t)
	t.Cleanup(server.Close)

	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newCollectorGetCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"collector-1", "-o", "wide"})
	require.NoError(t, cmd.Execute())

	output := stdout.String()
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "UPDATED_AT")
	assert.Contains(t, output, "LOCAL_ATTRIBUTES")
	assert.Contains(t, output, "1.10.2")
	assert.Contains(t, output, "2026-09-18 11:12")
}

func TestCollectorListRejectsNegativeLimitBeforeAPICall(t *testing.T) {
	cmd := (&fleetHelper{}).newCollectorListCommand()
	cmd.SetArgs([]string{"--limit", "-1"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit")
}

func newCollectorReadServer(t *testing.T) *httptest.Server {
	t.Helper()
	collector := map[string]any{
		"id": "collector-1", "name": "production", "collectorType": "COLLECTOR_TYPE_ALLOY", "enabled": true,
		"localAttributes":  map[string]string{"collector.version": "1.10.2", "collector.os": "linux"},
		"remoteAttributes": map[string]string{"env": "production"},
		"createdAt":        "2026-08-01T10:00:00Z", "updatedAt": "2026-09-18T11:12:13Z",
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, pathListCollectors):
			writeContractJSON(t, w, map[string]any{"collectors": []map[string]any{collector}})
		case strings.HasSuffix(r.URL.Path, pathGetCollector):
			writeContractJSON(t, w, collector)
		default:
			http.NotFound(w, r)
		}
	}))
}
