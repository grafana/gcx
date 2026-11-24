package tempo_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExceptionsCmd_GroupsSpanSample(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/proxy/uid/tempo-uid/api/search":
			gotQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"traces":[{
				"traceID":"t1",
				"startTimeUnixNano":"200",
				"spanSets":[{"spans":[
					{"spanID":"s1","startTimeUnixNano":"200","attributes":[
						{"key":"resource.service.name","value":{"stringValue":"checkout"}},
						{"key":"event.exception.message","value":{"stringValue":"timeout for user 123"}},
						{"key":"event.exception.type","value":{"stringValue":"TimeoutError"}}
					]},
					{"spanID":"s2","startTimeUnixNano":"100","attributes":[
						{"key":"resource.service.name","value":{"stringValue":"checkout"}},
						{"key":"event.exception.message","value":{"stringValue":"timeout for user 456"}},
						{"key":"event.exception.type","value":{"stringValue":"TimeoutError"}}
					]}
				]}]
			}]}`))
			assert.NoError(t, err)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	loader := tempoLoader(t, srv.URL)
	stdout, stderr, err := execTempoCmd(tempo.ExceptionsCmd(loader), []string{
		"exceptions", `{ resource.service.name = "checkout" }`, "--since", "1h", "--limit", "1", "-o", "json",
	})
	require.NoError(t, err)
	assert.Contains(t, gotQuery.Get("q"), `resource.service.name = "checkout"`)
	assert.Contains(t, gotQuery.Get("q"), "status = error")
	assert.Contains(t, gotQuery.Get("q"), "most_recent=true")
	assert.Equal(t, "1", gotQuery.Get("limit"))
	assert.Equal(t, "10", gotQuery.Get("spss"))
	assert.NotEmpty(t, gotQuery.Get("start"))
	assert.NotEmpty(t, gotQuery.Get("end"))
	assert.Contains(t, stdout, `"message": "timeout for user \u003cnum\u003e"`)
	assert.Contains(t, stdout, `"occurrences": 2`)
	assert.Contains(t, stdout, `"type": "TimeoutError"`)
	assert.Contains(t, stdout, `"service": "checkout"`)
	assert.Contains(t, stdout, `"url":`)
	assert.Contains(t, stdout, "grafana-exploretraces-app")
	assert.Contains(t, stdout, "actionView=exceptions")
	assert.Contains(t, stdout, "var-metric=errors")
	assert.Contains(t, stdout, "event.exception.type%7C%3D%7CTimeoutError")
	assert.Contains(t, stdout, `"traces_sampled": 1`)
	assert.Contains(t, stderr, "exception counts use the first 1 traces")
	assert.Regexp(t, `--limit['"\s]+2`, stderr)

	tableOut, _, err := execTempoCmd(tempo.ExceptionsCmd(loader), []string{
		"exceptions", `{ resource.service.name = "checkout" }`, "--since", "1h", "--limit", "1",
	})
	require.NoError(t, err)
	assert.Contains(t, tableOut, "SERVICE")
	assert.Contains(t, tableOut, "LINK")
	assert.Contains(t, tableOut, srv.URL)
	assert.Contains(t, tableOut, "actionView=exceptions")
	assert.Contains(t, tableOut, "event.exception.type%7C%3D%7CTimeoutError")
	assert.Contains(t, tableOut, "timeout for user <num>")
	assert.Contains(t, tableOut, "TimeoutError")
	assert.Contains(t, tableOut, "2")
}

