package faro_test

import (
	"bytes"
	"testing"

	faro "github.com/grafana/gcx/internal/providers/faro"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractSessionRows(t *testing.T) {
	resp := &loki.QueryResponse{
		Status: "success",
		Data: loki.QueryResultData{
			ResultType: "streams",
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app_id": "4"},
					Values: [][]string{
						{"1779187750000000000", `kind=event session_id=sess-1 browser_name=Chrome browser_version=136.0 app_name=my-app`},
						{"1779187740000000000", `kind=event session_id=sess-2 browser_name=Firefox browser_version=137.0 app_name=other-app`},
						{"1779187730000000000", `kind=event session_id=sess-1 browser_name=Chrome browser_version=136.0 app_name=my-app`},
					},
				},
			},
		},
	}

	rows := faro.ExtractSessionRows(resp)
	require.Len(t, rows, 2)

	assert.Equal(t, "sess-1", rows[0].SessionID, "most recent session should be first")
	assert.Equal(t, "Chrome 136.0", rows[0].Browser)
	assert.Equal(t, "my-app", rows[0].AppName)

	assert.Equal(t, "sess-2", rows[1].SessionID)
	assert.Equal(t, "Firefox 137.0", rows[1].Browser)
}

func TestExtractSessionRows_QuotedLogfmt(t *testing.T) {
	resp := &loki.QueryResponse{
		Status: "success",
		Data: loki.QueryResultData{
			ResultType: "streams",
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app_id": "4"},
					Values: [][]string{
						{"1779187750000000000", `kind=event session_id=sess-1 browser_name="Brave Browser" browser_version=1.0 app_name="my app"`},
					},
				},
			},
		},
	}

	rows := faro.ExtractSessionRows(resp)
	require.Len(t, rows, 1)
	assert.Equal(t, "Brave Browser 1.0", rows[0].Browser)
	assert.Equal(t, "my app", rows[0].AppName)
}

func TestExtractSessionRows_Empty(t *testing.T) {
	resp := &loki.QueryResponse{
		Status: "success",
		Data: loki.QueryResultData{
			ResultType: "streams",
			Result:     nil,
		},
	}

	rows := faro.ExtractSessionRows(resp)
	assert.Empty(t, rows)
}

func TestSessionListCodec_Encode(t *testing.T) {
	rows := []faro.SessionListRow{
		{SessionID: "sess-1", Browser: "Chrome 136.0", AppName: "my-app", LastSeen: "2026-05-19T10:00:00Z"},
	}

	codec := &faro.SessionListCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, rows)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "SESSION ID")
	assert.Contains(t, out, "BROWSER")
	assert.Contains(t, out, "sess-1")
	assert.Contains(t, out, "Chrome 136.0")
}

func TestSessionListCodec_EncodeEmpty(t *testing.T) {
	codec := &faro.SessionListCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, []faro.SessionListRow{})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No sessions")
}

func TestListSessionsCommandRegistered(t *testing.T) {
	p := &faro.FaroProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	frontendCmd := cmds[0]
	appsCmd, _, err := frontendCmd.Find([]string{"apps"})
	require.NoError(t, err)

	found := false
	for _, sub := range appsCmd.Commands() {
		if sub.Name() == "list-sessions" {
			found = true
			break
		}
	}
	assert.True(t, found, "list-sessions command should be registered under apps")
}
