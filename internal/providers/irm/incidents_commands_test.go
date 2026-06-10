package irm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// ---------------------------------------------------------------------------
// IncidentTableCodec tests
// ---------------------------------------------------------------------------

func TestIncidentTableCodec_Encode(t *testing.T) {
	t0 := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	ft := irm.FlexTime(t0)

	incs := []irm.Incident{
		{
			IncidentID:  "inc-001",
			Title:       "Database outage in production",
			Status:      "active",
			Severity:    "critical",
			CreatedTime: ft,
		},
		{
			IncidentID:  "inc-002",
			Title:       "Minor latency spike",
			Status:      "resolved",
			Severity:    "",
			CreatedTime: irm.FlexTime(time.Time{}),
		},
	}

	tests := []struct {
		name        string
		wide        bool
		wantColumns []string
		wantRows    []string
	}{
		{
			name:        "table format shows standard columns",
			wide:        false,
			wantColumns: []string{"INCIDENTID", "TITLE", "STATUS", "SEVERITY", "CREATED"},
			wantRows:    []string{"inc-001", "Database outage", "active", "critical", "2024-06-15 10:30"},
		},
		{
			name:        "wide format includes TYPE column",
			wide:        true,
			wantColumns: []string{"INCIDENTID", "TITLE", "STATUS", "SEVERITY", "TYPE", "CREATED"},
			wantRows:    []string{"inc-001", "Database outage", "active", "critical", "2024-06-15 10:30"},
		},
		{
			name:        "missing severity shows dash",
			wide:        false,
			wantColumns: []string{"INCIDENTID", "TITLE", "STATUS", "SEVERITY"},
			wantRows:    []string{"inc-002", "Minor latency spike", "resolved", "-"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := &irm.IncidentTableCodec{Wide: tt.wide}
			var buf bytes.Buffer
			err := codec.Encode(&buf, incs)
			require.NoError(t, err)

			output := buf.String()
			for _, col := range tt.wantColumns {
				assert.Contains(t, output, col, "column %q should appear in header", col)
			}
			for _, row := range tt.wantRows {
				assert.Contains(t, output, row, "value %q should appear in output", row)
			}
		})
	}
}

func TestIncidentTableCodec_EncodeWrongType(t *testing.T) {
	codec := &irm.IncidentTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice-of-incidents")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []Incident")
}

func TestIncidentTableCodec_TitleTruncation(t *testing.T) {
	longTitle := strings.Repeat("A", 60)
	incs := []irm.Incident{
		{
			IncidentID: "inc-trunc",
			Title:      longTitle,
			Status:     "active",
		},
	}

	codec := &irm.IncidentTableCodec{Wide: false}
	var buf bytes.Buffer
	err := codec.Encode(&buf, incs)
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, longTitle, "long title should be truncated in table mode")
	assert.Contains(t, output, "...", "truncated title should end with ...")
}

func TestIncidentTableCodec_WideTitleNotTruncated(t *testing.T) {
	longTitle := strings.Repeat("A", 60)
	incs := []irm.Incident{
		{
			IncidentID: "inc-wide",
			Title:      longTitle,
			Status:     "active",
		},
	}

	codec := &irm.IncidentTableCodec{Wide: true}
	var buf bytes.Buffer
	err := codec.Encode(&buf, incs)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, longTitle, "wide mode should not truncate title")
}

func TestIncidentTableCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string((&irm.IncidentTableCodec{}).Format()))
	assert.Equal(t, "wide", string((&irm.IncidentTableCodec{Wide: true}).Format()))
}

func TestIncidentTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &irm.IncidentTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

// ---------------------------------------------------------------------------
// ActivityTableCodec tests
// ---------------------------------------------------------------------------

