//nolint:testpackage // Uses the unexported command constructors and the package-internal mockLoader.
package k6

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// newLoadTestsLoader fakes the k6 load tests API with total load tests.
// Project 3 holds the first 30 of them. The projects API returns 60 projects. The handler applies $skip and $top
// and records each $top value that it receives.
func newLoadTestsLoader(t *testing.T, total int, tops *[]string) *mockLoader {
	t.Helper()
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v3/account/grafana-app/start", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"organization_id":"42","v3_grafana_token":"cached-v3"}`))
	})
	mux.HandleFunc("GET /cloud/v6/load_tests", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		*tops = append(*tops, q.Get("$top"))
		mu.Unlock()
		n := total
		if q.Get("project_id") == "3" {
			n = 30
		}
		skip, _ := strconv.Atoi(q.Get("$skip"))
		top, _ := strconv.Atoi(q.Get("$top"))
		var page []LoadTest
		for i := skip; i < n && i < skip+top; i++ {
			page = append(page, LoadTest{ID: i + 1, Name: fmt.Sprintf("lt-%d", i), ProjectID: 3})
		}
		_ = json.NewEncoder(w).Encode(loadTestsResponse{Value: page})
	})
	mux.HandleFunc("GET /cloud/v6/projects", func(w http.ResponseWriter, _ *http.Request) {
		projects := make([]Project, 0, 60)
		for i := range 60 {
			projects = append(projects, Project{ID: i + 1, Name: fmt.Sprintf("p-%d", i)})
		}
		_ = json.NewEncoder(w).Encode(projectsResponse{Value: projects})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mockLoader{
		cloudCfg:   providers.CloudRESTConfig{Stack: cloud.StackInfo{ID: 999}, Namespace: "stack-999"},
		grafanaCfg: config.NamespacedRESTConfig{Config: rest.Config{BearerToken: "glsa_test"}},
		providerCfg: map[string]string{
			"api-domain":     srv.URL,
			keyCachedToken:   "cached-v3",
			keyCachedOrgID:   "42",
			keyCachedStackID: "999",
		},
	}
}

func TestLoadTestsListTruncation(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantLen  int
		wantTops []string
		wantHint string
	}{
		{
			name:     "default limit fetches one spare item",
			args:     []string{"-o", "json"},
			wantLen:  50,
			wantTops: []string{"51"},
			wantHint: "hint: showing first 50; more results are available. See more with: gcx k6 load-tests list -o json --limit 100",
		},
		{
			name:     "limit above the total is complete",
			args:     []string{"-o", "json", "--limit", "200"},
			wantLen:  120,
			wantTops: []string{"100", "100"},
		},
		{
			name:     "limit 0 drains all pages",
			args:     []string{"-o", "json", "--limit", "0"},
			wantLen:  120,
			wantTops: []string{"100", "100"},
		},
		{
			name:     "project filter reports the observed total",
			args:     []string{"-o", "json", "--project-id", "3", "--limit", "10"},
			wantLen:  10,
			wantTops: []string{"100"},
			wantHint: "hint: showing first 10 of 30. See all results with: gcx k6 load-tests list -o json --project-id 3 --limit 0",
		},
		{
			name:     "project filter complete",
			args:     []string{"-o", "json", "--project-id", "3"},
			wantLen:  30,
			wantTops: []string{"100"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.PinArgv(t, append([]string{"gcx", "k6", "load-tests", "list"}, tc.args...)...)
			var tops []string
			loader := newLoadTestsLoader(t, 120, &tops)

			stdout, stderr, err := runK6Command(t, false, loader, newTestsListCommand, tc.args, "")
			require.NoError(t, err)

			var got []LoadTest
			require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout must stay a bare JSON array")
			assert.Len(t, got, tc.wantLen)
			assert.Equal(t, tc.wantTops, tops)
			if tc.wantHint == "" {
				assert.NotContains(t, stderr, "showing first")
			} else {
				assert.Contains(t, stderr, tc.wantHint)
			}
		})
	}
}

func TestProjectsListTruncation(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantLen  int
		wantHint string
	}{
		{"default limit", []string{"-o", "json"}, 50, "hint: showing first 50 of 60. See all results with: gcx k6 projects list -o json --limit 0"},
		{"no limit", []string{"-o", "json", "--limit", "0"}, 60, ""},
		{"table truncated", []string{"--limit", "5"}, -1, "hint: showing first 5 of 60. See all results with: gcx k6 projects list --limit 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.PinArgv(t, append([]string{"gcx", "k6", "projects", "list"}, tc.args...)...)
			var tops []string
			loader := newLoadTestsLoader(t, 0, &tops)

			stdout, stderr, err := runK6Command(t, false, loader, newProjectsListCommand, tc.args, "")
			require.NoError(t, err)

			if tc.wantLen >= 0 {
				var got []map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout must stay a bare JSON array")
				assert.Len(t, got, tc.wantLen)
			}
			if tc.wantHint == "" {
				assert.NotContains(t, stderr, "showing first")
			} else {
				assert.Contains(t, stderr, tc.wantHint)
			}
		})
	}
}
