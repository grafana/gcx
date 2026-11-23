package kg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const testWindow = "--from=2026-01-01T00:00:00Z --to=2026-01-01T01:00:00Z"

// runEntities runs 'gcx kg entities <args>' against f and returns stdout,
// stderr and the error.
func runEntities(t *testing.T, f *fakeKG, args string) (string, string, error) {
	t.Helper()
	server := httptest.NewServer(f.handler(t))
	t.Cleanup(server.Close)
	loader := &FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "stack-123"}}
	cmd := newEntitiesCommand(loader)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(strings.Fields(args))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func entitiesFake(applyScope bool) *fakeKG {
	api := svc("api", "gadget")
	api.Scope = map[string]string{"env": "prod", "namespace": "ns1"}
	db := svc("db", "Widget")
	db.Scope = map[string]string{"env": "prod", "namespace": "ns2"}
	// gateway is a Service without a namespace.
	gateway := svc("gateway", "gadget")
	gateway.Scope = map[string]string{"env": "prod"}
	return &fakeKG{
		counts: map[string]int64{"Service": 3, "Node": 2},
		schema: map[string][]string{
			"Service": {"name", "namespace", "class"},
			"Node":    {"name"},
		},
		entities: map[string][]SearchResult{
			"Service": {api, db, gateway},
			// Nodes carry no namespace: a backend that applies --namespace
			// returns them.
			"Node": {
				{Name: "node-1", Type: "Node", Scope: map[string]string{"env": "prod"}, Assertion: map[string]any{"assertions": []any{map[string]any{"assertionName": "Saturation"}}}},
				{Name: "node-2", Type: "Node", Scope: map[string]string{"env": "dev"}},
			},
		},
		applyScope: applyScope,
	}
}