func TestActivityTableCodec_Encode(t *testing.T) {
	items := []irm.ActivityItem{
		{
			ActivityItemID: "act-001",
			IncidentID:     "inc-123",
			ActivityKind:   "userNote",
			Body:           "This is a note",
			EventTime:      "2024-06-15T10:30:00Z",
			User:           irm.ActivityUser{UserID: "u-1", Name: "Alice"},
		},
		{
			ActivityItemID: "act-002",
			IncidentID:     "inc-123",
			ActivityKind:   "statusChange",
			Body:           "Status changed to resolved",
			CreatedTime:    "2024-06-15T11:00:00Z",
			User:           irm.ActivityUser{UserID: "u-2", Name: "Bob"},
		},
	}

	codec := &irm.ActivityTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, items)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "ID")
	assert.Contains(t, output, "KIND")
	assert.Contains(t, output, "USER")
	assert.Contains(t, output, "act-001")
	assert.Contains(t, output, "userNote")
	assert.Contains(t, output, "Alice")
	assert.Contains(t, output, "This is a note")
}

func TestActivityTableCodec_LongBodyTruncated(t *testing.T) {
	longBody := strings.Repeat("X", 80)
	items := []irm.ActivityItem{
		{
			ActivityItemID: "act-long",
			ActivityKind:   "userNote",
			Body:           longBody,
		},
	}

	codec := &irm.ActivityTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, items)
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, longBody, "long body should be truncated")
	assert.Contains(t, output, "...", "truncated body should end with ...")
}

// ---------------------------------------------------------------------------
// SeverityTableCodec tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// IncidentContextTableCodec tests
// ---------------------------------------------------------------------------

