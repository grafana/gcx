package irm_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