// serveTempoSearch starts a Grafana stand-in whose Tempo search proxy answers
// every call with body, and returns its URL.
func serveTempoSearch(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/proxy/uid/tempo-uid/api/search":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(body))
			assert.NoError(t, err)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestExceptionsCmd_WarnsWhenTempoLeavesSpansOut(t *testing.T) {
	// One trace matched 18 spans and Tempo returned 1 of them.
	const cutOff = `{"traces":[{"traceID":"t1","startTimeUnixNano":"200","spanSets":[{"matched":18,"spans":[
		{"spanID":"s1","startTimeUnixNano":"200","attributes":[
			{"key":"resource.service.name","value":{"stringValue":"checkout"}},
			{"key":"event.exception.message","value":{"stringValue":"boom"}},
			{"key":"event.exception.type","value":{"stringValue":"TimeoutError"}}
		]}
	]}]}]}`
	const complete = `{"traces":[{"traceID":"t1","startTimeUnixNano":"200","spanSets":[{"matched":1,"spans":[
		{"spanID":"s1","startTimeUnixNano":"200","attributes":[
			{"key":"resource.service.name","value":{"stringValue":"checkout"}},
			{"key":"event.exception.message","value":{"stringValue":"boom"}},
			{"key":"event.exception.type","value":{"stringValue":"TimeoutError"}}
		]}
	]}]}]}`
	args := []string{"exceptions", `{ resource.service.name = "checkout" }`, "--since", "1h", "-o", "json"}

	t.Run("reports the shortfall in the result and on stderr", func(t *testing.T) {
		stdout, stderr, err := execTempoCmd(tempo.ExceptionsCmd(tempoLoader(t, serveTempoSearch(t, cutOff))), args)
		require.NoError(t, err)
		assert.Contains(t, stdout, `"spans_omitted": 17`)
		assert.Contains(t, stderr, "left out 17 matching spans")
		assert.Contains(t, stderr, "at most 10 per trace")
		assert.Contains(t, stderr, "a larger --limit will not help", "raising --limit must not be offered as the fix")
		assert.NotContains(t, stderr, "See a larger sample", "a trace-limit hint would point at the wrong remedy")
	})

	t.Run("stays quiet when every matching span came back", func(t *testing.T) {
		stdout, stderr, err := execTempoCmd(tempo.ExceptionsCmd(tempoLoader(t, serveTempoSearch(t, complete))), args)
		require.NoError(t, err)
		assert.Contains(t, stdout, `"spans_omitted": 0`)
		assert.NotContains(t, stderr, "left out")
	})
}

func TestExceptionsCmd_ErrorOnEmpty(t *testing.T) {
	const empty = `{"traces":[]}`
	const oneException = `{"traces":[{"traceID":"t1","startTimeUnixNano":"200","spanSets":[{"spans":[
		{"spanID":"s1","startTimeUnixNano":"200","attributes":[
			{"key":"resource.service.name","value":{"stringValue":"checkout"}},
			{"key":"event.exception.message","value":{"stringValue":"boom"}},
			{"key":"event.exception.type","value":{"stringValue":"TimeoutError"}}
		]}
	]}]}]}`
	args := func(extra ...string) []string {
		return append([]string{"exceptions", `{ resource.service.name = "checkout" }`, "--since", "1h", "-o", "json"}, extra...)
	}
	// The real root command sets SilenceUsage; without it cobra prints usage to
	// stdout on any error and hides what the command itself wrote.
	exceptionsCmd := func(t *testing.T, body string) *cobra.Command {
		t.Helper()
		cmd := tempo.ExceptionsCmd(tempoLoader(t, serveTempoSearch(t, body)))
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return cmd
	}

	t.Run("empty sample prints no result, only the error", func(t *testing.T) {
		stdout, _, err := execTempoCmd(exceptionsCmd(t, empty), args("--error-on-empty"))
		require.ErrorIs(t, err, dsquery.ErrNoResult)
		assert.Empty(t, stdout, "stdout must not carry a result before the error")
	})

	t.Run("empty sample still links to Explore", func(t *testing.T) {
		stdout, stderr, err := execTempoCmd(exceptionsCmd(t, empty), args("--error-on-empty", "--share-link"))
		require.ErrorIs(t, err, dsquery.ErrNoResult)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "Explore link:")
	})

	t.Run("empty sample is printed without the flag", func(t *testing.T) {
		stdout, _, err := execTempoCmd(exceptionsCmd(t, empty), args())
		require.NoError(t, err)
		assert.Contains(t, stdout, `"exceptions"`)
	})

	t.Run("non-empty sample is printed with the flag", func(t *testing.T) {
		stdout, _, err := execTempoCmd(exceptionsCmd(t, oneException), args("--error-on-empty"))
		require.NoError(t, err)
		assert.Contains(t, stdout, `"type": "TimeoutError"`)
	})
}

func TestExceptionsCmd_RejectsPipelineBeforeIO(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request %s", r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := execTempoCmd(tempo.ExceptionsCmd(tempoLoader(t, srv.URL)), []string{
		"exceptions", `{ status = error } | count()`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one TraceQL spanset")
	assert.Contains(t, err.Error(), `{ status = error } | count()`)
	var usage *gcxerrors.UsageError
	require.ErrorAs(t, err, &usage)
	require.NotEmpty(t, usage.Suggestions)
	assert.Contains(t, usage.Suggestions[0], "exceptions --help")
	assert.NotContains(t, stderr, "span search")
}

func TestExceptionsCmd_RejectsLimitAboveTheCap(t *testing.T) {
	_, _, err := execTempoCmd(tempo.ExceptionsCmd(&providers.ConfigLoader{}), []string{
		"exceptions", "--limit", "301",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit must be from 1 to 300")
	assert.Contains(t, err.Error(), "301")
}

func TestExceptionsExploreAndTabURLs(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	explore := tempo.ExceptionsExploreURL("https://stack.grafana.net", dsquery.ExploreQuery{
		DatasourceUID:  "tempo-uid",
		DatasourceType: "tempo",
		Expr:           `{ status = error } | select(resource.service.name) with(most_recent=true)`,
	}, 400)
	panes := mustParseURL(t, explore).Query().Get("panes")
	assert.Contains(t, panes, `"tableType":"spans"`)
	assert.Contains(t, panes, `"spss":10`)
	assert.Contains(t, panes, "most_recent")

	tab, ok := tempo.TracesExceptionsURL("https://stack.grafana.net", "tempo-uid", `{ resource.service.name = "checkout" }`, start, start.Add(time.Hour))
	require.True(t, ok)
	u, err := url.Parse(tab)
	require.NoError(t, err)
	assert.Equal(t, "errors", u.Query().Get("var-metric"))
	assert.Equal(t, "exceptions", u.Query().Get("actionView"))
	assert.Equal(t, []string{"resource.service.name|=|checkout"}, u.Query()["var-filters"])

	all, ok := tempo.TracesExceptionsURL("https://stack.grafana.net", "tempo-uid", "", start, start.Add(time.Hour))
	require.True(t, ok)
	u, err = url.Parse(all)
	require.NoError(t, err)
	assert.Equal(t, "exceptions", u.Query().Get("actionView"))
	assert.Empty(t, u.Query()["var-filters"])

	_, ok = tempo.TracesExceptionsURL("https://stack.grafana.net", "tempo-uid", `{ status = error } | count()`, start, start)
	assert.False(t, ok)
}

func tempoLoader(t *testing.T, server string) *providers.ConfigLoader {
	t.Helper()
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeTempoTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+server+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
    datasources:
      tempo: tempo-uid
current-context: default
`))
	return loader
}