func TestIncidentContextTableCodec_Encode(t *testing.T) {
	alertGroup := "ag-99"
	contexts := []irm.IncidentContext{
		{
			// Alert-group links come back as genericURL contexts with
			// alertGroupID set — there is no dedicated alertGroup type
			// in the IRM API.
			ContextID:    "ctx-001",
			IncidentID:   "inc-123",
			Type:         "genericURL",
			Status:       "attached",
			Title:        "Alert Group",
			AlertGroupID: &alertGroup,
			CreatedTime:  "2024-06-15T10:30:00Z",
		},
		{
			ContextID:  "ctx-002",
			IncidentID: "inc-123",
			Type:       "grafana.dashboard",
		},
	}

	tests := []struct {
		name        string
		wide        bool
		wantColumns []string
		wantInRows  []string
	}{
		{
			name:        "table format shows standard columns",
			wide:        false,
			wantColumns: []string{"CONTEXTID", "TYPE", "STATUS", "ALERTGROUPID", "TITLE"},
			wantInRows:  []string{"ctx-001", "genericURL", "attached", "ag-99", "Alert Group", "ctx-002", "grafana.dashboard"},
		},
		{
			name:        "wide format includes CREATED column",
			wide:        true,
			wantColumns: []string{"CONTEXTID", "TYPE", "STATUS", "ALERTGROUPID", "TITLE", "CREATED"},
			wantInRows:  []string{"2024-06-15T10:30"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := &irm.IncidentContextTableCodec{Wide: tt.wide}
			var buf bytes.Buffer
			err := codec.Encode(&buf, contexts)
			require.NoError(t, err)

			out := buf.String()
			for _, col := range tt.wantColumns {
				assert.Contains(t, out, col)
			}
			for _, want := range tt.wantInRows {
				assert.Contains(t, out, want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// label validation tests
// ---------------------------------------------------------------------------

func TestListOpts_LabelValidation(t *testing.T) {
	tests := []struct {
		name    string
		labels  []string
		wantErr string
	}{
		{
			// Labels under the default Tags key are matched by plain text,
			// so values without a colon are valid.
			name:   "plain label text passes",
			labels: []string{"security", "PIR not needed"},
		},
		{
			// Keyed labels are matched by their key:value composite.
			name:   "key:value text passes through verbatim",
			labels: []string{"team:platform"},
		},
		{
			name:    "empty label fails",
			labels:  []string{"security", ""},
			wantErr: "label must not be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := irm.NewTestListCommand(tt.labels, "", "")
			cmd.SetArgs([]string{})
			err := cmd.Execute()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// fakeGrafanaConfigLoader implements irm.GrafanaConfigLoader for command tests.
type fakeGrafanaConfigLoader struct {
	cfg config.NamespacedRESTConfig
}

func (l fakeGrafanaConfigLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return l.cfg, nil
}

// TestIncidentsListCommand_BuildsQuery runs the real list command against a
// fake API and asserts the filters land in the request as the API documents
// them: plain label text in incidentLabels and RFC3339 dates.
func TestIncidentsListCommand_BuildsQuery(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	var query map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query map[string]any `json:"query"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		query = body.Query
		writeJSON(w, map[string]any{
			"incidents": []map[string]any{
				{"incidentID": "inc-1", "title": "Security incident", "status": "active"},
			},
			"cursor": map[string]any{"hasMore": false},
		})
	}))
	t.Cleanup(server.Close)

	loader := fakeGrafanaConfigLoader{cfg: config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}}

	cmd := irm.NewListCommand(loader)
	cmd.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--labels", "security,important",
		"--from", "2024-06-01T00:00:00Z",
		"--to", "2024-06-15T00:00:00Z",
		"--limit", "10",
	})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, []any{"security", "important"}, query["incidentLabels"])
	assert.Equal(t, "2024-06-01T00:00:00Z", query["dateFrom"])
	assert.Equal(t, "2024-06-15T00:00:00Z", query["dateTo"])
	assert.InDelta(t, 10, query["limit"], 0)

	assert.Contains(t, stdout.String(), "inc-1")
	assert.Contains(t, stdout.String(), "Security incident")
}

func TestIncidentsListCommand_RejectsNonPositiveLimit(t *testing.T) {
	for _, limit := range []string{"0", "-5"} {
		t.Run("limit "+limit, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("API must not be called when validation fails")
			}))
			t.Cleanup(server.Close)

			cmd := irm.NewListCommand(fakeGrafanaConfigLoader{cfg: config.NamespacedRESTConfig{
				Config: rest.Config{Host: server.URL},
			}})
			cmd.SilenceUsage = true
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"--limit", limit})

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be at least 1")
		})
	}
}

func TestListOpts_DateValidation(t *testing.T) {
	tests := []struct {
		name     string
		dateFrom string
		dateTo   string
		wantErr  string
	}{
		{
			name: "no dates passes",
		},
		{
			name:     "valid RFC3339 from",
			dateFrom: "2024-06-15T10:30:00Z",
		},
		{
			name:   "valid relative to",
			dateTo: "now",
		},
		{
			name:     "valid relative range",
			dateFrom: "now-7d",
			dateTo:   "now",
		},
		{
			name:     "invalid from",
			dateFrom: "not-a-date",
			wantErr:  "invalid --from value",
		},
		{
			name:    "invalid to",
			dateTo:  "not-a-date",
			wantErr: "invalid --to value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := irm.NewTestListCommand(nil, tt.dateFrom, tt.dateTo)
			cmd.SetArgs([]string{})
			err := cmd.Execute()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SeverityTableCodec tests
// ---------------------------------------------------------------------------

func TestSeverityTableCodec_Encode(t *testing.T) {
	sevs := []irm.Severity{
		{SeverityID: "sev-1", DisplayLabel: "Critical", Level: 1, Color: "#FF0000"},
		{SeverityID: "sev-2", DisplayLabel: "High", Level: 2, Color: "#FF8800"},
		{SeverityID: "sev-3", DisplayLabel: "Low", Level: 3},
	}

	codec := &irm.SeverityTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, sevs)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "ID")
	assert.Contains(t, output, "LEVEL")
	assert.Contains(t, output, "LABEL")
	assert.Contains(t, output, "COLOR")
	assert.Contains(t, output, "sev-1")
	assert.Contains(t, output, "Critical")
	assert.Contains(t, output, "#FF0000")
	assert.Contains(t, output, "-")
}
