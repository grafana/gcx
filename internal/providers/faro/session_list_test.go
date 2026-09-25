package faro //nolint:testpackage // Tests the unexported replay session parser and codec.

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractReplaySessionRows(t *testing.T) {
	resp := &loki.QueryResponse{
		Status: "success",
		Data: loki.QueryResultData{
			ResultType: "streams",
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app_id": "4"},
					Values: []loki.LogEntry{
						{Timestamp: "1779187750000000000", Line: `kind=event session_id=sess-1 browser_name=Chrome browser_version=136.0 app_name=my-app`},
						{Timestamp: "1779187740000000000", Line: `kind=event session_id=sess-2 browser_name=Firefox browser_version=137.0 app_name=other-app`},
						{Timestamp: "1779187730000000000", Line: `kind=event session_id=sess-1 browser_name=Chrome browser_version=136.0 app_name=my-app`},
					},
				},
			},
		},
	}

	rows := extractReplaySessionRows(resp)
	require.Len(t, rows, 2)

	assert.Equal(t, "sess-1", rows[0].SessionID, "most recent session should be first")
	assert.Equal(t, "Chrome 136.0", rows[0].Browser)
	assert.Equal(t, "my-app", rows[0].AppName)

	assert.Equal(t, "sess-2", rows[1].SessionID)
	assert.Equal(t, "Firefox 137.0", rows[1].Browser)
}

func TestExtractReplaySessionRows_QuotedLogfmt(t *testing.T) {
	resp := &loki.QueryResponse{
		Status: "success",
		Data: loki.QueryResultData{
			ResultType: "streams",
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app_id": "4"},
					Values: []loki.LogEntry{
						{Timestamp: "1779187750000000000", Line: `kind=event session_id=sess-1 browser_name="Brave Browser" browser_version=1.0 app_name="my app"`},
					},
				},
			},
		},
	}

	rows := extractReplaySessionRows(resp)
	require.Len(t, rows, 1)
	assert.Equal(t, "Brave Browser 1.0", rows[0].Browser)
	assert.Equal(t, "my app", rows[0].AppName)
}

func TestExtractReplaySessionRows_EmptyBrowserName(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: []loki.LogEntry{{
					Timestamp: "1779187750000000000",
					Line:      `session_id=sess-1 browser_version=1.0`,
				}},
			}},
		},
	}

	rows := extractReplaySessionRows(resp)
	require.Len(t, rows, 1)
	assert.Equal(t, "1.0", rows[0].Browser)
}

func TestExtractReplaySessionRows_Empty(t *testing.T) {
	resp := &loki.QueryResponse{
		Status: "success",
		Data: loki.QueryResultData{
			ResultType: "streams",
			Result:     nil,
		},
	}

	rows := extractReplaySessionRows(resp)
	assert.Empty(t, rows)
}

func TestReplaySessionTable_Encode(t *testing.T) {
	rows := []replaySessionListRow{
		{SessionID: "sess-1", Browser: "Chrome 136.0", AppName: "my-app", LastSeen: "2026-05-19T10:00:00Z"},
	}

	codec := replaySessionTableCodec{table: replaySessionTable().Codec(cmdio.FormatText)}
	var buf bytes.Buffer
	err := codec.Encode(&buf, replaySessionListResult{Items: rows})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "SESSION ID")
	assert.Contains(t, out, "BROWSER")
	assert.Contains(t, out, "sess-1")
	assert.Contains(t, out, "Chrome 136.0")
}

func TestReplaySessionTable_EncodeEmpty(t *testing.T) {
	codec := replaySessionTableCodec{table: replaySessionTable().Codec(cmdio.FormatText)}
	var buf bytes.Buffer
	err := codec.Encode(&buf, replaySessionListResult{Items: []replaySessionListRow{}})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No session replays")
}

func TestReplaySessionListEnvelopeCarriesTruncation(t *testing.T) {
	testutils.SetAgentMode(t, false)
	result := replaySessionListResult{
		Items:    []replaySessionListRow{{SessionID: "sess-1"}},
		ListMeta: &cmdio.ListMeta{Truncated: true, Returned: 1, Cap: lokiEventsPageSize},
	}
	var output bytes.Buffer
	opts := cmdio.Options{OutputFormat: "json"}
	err := opts.Encode(&output, result)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[{"session_id":"sess-1","browser":"","app_name":"","last_seen":""}],"list_meta":{"truncated":true,"returned":1,"cap":1000}}`, output.String())
	result.ListMeta = nil
	output.Reset()
	err = opts.Encode(&output, result)
	require.NoError(t, err)
	assert.NotContains(t, output.String(), "list_meta")
}

func TestListReplaySessionsCommandRegistered(t *testing.T) {
	p := &FaroProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	frontendCmd := cmds[0]
	appsCmd, _, err := frontendCmd.Find([]string{"apps"})
	require.NoError(t, err)

	found := false
	for _, sub := range appsCmd.Commands() {
		if sub.Name() == "list-replay-sessions" {
			found = true
			break
		}
	}
	assert.True(t, found, "list-replay-sessions command should be registered under apps")
}

func TestListReplaySessionsRejectsInvalidFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "zero limit", args: []string{"my-web-app-42", "--limit", "0"}, wantErr: "--limit must be positive"},
		{name: "negative limit", args: []string{"my-web-app-42", "--limit", "-1"}, wantErr: "--limit must be positive"},
		{name: "invalid since", args: []string{"my-web-app-42", "--since", "not-a-duration"}, wantErr: "invalid --since value"},
		{name: "zero since", args: []string{"my-web-app-42", "--since", "0s"}, wantErr: "--since must be positive"},
		{name: "negative since", args: []string{"my-web-app-42", "--since=-1h"}, wantErr: "--since must be positive"},
		{name: "bare app name", args: []string{"my-web-app"}, wantErr: "expected a numeric ID or slug-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &FaroProvider{}
			cmds := p.Commands()
			require.Len(t, cmds, 1)

			cmd := cmds[0]
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(append([]string{"apps", "list-replay-sessions"}, tt.args...))

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
