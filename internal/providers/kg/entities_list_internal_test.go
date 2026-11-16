package kg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// catalogOnly is the type counts of a window without entities: the graph's
// own catalog records are still counted.
var catalogOnly = map[string]int64{"Account": 1, "EntityType": 12, "Schema": 1} //nolint:gochecknoglobals // test fixture

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

func TestCheckEntityType(t *testing.T) {
	schema := map[string][]string{
		"Service": {"name", "namespace", "workload_type"},
		"Pod":     {"name", "namespace", "node"},
		"Volume":  {"name"},
	}
	api := svc("api", "deployment")
	api.Scope = map[string]string{"env": "prod", "namespace": "ns1"}
	db := svc("db", "statefulset")
	db.Scope = map[string]string{"env": "prod", "namespace": "ns2"}
	dup1 := svc("dup", "deployment")
	dup1.Scope = map[string]string{"env": "prod", "namespace": "ns1"}
	dup2 := svc("dup", "deployment")
	dup2.Scope = map[string]string{"env": "prod", "namespace": "ns2"}
	entities := map[string][]SearchResult{
		"Service": {api, db, dup1, dup2},
		"Pod":     {{Name: "api-1", Type: "Pod"}},
	}
	counts := map[string]int64{"Service": 4, "Pod": 1, "Volume": 0}

	tests := []struct {
		name            string
		fake            *fakeKG
		entityType      string
		check           typeCheck
		want            string
		wantSummary     string
		wantDetails     string
		wantSuggestions []string
		wantNoSuggest   string
		// wantFirst is the first suggestion; wantNoText must not appear in
		// the summary or details.
		wantFirst  string
		wantNoText string
	}{
		{name: "type with entities in the window", entityType: "Service", want: "Service"},
		{name: "letter case is corrected", entityType: "service", want: "Service"},
		{name: "surrounding whitespace is trimmed", entityType: " Service ", want: "Service"},
		{name: "schema type without entities is kept", entityType: "Volume", want: "Volume"},
		{name: "schema type case is corrected", entityType: "volume", want: "Volume"},
		{name: "count API error skips the check", fake: &fakeKG{countErr: true}, entityType: "Nope", want: "Nope"},
		{
			name:        "unknown type names a kind property match",
			entityType:  "StatefulSet",
			check:       typeCheck{verb: "list"},
			wantSummary: `entity type "StatefulSet" is not in the graph in the window`,
			wantDetails: "Entity types in this window: Pod, Service, Volume",
			wantSuggestions: []string{
				"gcx kg entities list --type Service --property workload_type=statefulset --from ",
				"gcx kg meta schema --from ",
			},
		},
		{
			name:       "unknown type with a name adds the caller's flags to the list command",
			entityType: "Deployment",
			check: typeCheck{
				verb:   "list",
				name:   "api",
				search: typeSearch{propertyFilters: []PropertyMatcher{{Name: "name", Op: "=", Value: "api"}}},
				flags:  []string{"--property name=api", "--insight any", "--namespace ns1"},
			},
			wantSummary: `entity type "Deployment" is not in the graph`,
			wantSuggestions: []string{
				"gcx kg entities list --type Service --property workload_type=deployment --property name=api --insight any --namespace ns1 --from ",
			},
			wantNoSuggest: "--type Service --property name=api",
		},
		{
			name:       "inspect suggests the one entity under the type it has, with its scope",
			entityType: "Deployment",
			check: typeCheck{
				verb:   "inspect",
				name:   "api",
				search: typeSearch{propertyFilters: []PropertyMatcher{{Name: "name", Op: "=", Value: "api"}}},
				flags:  []string{"--env prod"},
			},
			wantSummary:     `entity type "Deployment" is not in the graph`,
			wantSuggestions: []string{"gcx kg entities inspect Service--api --env prod --namespace ns1 --from "},
		},
		{
			name:       "inspect keeps the caller's scope when the name has several entities",
			entityType: "Deployment",
			check: typeCheck{
				verb:   "inspect",
				name:   "dup",
				search: typeSearch{propertyFilters: []PropertyMatcher{{Name: "name", Op: "=", Value: "dup"}}},
				flags:  []string{"--env prod"},
			},
			wantSummary:     `entity type "Deployment" is not in the graph`,
			wantSuggestions: []string{"gcx kg entities inspect Service--dup --env prod --from "},
		},
		{
			name:       "a name found under another type without a kind match",
			entityType: "Container",
			check: typeCheck{
				verb:   "inspect",
				name:   "api-1",
				search: typeSearch{propertyFilters: []PropertyMatcher{{Name: "name", Op: "=", Value: "api-1"}}},
			},
			wantSummary:     `entity type "Container" is not in the graph`,
			wantSuggestions: []string{"gcx kg entities inspect Pod--api-1 --from "},
			wantNoSuggest:   "Service--api-1",
		},
		{
			name:          "no probe matches leaves only the schema pointer",
			entityType:    "Widget",
			check:         typeCheck{verb: "list"},
			wantSummary:   `entity type "Widget" is not in the graph`,
			wantNoSuggest: "These return matching entities",
		},
		{
			name:        "empty default window",
			fake:        &fakeKG{counts: catalogOnly, schema: map[string][]string{}},
			entityType:  "Service",
			check:       typeCheck{verb: "list"},
			wantSummary: "no entities of any type in the window",
			wantDetails: "the window is the last hour",
		},
		{
			name:        "unknown type in the default window says the window is the last hour",
			fake:        &fakeKG{counts: map[string]int64{"K6Test": 1}, schema: map[string][]string{"K6Test": {"name"}}},
			entityType:  "Service",
			check:       typeCheck{verb: "list"},
			wantSummary: `entity type "Service" is not in the graph in the window`,
			wantDetails: "Without --from/--to or --since, the window is the last hour. Entity types in this window: K6Test",
			wantFirst:   "Pass --from/--to covering the time you are asking about",
		},
		{
			name:        "unknown type in a window the caller chose does not blame the window",
			fake:        &fakeKG{counts: map[string]int64{"K6Test": 1}, schema: map[string][]string{"K6Test": {"name"}}},
			entityType:  "Service",
			check:       typeCheck{verb: "list", windowGiven: true},
			wantSummary: `entity type "Service" is not in the graph in the window`,
			wantDetails: "Entity types in this window: K6Test",
			wantFirst:   "Run 'gcx kg meta schema --from ",
			wantNoText:  "last hour",
		},
		{
			name:       "empty window the caller chose keeps the empty result",
			fake:       &fakeKG{counts: catalogOnly, schema: map[string][]string{}},
			entityType: "Service",
			check:      typeCheck{verb: "list", windowGiven: true},
			want:       "Service",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.fake
			if f == nil {
				f = &fakeKG{}
			}
			if f.counts == nil && !f.countErr {
				f.counts = counts
			}
			if f.schema == nil {
				f.schema = schema
			}
			if f.entities == nil {
				f.entities = entities
			}
			client := newFakeClient(t, f)
			c := tt.check
			c.search.startMs, c.search.endMs = 1000, 2000
			got, err := checkEntityType(t.Context(), client, tt.entityType, c)
			if tt.wantSummary == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
				return
			}
			var de gcxerrors.DetailedError
			require.True(t, errors.As(err, &de), "want DetailedError, got %v", err)
			assert.Contains(t, de.Summary, tt.wantSummary)
			assert.Contains(t, de.Details, tt.wantDetails)
			joined := strings.Join(de.Suggestions, "\n")
			for _, s := range tt.wantSuggestions {
				assert.Contains(t, joined, s)
			}
			if tt.wantNoSuggest != "" {
				assert.NotContains(t, joined, tt.wantNoSuggest)
			}
			if tt.wantFirst != "" {
				require.NotEmpty(t, de.Suggestions)
				assert.True(t, strings.HasPrefix(de.Suggestions[0], tt.wantFirst), "first suggestion %q", de.Suggestions[0])
			}
			if tt.wantNoText != "" {
				assert.NotContains(t, de.Summary+"\n"+de.Details, tt.wantNoText)
			}
		})
	}
}

