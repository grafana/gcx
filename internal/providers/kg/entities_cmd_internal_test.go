package kg

import (
	"bytes"
	"encoding/json"
	"errors"
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

func entitiesFake() *fakeKG {
	api := svc("api", "deployment")
	api.Scope = map[string]string{"env": "prod", "namespace": "ns1"}
	db := svc("db", "statefulset")
	db.Scope = map[string]string{"env": "prod", "namespace": "ns2"}
	// gateway is a Service without a namespace.
	gateway := svc("gateway", "deployment")
	gateway.Scope = map[string]string{"env": "prod"}
	return &fakeKG{
		counts: map[string]int64{"Service": 3, "Node": 2},
		schema: map[string][]string{
			"Service": {"name", "namespace", "workload_type"},
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
			name:    "an unknown type with a name suggests the command with the caller's flags",
			args:    "list --type Deployment --property name=api --namespace ns1 " + testWindow,
			wantErr: `entity type "Deployment" is not in the graph`,
			wantSuggestions: []string{
				"These return matching entities: gcx kg entities list --type Service --property workload_type=deployment --property name=api --namespace ns1 --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			},
			wantCountCalls: 1,
		},
		{
			name:    "an unknown type with a name found under another type",
			args:    "list --type Container --property name=node-1 --insight any " + testWindow,
			wantErr: `entity type "Container" is not in the graph`,
			wantSuggestions: []string{
				"These return matching entities: gcx kg entities list --type Node --property name=node-1 --insight any --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			},
			wantCountCalls: 1,
		},
		{
			name:            "probes apply --insight like the caller's search",
			args:            "list --type Container --property name=node-2 --insight any " + testWindow,
			wantErr:         `entity type "Container" is not in the graph`,
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
			fake:           &fakeKG{counts: map[string]int64{"K6Test": 1}, schema: map[string][]string{"K6Test": {"name"}}},
			args:           "list --type Service -o json",
			wantErr:        "(the default window: the last hour)",
			wantCountCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.fake
			if f == nil {
				f = entitiesFake()
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
					require.True(t, errors.As(err, &de), "want DetailedError, got %v", err)
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
	var rows []SearchResult
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

func TestEntitiesInspectCommandTypeCheck(t *testing.T) {
	tests := []struct {
		name           string
		fake           *fakeKG
		args           string
		wantErr        string
		wantSuggestion string
		wantCountCalls int
	}{
		{name: "a found entity makes no count call", args: "inspect Service--api " + testWindow},
		{name: "a type in the wrong case is corrected", args: "inspect service--api " + testWindow, wantCountCalls: 1},
		{
			name:           "an unknown type suggests the entity under its type, with its scope",
			args:           "inspect Deployment--api --env prod " + testWindow,
			wantErr:        `entity type "Deployment" is not in the graph`,
			wantSuggestion: "These return matching entities: gcx kg entities inspect Service--api --env prod --namespace ns1 --from 2026-01-01T00:00:00Z --to 2026-01-01T01:00:00Z",
			wantCountCalls: 1,
		},
		{name: "a known type keeps the not-found error", args: "inspect Service--nope " + testWindow, wantErr: "Service/nope not found", wantCountCalls: 1},
		{
			name:           "an unknown type in the default window points at the window",
			fake:           &fakeKG{counts: map[string]int64{"K6Test": 1}, schema: map[string][]string{"K6Test": {"name"}}},
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
				f = entitiesFake()
			}
			_, _, err := runEntities(t, f, tt.args)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				if tt.wantSuggestion != "" {
					var de gcxerrors.DetailedError
					require.True(t, errors.As(err, &de), "want DetailedError, got %v", err)
					assert.Contains(t, de.Suggestions, tt.wantSuggestion)
				}
			}
			assert.Equal(t, tt.wantCountCalls, f.countCalls)
		})
	}
}
