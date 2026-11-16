package kg

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// fakeKG serves the KG endpoints that 'entities list' and 'entities inspect'
// call: entity type counts, schema, search, entity lookup and llm-summary.
type fakeKG struct {
	counts   map[string]int64
	countErr bool
	schema   map[string][]string // entity type -> properties
	entities map[string][]SearchResult
	pageSize int
	// failPage, when > 0, makes that search page fail with a 500.
	failPage int

	mu          sync.Mutex
	searchReq   []SearchRequest
	countCalls  int
	schemaCalls int
}

func (f *fakeKG) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/entity_type/count"):
			f.mu.Lock()
			f.countCalls++
			f.mu.Unlock()
			if f.countErr {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(f.counts)
		case strings.HasSuffix(r.URL.Path, "/v1/entity"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/llm-summary"):
			var req LLMSummaryRequest
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			var graph []any
			for _, e := range f.entities[req.EntityKeys[0].Type] {
				if e.Name == req.EntityKeys[0].Name {
					graph = append(graph, map[string]any{"name": e.Name})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"graphData": graph})
		case strings.HasSuffix(r.URL.Path, "/v1/search/sample"):
			_ = json.NewEncoder(w).Encode(map[string]any{"entities": []any{}})
		case strings.HasSuffix(r.URL.Path, "/v1/search"):
			f.search(t, w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (f *fakeKG) search(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var raw map[string]any
	assert.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
	if _, ok := raw["definitionId"]; ok {
		f.mu.Lock()
		f.schemaCalls++
		f.mu.Unlock()
		var ents []map[string]any
		for typ, props := range f.schema {
			p := map[string]any{}
			for _, name := range props {
				p[name] = "x"
			}
			ents = append(ents, map[string]any{"name": typ, "properties": p})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"entities": ents}})
		return
	}
	b, _ := json.Marshal(raw)
	var req SearchRequest
	assert.NoError(t, json.Unmarshal(b, &req))
	f.mu.Lock()
	f.searchReq = append(f.searchReq, req)
	f.mu.Unlock()
	if f.failPage > 0 && req.PageNum == f.failPage {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	m := req.FilterCriteria[0]
	var rows []SearchResult
	for _, e := range f.entities[m.EntityType] {
		if matchesAll(e, m.PropertyMatchers) {
			rows = append(rows, e)
		}
	}
	size := f.pageSize
	if size == 0 {
		size = 1000
	}
	from := min(req.PageNum*size, len(rows))
	to := min(from+size, len(rows))
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"entities": rows[from:to],
		"pageNum":  req.PageNum,
		"lastPage": to >= len(rows),
	}})
}

func matchesAll(e SearchResult, ms []PropertyMatcher) bool {
	for _, m := range ms {
		if m.Op != "=" {
			continue
		}
		var got string
		if m.Name == "name" {
			got = e.Name
		} else {
			got, _ = e.Properties[m.Name].(string)
		}
		if got != m.Value {
			return false
		}
	}
	return true
}

func newFakeClient(t *testing.T, f *fakeKG) *Client {
	t.Helper()
	server := httptest.NewServer(f.handler(t))
	t.Cleanup(server.Close)
	c, err := NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "stack-123"})
	require.NoError(t, err)
	return c
}

func svc(name, kind string) SearchResult {
	return SearchResult{Name: name, Type: "Service", Properties: map[string]any{"workload_type": kind}}
}

func scoped(name, env, namespace string) SearchResult {
	sc := map[string]string{}
	if env != "" {
		sc["env"] = env
	}
	if namespace != "" {
		sc["namespace"] = namespace
	}
	return SearchResult{Name: name, Type: "Service", Scope: sc}
}