// TestProbeTypeAlternativesBudget covers a schema with more candidate probes
// than the budgets allow: types are probed most entities first, name probes
// have their own budget, and types without entities or outside the schema are
// not probed.
func TestProbeTypeAlternativesBudget(t *testing.T) {
	schema := map[string][]string{}
	counts := map[string]int64{}
	for i := range 20 {
		typ := fmt.Sprintf("A%02d", i) // sorts before the type that holds the entity
		schema[typ] = []string{"name", "a_type", "b_kind"}
		counts[typ] = int64(i + 1)
	}
	schema["Empty"] = []string{"name", "x_type"}
	counts["Empty"] = 0
	schema["Zeta"] = []string{"name"}
	counts["Zeta"] = 1000
	counts["EntityType"] = 5000 // a catalog record, not in the schema
	f := &fakeKG{
		counts:   counts,
		schema:   schema,
		entities: map[string][]SearchResult{"Zeta": {{Name: "target", Type: "Zeta"}}},
	}
	client := newFakeClient(t, f)
	_, err := checkEntityType(t.Context(), client, "Microservice", typeCheck{
		verb:   "list",
		name:   "target",
		search: typeSearch{startMs: 1000, endMs: 2000, propertyFilters: []PropertyMatcher{{Name: "name", Op: "=", Value: "target"}}},
		flags:  []string{"--property name=target"},
		// A window was given, so the first suggestion is a probe's.
		windowGiven: true,
	})
	var de gcxerrors.DetailedError
	require.True(t, errors.As(err, &de), "want DetailedError, got %v", err)
	assert.Contains(t, de.Suggestions[0], "gcx kg entities list --type Zeta --property name=target --from ")
	require.Len(t, f.searchReq, maxKindProbes+maxNameProbes)
	probed := map[string]bool{}
	for _, req := range f.searchReq {
		probed[req.FilterCriteria[0].EntityType] = true
	}
	assert.True(t, probed["A19"], "the most populous A type is probed")
	assert.False(t, probed["A00"], "the least populous types fall outside the budget")
	assert.False(t, probed["Empty"], "a type without entities is not probed")
	assert.False(t, probed["EntityType"], "a type outside the schema is not probed")
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
