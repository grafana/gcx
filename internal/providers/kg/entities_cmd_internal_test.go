package kg

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
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