func names(rs []SearchResult) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestSearchByTypesAllPages(t *testing.T) {
	var five []SearchResult
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		five = append(five, svc(n, map[bool]string{true: "statefulset", false: "deployment"}[n == "b" || n == "e"]))
	}
	statefulSets := func(rs []SearchResult) []SearchResult {
		var out []SearchResult
		for _, r := range rs {
			if r.Properties["workload_type"] == "statefulset" {
				out = append(out, r)
			}
		}
		return out
	}
	tests := []struct {
		name       string
		limit      int64
		maxPages   int
		failPage   int
		keep       func([]SearchResult) []SearchResult
		wantNames  []string
		wantPages  int
		wantErr    string
		wantStderr string
	}{
		{name: "limit 0 follows every page", limit: 0, wantNames: []string{"a", "b", "c", "d", "e"}, wantPages: 3},
		{name: "stops once the limit is reached", limit: 2, wantNames: []string{"a", "b"}, wantPages: 1},
		{name: "a limit past the first page fetches the next", limit: 3, wantNames: []string{"a", "b", "c", "d"}, wantPages: 2},
		{name: "client-side filter counts kept rows against the limit", limit: 2, keep: statefulSets, wantNames: []string{"b", "e"}, wantPages: 3},
		{
			name: "stops at the page bound", limit: 0, maxPages: 2,
			wantNames: []string{"a", "b", "c", "d"}, wantPages: 2, wantStderr: "stopped after 2 backend pages",
		},
		{name: "a failed later page aborts the listing", limit: 0, failPage: 1, wantPages: 2, wantErr: "listing Service entities failed on backend page 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeKG{entities: map[string][]SearchResult{"Service": five}, pageSize: 2, failPage: tt.failPage}
			client := newFakeClient(t, f)
			var stderr bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetErr(&stderr)
			maxPages := tt.maxPages
			if maxPages == 0 {
				maxPages = maxListPages
			}
			got, err := searchByTypesAllPages(t.Context(), cmd, client, []string{"Service"}, typeSearch{startMs: 1000, endMs: 2000, keep: tt.keep}, tt.limit, maxPages)
			assert.Len(t, f.searchReq, tt.wantPages)
			for i, req := range f.searchReq {
				assert.Equal(t, i, req.PageNum)
			}
			if tt.wantErr != "" {
				var de gcxerrors.DetailedError
				require.True(t, errors.As(err, &de), "want DetailedError, got %v", err)
				assert.Contains(t, de.Summary, tt.wantErr)
				assert.Contains(t, de.Details, "Pages 0-0 returned 2 rows")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantNames, names(got))
			assert.Contains(t, stderr.String(), tt.wantStderr)
		})
	}
}

func TestSearchByTypesAllPagesFirstPageServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "stack-123"})
	require.NoError(t, err)
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	got, err := searchByTypesAllPages(t.Context(), cmd, client, []string{"Service"}, typeSearch{startMs: 1000, endMs: 2000}, 0, maxListPages)
	require.NoError(t, err, "a server error on the first page skips the type, as searchByTypes does")
	assert.Empty(t, got)
	assert.Contains(t, stderr.String(), `skipping entity type "Service"`)
}

// TestSearchByTypesAllPagesSkipsRepeats covers a backend whose pages overlap:
// a row that an earlier page returned is skipped and does not count against
// the limit.
func TestSearchByTypesAllPagesSkipsRepeats(t *testing.T) {
	withID := func(id int, name string) SearchResult {
		return SearchResult{ID: id, Name: name, Type: "Service"}
	}
	tests := []struct {
		name      string
		rows      []SearchResult
		limit     int64
		wantNames []string
		wantPages int
	}{
		{
			name: "by id", limit: 0,
			rows:      []SearchResult{withID(1, "a"), withID(2, "b"), withID(2, "b"), withID(3, "c")},
			wantNames: []string{"a", "b", "c"}, wantPages: 2,
		},
		{
			name: "a repeat does not count against the limit", limit: 3,
			rows:      []SearchResult{withID(1, "a"), withID(2, "b"), withID(2, "b"), withID(3, "c"), withID(4, "d")},
			wantNames: []string{"a", "b", "c"}, wantPages: 2,
		},
		{
			name: "by type, name and scope without an id", limit: 0,
			rows:      []SearchResult{scoped("a", "prod", "ns1"), scoped("b", "prod", "ns1"), scoped("b", "prod", "ns1"), scoped("b", "prod", "ns2")},
			wantNames: []string{"a", "b", "b"}, wantPages: 2,
		},
		{
			name: "rows with the same name and another id are kept", limit: 0,
			rows:      []SearchResult{withID(1, "a"), withID(2, "a"), withID(3, "a")},
			wantNames: []string{"a", "a", "a"}, wantPages: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeKG{entities: map[string][]SearchResult{"Service": tt.rows}, pageSize: 2}
			client := newFakeClient(t, f)
			got, err := searchByTypesAllPages(t.Context(), &cobra.Command{}, client, []string{"Service"}, typeSearch{startMs: 1000, endMs: 2000}, tt.limit, maxListPages)
			require.NoError(t, err)
			assert.Equal(t, tt.wantNames, names(got))
			assert.Len(t, f.searchReq, tt.wantPages)
		})
	}
}