func TestEntitiesListCommand(t *testing.T) {
	tests := []struct {
		name            string
		fake            *fakeKG
		args            string
		wantNames       []string
		wantErr         string
		wantSuggestions []string
		wantCountCalls  int
		// wantStderr and wantNoStderr are checked against stderr.
		wantStderr   string
		wantNoStderr string
	}{
		{
			name:      "--page 0 applies the scope",
			args:      "list --type Service --page 0 --namespace ns1 -o json " + testWindow,
			wantNames: []string{"api"},
		},
		{
			name:      "--page 0 with none",
			args:      "list --type Service --page 0 --namespace none -o json " + testWindow,
			wantNames: []string{"gateway"},
		},
		{
			name:      "rows of a type without the dimension are kept",
			args:      "list --type Node --page 0 --namespace ns1 -o json " + testWindow,
			wantNames: []string{"node-1", "node-2"},
		},
		{
			name:      "a backend that applies the scope: output unchanged",
			fake:      entitiesFake(true),
			args:      "list --type Node --env prod --namespace ns1 -o json " + testWindow,
			wantNames: []string{"node-1"},
		},
		{
			name:      "a backend that applies the scope: a type with the dimension",
			fake:      entitiesFake(true),
			args:      "list --type Service --namespace ns1 -o json " + testWindow,
			wantNames: []string{"api"},
		},
		{
			name:      "a backend that applies the scope: --insight any",
			fake:      entitiesFake(true),
			args:      "list --type Node --namespace ns1 --insight any -o json " + testWindow,
			wantNames: []string{"node-1"},
		},
		{
			name:      "a backend that applies the scope: --namespace none",
			fake:      entitiesFake(true),
			args:      "list --type Node --namespace none -o json " + testWindow,
			wantNames: []string{"node-1", "node-2"},
		},
		{
			name:           "a name lookup does not hide that the type has a namespace",
			args:           "list --type Service --property name=gateway --namespace ns1 -o json " + testWindow,
			wantNames:      []string{},
			wantCountCalls: 1,
		},
		{
			name:         "the limit hint says how far --limit 0 reads",
			args:         "list --type Service --limit 2 -o json " + testWindow,
			wantNames:    []string{"api", "db"},
			wantStderr:   "raise --limit (--limit 0 follows up to 50 backend pages per type); for an exact count use 'gcx kg graph query' with count()",
			wantNoStderr: "for all",
		},
		{
			name:         "the limit hint with --page",
			args:         "list --type Service --page 0 --limit 2 -o json " + testWindow,
			wantNames:    []string{"api", "db"},
			wantStderr:   "--limit of 2 reached — results may be truncated; raise --limit; for an exact count",
			wantNoStderr: "follows up to",
		},
		{
			name:      "a found type makes no count call",
			args:      "list --type Service -o json " + testWindow,
			wantNames: []string{"api", "db", "gateway"},
		},
		{
			name:           "a type in the wrong case is corrected and the search re-run",
			args:           "list --type service -o json " + testWindow,
			wantNames:      []string{"api", "db", "gateway"},
			wantCountCalls: 1,
		},
		{
			name:    "a bare --insight before --type",
			args:    "list --insight --type Service " + testWindow,
			wantErr: "use --insight any",
		},
		{
			name:    "a bare --insight before the window",
			args:    "list --type Service --insight --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			wantErr: "use --insight any",
		},
		{
			name:    "an unknown type with a name suggests the command with the caller's flags",
			args:    "list --type Gadget --property name=api --namespace ns1 " + testWindow,
			wantErr: `entity type "Gadget" is not in the graph`,
			wantSuggestions: []string{
				"These return matching entities: gcx kg entities list --type Service --property class=gadget --property name=api --namespace ns1 --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			},
			wantCountCalls: 1,
		},
		{
			name:    "an unknown type in lower case suggests the value as stored",
			args:    "list --type widget --namespace ns2 " + testWindow,
			wantErr: `entity type "widget" is not in the graph`,
			wantSuggestions: []string{
				"These return matching entities: gcx kg entities list --type Service --property class=Widget --namespace ns2 --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			},
			wantCountCalls: 1,
		},
		{
			name:    "an unknown type with a name found under another type",
			args:    "list --type Gizmo --property name=node-1 --insight any " + testWindow,
			wantErr: `entity type "Gizmo" is not in the graph`,
			wantSuggestions: []string{
				"These return matching entities: gcx kg entities list --type Node --property name=node-1 --insight any --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			},
			wantCountCalls: 1,
		},
		{
			name:            "probes apply --insight like the caller's search",
			args:            "list --type Gizmo --property name=node-2 --insight any " + testWindow,
			wantErr:         `entity type "Gizmo" is not in the graph`,
			wantSuggestions: []string{},
			wantCountCalls:  1,
		},
		{
			name:           "an empty window the caller chose returns []",
			fake:           &fakeKG{counts: catalogOnly, schema: map[string][]string{}},
			args:           "list --type Service -o json " + testWindow,
			wantNames:      []string{},
			wantCountCalls: 1,
		},
		{
			name:           "an empty default window is an error",
			fake:           &fakeKG{counts: catalogOnly, schema: map[string][]string{}},
			args:           "list --type Service -o json",
			wantErr:        "no entities of any type in the window",
			wantCountCalls: 1,
		},
		{
			name:           "an unknown type in the default window points at the window first",
			fake:           &fakeKG{counts: map[string]int64{"Other": 1}, schema: map[string][]string{"Other": {"name"}}},
			args:           "list --type Service -o json",
			wantErr:        "(the default window: the last hour)",
			wantCountCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.fake
			if f == nil {
				f = entitiesFake(false)
			}
			stdout, stderr, err := runEntities(t, f, tt.args)
			assert.Contains(t, stderr, tt.wantStderr)
			if tt.wantNoStderr != "" {
				assert.NotContains(t, stderr, tt.wantNoStderr)
			}
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				var de gcxerrors.DetailedError
				if tt.wantSuggestions != nil {
					require.ErrorAs(t, err, &de)
					assert.Equal(t, append(tt.wantSuggestions, "Run 'gcx kg meta schema --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z' for each type's properties"), de.Suggestions)
				}
			} else {
				require.NoError(t, err)
				var got []SearchResult
				require.NoError(t, json.Unmarshal([]byte(stdout), &got), stdout)
				assert.Equal(t, tt.wantNames, names(got))
			}
			assert.Equal(t, tt.wantCountCalls, f.countCalls)
		})
	}
}

// TestEntitiesListCommandPageBound covers how many backend pages list reads
// when a client-side filter drops every row.
func TestEntitiesListCommandPageBound(t *testing.T) {
	rows := make([]SearchResult, 0, 60)
	for i := range 60 {
		rows = append(rows, scoped(fmt.Sprintf("s%02d", i), "prod", "other"))
	}
	readMore := "pass --limit N or --limit 0 to read up to 50 pages"
	tests := []struct {
		name         string
		limit        string
		wantPages    int
		wantStderr   string
		wantNoStderr string
	}{
		{name: "without --limit", wantPages: maxDefaultListPages, wantStderr: readMore},
		{name: "with --limit 50", limit: "--limit 50", wantPages: maxListPages, wantNoStderr: readMore},
		{name: "with --limit 0", limit: "--limit 0", wantPages: maxListPages, wantNoStderr: readMore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeKG{
				counts:   map[string]int64{"Service": 60},
				schema:   map[string][]string{"Service": {"name"}},
				entities: map[string][]SearchResult{"Service": rows},
				pageSize: 1,
			}
			// No row has an insight, so --insight any drops every row.
			stdout, stderr, err := runEntities(t, f, "list --type Service --insight any -o json "+tt.limit+" "+testWindow)
			require.NoError(t, err)
			assert.JSONEq(t, "[]", stdout)
			assert.Len(t, f.searchReq, tt.wantPages)
			assert.Contains(t, stderr, fmt.Sprintf("stopped after %d backend pages", tt.wantPages))
			assert.Contains(t, stderr, tt.wantStderr)
			if tt.wantNoStderr != "" {
				assert.NotContains(t, stderr, tt.wantNoStderr)
			}
		})
	}
}

