package newrelic_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/datasources/newrelic"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runQuery(t *testing.T, loader *providers.ConfigLoader, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "test"}
	root.AddCommand(newrelic.QueryCmd(loader))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"query"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestQueryRejectsInvalidInputsBeforeIO(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{}, "expression is required"},
		{[]string{"  "}, "NRQL expression"},
		{[]string{"SELECT 1", "--account-id=0"}, "--account-id"},
		{[]string{"SELECT 1", "--account-id=-1"}, "--account-id"},
		{[]string{"SELECT 1", "--timeout-seconds=4"}, "--timeout-seconds"},
		{[]string{"SELECT 1", "--timeout-seconds=121"}, "--timeout-seconds"},
		{[]string{"SELECT 1", "--datasource="}, "--datasource"},
		{[]string{"SELECT 1", "--from=now-1h"}, "--to"},
		{[]string{"SELECT 1", "--from=invalid", "--to=now"}, "invalid --from time"},
		{[]string{"SELECT 1", "--from=now-1h", "--to=invalid"}, "invalid --to time"},
		{[]string{"SELECT 1", "-o=graph"}, "graph"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := runQuery(t, &providers.ConfigLoader{}, tt.args...)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestQueryCommandFormatsAndLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bootdata":
			http.NotFound(w, r)
		case r.URL.Path == "/api/datasources/uid/nr-uid":
			_, _ = w.Write([]byte(`{"id":1,"uid":"nr-uid","type":"nrgrafanaplugin-newrelic-datasource"}`))
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"name":"counts","fields":[{"name":"count","type":"number"}]},"data":{"values":[[42]]}},{"schema":{"name":"logs","fields":[{"name":"message","type":"string"}]},"data":{"values":[["hello"]]}}]}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(testutils.CreateTempFile(t, fmt.Sprintf(`current-context: test
contexts:
  test:
    grafana:
      server: %s
      token: test-token
      org-id: 1
`, srv.URL)))
	for _, tt := range []struct {
		args []string
		want []string
	}{
		{[]string{"--since", "2h"}, []string{"counts", "logs", "42", "hello"}},
		{[]string{"-o", "wide"}, []string{"counts", "logs", "42", "hello"}},
		{[]string{"-o", "yaml"}, []string{"frames:", "counts", "logs"}},
		{[]string{"-o", "json", "--share-link", "--since", "2h"}, []string{`"frames"`, "Explore link:", "queryText"}},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			out, err := runQuery(t, loader, append([]string{"-d", "nr-uid", "SELECT count(*) FROM Transaction"}, tt.args...)...)
			require.NoError(t, err)
			for _, want := range tt.want {
				assert.Contains(t, out, want)
			}
		})
	}
}

func TestQueryCommandEmptyFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bootdata":
			http.NotFound(w, r)
		case r.URL.Path == "/api/datasources/uid/nr-uid":
			_, _ = w.Write([]byte(`{"id":1,"uid":"nr-uid","type":"nrgrafanaplugin-newrelic-datasource"}`))
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"results":{"A":{"status":200}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(testutils.CreateTempFile(t, fmt.Sprintf(`current-context: test
contexts:
  test:
    grafana:
      server: %s
      token: test-token
      org-id: 1
`, srv.URL)))
	out, err := runQuery(t, loader, "-d", "nr-uid", "SELECT count(*) FROM Transaction")
	require.NoError(t, err)
	assert.Contains(t, out, "No data")
	out, err = runQuery(t, loader, "-d", "nr-uid", "SELECT count(*) FROM Transaction", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, out, `"frames": []`)
}

func TestQueryCommandResultsAndType(t *testing.T) {
	for _, tt := range []struct {
		name    string
		dsType  string
		wantErr string
	}{
		{"plugin", "nrgrafanaplugin-newrelic-datasource", ""},
		{"enterprise rejected", "grafana-newrelic-datasource", "not newrelic"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/bootdata":
					http.NotFound(w, r)
				case r.URL.Path == "/api/datasources/uid/nr-uid":
					_, _ = fmt.Fprintf(w, `{"id":1,"uid":"nr-uid","name":"New Relic","type":%q}`, tt.dsType)
				case r.Method == http.MethodPost:
					posts++
					var body struct {
						Queries []map[string]any `json:"queries"`
					}
					if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) || !assert.Len(t, body.Queries, 1) {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					assert.Equal(t, "SELECT count(*) FROM Transaction", body.Queries[0]["queryText"])
					_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"count","type":"number"}]},"data":{"values":[[42]]}}]}}}`))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(srv.Close)
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(testutils.CreateTempFile(t, fmt.Sprintf(`version: 1
current-context: test
stacks:
  test:
    grafana:
      server: %q
      token: test-token
      org-id: 1
contexts:
  test:
    stack: test
`, srv.URL)))
			out, err := runQuery(t, loader, "-d", "nr-uid", "SELECT count(*) FROM Transaction", "-o", "json")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Zero(t, posts)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, posts)
			assert.Contains(t, out, `"frames"`)
			assert.Contains(t, out, `42`)
		})
	}
}
