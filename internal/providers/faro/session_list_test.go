package faro_test

import (
	"bytes"
	"testing"

	faro "github.com/grafana/gcx/internal/providers/faro"
	"github.com/grafana/gcx/internal/query/loki"
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

	rows := faro.ExtractReplaySessionRows(resp)
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

	rows := faro.ExtractReplaySessionRows(resp)
	require.Len(t, rows, 1)
	assert.Equal(t, "Brave Browser 1.0", rows[0].Browser)
	assert.Equal(t, "my app", rows[0].AppName)
}

func TestExtractReplaySessionRows_Empty(t *testing.T) {
	resp := &loki.QueryResponse{
		Status: "success",
		Data: loki.QueryResultData{
			ResultType: "streams",
			Result:     nil,
		},
	}

	rows := faro.ExtractReplaySessionRows(resp)
	assert.Empty(t, rows)
}

func TestReplaySessionListCodec_Encode(t *testing.T) {
	rows := []faro.ReplaySessionListRow{
		{SessionID: "sess-1", Browser: "Chrome 136.0", AppName: "my-app", LastSeen: "2026-05-19T10:00:00Z"},
	}

	codec := &faro.ReplaySessionListCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, rows)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "SESSION ID")
	assert.Contains(t, out, "BROWSER")
	assert.Contains(t, out, "sess-1")
	assert.Contains(t, out, "Chrome 136.0")
}

func TestReplaySessionListCodec_EncodeEmpty(t *testing.T) {
	codec := &faro.ReplaySessionListCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, []faro.ReplaySessionListRow{})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No session replays")
}

func TestListReplaySessionsCommandRegistered(t *testing.T) {
	p := &faro.FaroProvider{}
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &faro.FaroProvider{}
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