// sharedNameFake serves two entities of one type that share a name, one per
// backend page.
func sharedNameFake() *fakeKG {
	a := SearchResult{Name: "dup", Type: "Service", Scope: map[string]string{"env": "prod", "namespace": "ns1"}}
	b := SearchResult{Name: "dup", Type: "Service", Scope: map[string]string{"env": "prod", "namespace": "ns2"}}
	return &fakeKG{
		counts:   map[string]int64{"Service": 2},
		schema:   map[string][]string{"Service": {"name", "namespace"}},
		entities: map[string][]SearchResult{"Service": {a, b}},
		pageSize: 1,
	}
}

func TestEntitiesInspectCommandTypeCheck(t *testing.T) {
	tests := []struct {
		name           string
		fake           *fakeKG
		args           string
		wantErr        string
		wantSuggestion string
		wantNoSuggest  string
		wantCountCalls int
	}{
		{name: "a found entity makes no count call", args: "inspect Service--api " + testWindow},
		{
			// The fake ignores the scope, so only the client-side filter on
			// the probe keeps an entity of another namespace out.
			name:           "a probe drops an entity outside the requested scope",
			args:           "inspect Gadget--api --namespace ns2 " + testWindow,
			wantErr:        `entity type "Gadget" is not in the graph`,
			wantNoSuggest:  "These return matching entities",
			wantCountCalls: 1,
		},
		{
			name:           "a probe keeps an entity inside the requested scope",
			args:           "inspect Gadget--api --namespace ns1 " + testWindow,
			wantErr:        `entity type "Gadget" is not in the graph`,
			wantSuggestion: "These return matching entities: gcx kg entities inspect Service--api --env prod --namespace ns1 --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			wantCountCalls: 1,
		},
		{name: "a type in the wrong case is corrected", args: "inspect service--api " + testWindow, wantCountCalls: 1},
		{
			name:           "an unknown type suggests the entity under its type, with its scope",
			args:           "inspect Gadget--api --env prod " + testWindow,
			wantErr:        `entity type "Gadget" is not in the graph`,
			wantSuggestion: "These return matching entities: gcx kg entities inspect Service--api --env prod --namespace ns1 --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			wantCountCalls: 1,
		},
		{name: "a known type keeps the not-found error", args: "inspect Service--nope " + testWindow, wantErr: "Service/nope not found", wantCountCalls: 1},
		{
			// The backend pages one row at a time, so the probe reads one
			// entity and a further page: the name can have more entities,
			// so the suggestion is list, not inspect.
			name:           "an unknown type whose probe page is not the last suggests list",
			fake:           sharedNameFake(),
			args:           "inspect Gadget--dup " + testWindow,
			wantErr:        `entity type "Gadget" is not in the graph`,
			wantSuggestion: "These return matching entities: gcx kg entities list --type Service --property name=dup --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			wantNoSuggest:  "inspect Service--dup",
			wantCountCalls: 1,
		},
		{
			name:           "an unknown type in the default window points at the window",
			fake:           &fakeKG{counts: map[string]int64{"Other": 1}, schema: map[string][]string{"Other": {"name"}}},
			args:           "inspect Service--api",
			wantErr:        "(the default window: the last hour)",
			wantSuggestion: "Pass --from/--to covering the time you are asking about",
			wantCountCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.fake
			if f == nil {
				f = entitiesFake(false)
			}
			_, _, err := runEntities(t, f, tt.args)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				var de gcxerrors.DetailedError
				if tt.wantSuggestion != "" || tt.wantNoSuggest != "" {
					require.ErrorAs(t, err, &de)
				}
				if tt.wantSuggestion != "" {
					assert.Contains(t, de.Suggestions, tt.wantSuggestion)
				}
				if tt.wantNoSuggest != "" {
					for _, sug := range de.Suggestions {
						assert.NotContains(t, sug, tt.wantNoSuggest)
					}
				}
			}
			assert.Equal(t, tt.wantCountCalls, f.countCalls)
		})
	}
}
