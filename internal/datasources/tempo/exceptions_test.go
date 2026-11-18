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
	assert.Contains(t, stderr, "--limit 2")

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
