//nolint:testpackage // Command and adapter tests need the unexported constructors.
package k6

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// loadTestAPI fakes the routes a load test update uses for load test 9 in
// project 3, and records every write it receives.
type loadTestAPI struct {
	mu     sync.Mutex
	name   string
	writes []string
}

func (a *loadTestAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cloud/v6/load_tests/9", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		writeStatusJSON(w, http.StatusOK, map[string]any{"id": 9, "name": a.name, "project_id": 3})
	})
	mux.HandleFunc("PATCH /cloud/v6/load_tests/9", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.writes = append(a.writes, "PATCH")
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Name != "" {
			a.name = body.Name
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /cloud/v6/load_tests/9/script", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.writes = append(a.writes, "PUT script")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeStatusJSON(w, http.StatusNotFound, map[string]any{"error_msg": "404 Route Not Found"})
	})
	return mux
}

func (a *loadTestAPI) state() (string, []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.name, slices.Clone(a.writes)
}

func TestLoadTestUpdate_ProjectGuard(t *testing.T) {
	tests := []struct {
		name      string
		projectID int // 0 omits project_id from the input
		wantErr   bool
	}{
		{name: "different project", projectID: 4, wantErr: true},
		{name: "same project", projectID: 3},
		{name: "project omitted", projectID: 0},
	}
	for _, tt := range tests {
		check := func(t *testing.T, api *loadTestAPI, err error) {
			t.Helper()
			name, writes := api.state()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "project_id")
				assert.Empty(t, writes, "no write may be sent when the update is rejected")
				assert.Equal(t, "lt", name)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "renamed", name)
		}

		t.Run("command/"+tt.name, func(t *testing.T) {
			api := &loadTestAPI{name: "lt"}
			manifest := "name: renamed\n"
			if tt.projectID != 0 {
				manifest += "project_id: " + strconv.Itoa(tt.projectID) + "\n"
			}
			_, _, err := runK6Command(t, false, newTestLoader(t, api.handler()), newTestsUpdateCommand, []string{"9", "-f", "-"}, manifest)
			check(t, api, err)
		})

		t.Run("adapter/"+tt.name, func(t *testing.T) {
			api := &loadTestAPI{name: "lt"}
			crud := newLoadTestCRUD(newTestClient(t, "DirectClient", api.handler()), "stack-999", resources.Descriptor{
				GroupVersion: schema.GroupVersion{Group: APIGroup, Version: APIVersionStr},
				Kind:         "LoadTest", Singular: "loadtest", Plural: "loadtests",
			})
			res, err := LoadTestToResource(LoadTest{ID: 9, Name: "renamed", ProjectID: tt.projectID}, "stack-999")
			require.NoError(t, err)
			obj := res.ToUnstructured()
			_, err = crud.Update(t.Context(), &obj, metav1.UpdateOptions{})
			check(t, api, err)
		})
	}
}
