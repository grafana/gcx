package alert_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveStateHistory answers GET /api/v1/rules/history with the shared frame
// fixture, recording the query it received.
func serveStateHistory(gotQuery *string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stateHistoryFrameJSON))
	}
}

func TestStateHistoryListOutputContract(t *testing.T) {
	t.Run("agent mode emits one JSON value", func(t *testing.T) {
		setAgentMode(t, true)

		var query string
		loader := newAlertFixture(t, serveStateHistory(&query))
		stdout, _, err := runCmdSplit(t, alert.NewStateHistoryListCommandForTest(loader),
			[]string{"list", "--rule", "uid-1"}, "")
		require.NoError(t, err)

		doc := decodeSingleJSONDocument(t, stdout)
		items, ok := doc.([]any)
		require.True(t, ok, "state-history result should be a JSON array, got %T: %q", doc, stdout)
		require.Len(t, items, 2)
		record, ok := items[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "uid-1", record["ruleUid"])
		assert.Equal(t, "Alerting", record["current"], "newest transition should sort first")
	})

	t.Run("human default renders the table", func(t *testing.T) {
		setAgentMode(t, false)

		var query string
		loader := newAlertFixture(t, serveStateHistory(&query))
		stdout, _, err := runCmdSplit(t, alert.NewStateHistoryListCommandForTest(loader),
			[]string{"list"}, "")
		require.NoError(t, err)

		// The table header is stable regardless of row content.
		assert.Contains(t, stdout, "PREVIOUS")
		assert.Contains(t, stdout, "CURRENT")
		assert.Contains(t, stdout, "CPU Usage")
	})

	t.Run("flags translate to alerting query params", func(t *testing.T) {
		setAgentMode(t, false)

		var query string
		loader := newAlertFixture(t, serveStateHistory(&query))
		_, _, err := runCmdSplit(t, alert.NewStateHistoryListCommandForTest(loader),
			[]string{"list", "--rule", "uid-1", "--limit", "25", "--label", "severity=critical"}, "")
		require.NoError(t, err)

		assert.Contains(t, query, "ruleUID=uid-1")
		assert.Contains(t, query, "limit=25")
		assert.Contains(t, query, "labels_severity=critical")
	})
}

func TestStateHistoryListValidation(t *testing.T) {
	setAgentMode(t, false)

	cases := []struct {
		name string
		args []string
	}{
		{"malformed label", []string{"list", "--label", "novalue"}},
		{"negative limit", []string{"list", "--limit", "-1"}},
		{"unparsable from", []string{"list", "--from", "notatime"}},
		{"to before from", []string{"list", "--from", "now", "--to", "now-1h"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loader := newAlertFixture(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected API call on invalid input: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			})
			stdout, _, err := runCmdSplit(t, alert.NewStateHistoryListCommandForTest(loader), tc.args, "")
			require.Error(t, err)
			assert.Empty(t, stdout, "validation failures must not write to stdout")
		})
	}
}

// TestStateHistoryTableCodec pins the wide/narrow column shape independent of
// the command wiring.
func TestStateHistoryTableCodec(t *testing.T) {
	rows := []alert.StateTransition{{
		RuleUID:   "uid-1",
		RuleTitle: "CPU Usage",
		Previous:  "Normal",
		Current:   "Alerting",
		Labels:    map[string]string{"severity": "critical"},
	}}

	var narrow bytes.Buffer
	require.NoError(t, (&alert.StateHistoryTableCodec{}).Encode(&narrow, rows))
	assert.Contains(t, narrow.String(), "CURRENT")
	assert.NotContains(t, narrow.String(), "RULE_UID")

	var wide bytes.Buffer
	require.NoError(t, (&alert.StateHistoryTableCodec{Wide: true}).Encode(&wide, rows))
	assert.Contains(t, wide.String(), "RULE_UID")
	assert.Contains(t, wide.String(), "uid-1")
}
