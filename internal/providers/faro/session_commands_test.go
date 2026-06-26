package faro_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	faro "github.com/grafana/gcx/internal/providers/faro"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionTableCodec_Encode(t *testing.T) {
	rows := []faro.SessionTableRow{
		{
			RecordingID:       "rec-001",
			Status:            "complete",
			Duration:          2*time.Minute + 30*time.Second,
			DurationHuman:     (2*time.Minute + 30*time.Second).Truncate(time.Second).String(),
			Segments:          5,
			InactivityPeriods: 1,
		},
		{
			RecordingID:       "rec-002",
			Status:            "active",
			Duration:          45 * time.Second,
			DurationHuman:     (45 * time.Second).String(),
			Segments:          2,
			InactivityPeriods: 0,
		},
	}

	codec := &faro.SessionTableCodec{}
	var buf bytes.Buffer

	err := codec.Encode(&buf, rows)
	require.NoError(t, err)

	output := buf.String()

	// Check headers.
	assert.Contains(t, output, "RECORDING ID")
	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "DURATION")
	assert.Contains(t, output, "SEGMENTS")
	assert.Contains(t, output, "INACTIVITY PERIODS")

	// Check row values.
	assert.Contains(t, output, "rec-001")
	assert.Contains(t, output, "complete")
	assert.Contains(t, output, "5")
	assert.Contains(t, output, "1")

	assert.Contains(t, output, "rec-002")
	assert.Contains(t, output, "active")
	assert.Contains(t, output, "2")
	assert.Contains(t, output, "0")

	// Check format name.
	assert.Equal(t, faro.FormatText, codec.Format())
}

func TestSessionTableCodec_EncodeEmpty(t *testing.T) {
	codec := &faro.SessionTableCodec{}
	var buf bytes.Buffer

	err := codec.Encode(&buf, []faro.SessionTableRow{})
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "No recordings found.")
}

func TestSessionTableCodec_EncodeWrongType(t *testing.T) {
	codec := &faro.SessionTableCodec{}
	var buf bytes.Buffer

	err := codec.Encode(&buf, "not a slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []SessionTableRow")
}

func TestSessionTableCodec_Decode(t *testing.T) {
	codec := &faro.SessionTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestShowSessionCommandRegistered(t *testing.T) {
	p := &faro.FaroProvider{}
	cmds := p.Commands()

	require.Len(t, cmds, 1)
	faroCmd := cmds[0]

	var appsCmd *cobra.Command
	for _, c := range faroCmd.Commands() {
		if c.Use == "apps" {
			appsCmd = c
			break
		}
	}
	require.NotNil(t, appsCmd, "expected apps subcommand")

	subCmds := make(map[string]bool)
	for _, c := range appsCmd.Commands() {
		subCmds[c.Name()] = true
	}

	assert.True(t, subCmds["show-session"], "missing show-session subcommand")
}

func TestEventTypeName(t *testing.T) {
	tests := []struct {
		typeCode int
		expected string
	}{
		{0, "DomContentLoaded"},
		{1, "Load"},
		{2, "FullSnapshot"},
		{3, "IncrementalSnapshot"},
		{4, "Meta"},
		{5, "Custom"},
		{6, "Plugin"},
		{99, "Unknown(99)"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, faro.EventTypeName(tt.typeCode), "type %d", tt.typeCode)
	}
}

func TestIncrementalSourceName(t *testing.T) {
	tests := []struct {
		source   int
		expected string
	}{
		{0, "Mutation"},
		{1, "MouseMove"},
		{2, "MouseInteraction"},
		{3, "Scroll"},
		{5, "Input"},
		{14, "Selection"},
		{99, "Unknown(99)"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, faro.IncrementalSourceName(tt.source), "source %d", tt.source)
	}
}

func TestSegmentSummaryCodec_Encode(t *testing.T) {
	rows := []faro.EventSummaryRow{
		{Index: 0, Timestamp: time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), TypeName: "FullSnapshot", Source: "-"},
		{Index: 1, Timestamp: time.Date(2026, 5, 18, 10, 0, 1, 0, time.UTC), TypeName: "IncrementalSnapshot", Source: "MouseMove"},
	}

	codec := &faro.SegmentSummaryCodec{}
	assert.Equal(t, faro.FormatText, codec.Format())

	var buf bytes.Buffer
	err := codec.Encode(&buf, rows)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "INDEX")
	assert.Contains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "SOURCE")
	assert.Contains(t, out, "FullSnapshot")
	assert.Contains(t, out, "MouseMove")
}

func TestSegmentSummaryCodec_EncodeEmpty(t *testing.T) {
	codec := &faro.SegmentSummaryCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, []faro.EventSummaryRow{})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No events")
}

func TestShowSegmentCommandRegistered(t *testing.T) {
	p := &faro.FaroProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	frontendCmd := cmds[0]
	appsCmd, _, err := frontendCmd.Find([]string{"apps"})
	require.NoError(t, err)

	var segmentCmd *cobra.Command
	for _, sub := range appsCmd.Commands() {
		if sub.Name() == "show-segment" {
			segmentCmd = sub
			break
		}
	}
	require.NotNil(t, segmentCmd, "show-segment command should be registered under apps")

	f := segmentCmd.Flags().Lookup("recording-id")
	require.NotNil(t, f, "show-segment should have --recording-id flag")
	assert.Empty(t, f.DefValue)
}

func TestEventsToSummaryRows(t *testing.T) {
	events := []faro.RRWebEvent{
		{Type: 4, Timestamp: 1700000000000, Data: json.RawMessage(`{}`)},
		{Type: 2, Timestamp: 1700000001000, Data: json.RawMessage(`{}`)},
		{Type: 3, Timestamp: 1700000002000, Data: json.RawMessage(`{"source":1}`)},
	}

	rows := faro.EventsToSummaryRows(events)
	require.Len(t, rows, 3)

	assert.Equal(t, 0, rows[0].Index)
	assert.Equal(t, "Meta", rows[0].TypeName)
	assert.Equal(t, "-", rows[0].Source)

	assert.Equal(t, 1, rows[1].Index)
	assert.Equal(t, "FullSnapshot", rows[1].TypeName)

	assert.Equal(t, 2, rows[2].Index)
	assert.Equal(t, "IncrementalSnapshot", rows[2].TypeName)
	assert.Equal(t, "MouseMove", rows[2].Source)
}
