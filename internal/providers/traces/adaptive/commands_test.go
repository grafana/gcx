package traces_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	traces "github.com/grafana/gcx/internal/providers/traces/adaptive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ReadPolicyFromFile (via stdin simulation)
// ---------------------------------------------------------------------------

func TestReadPolicyFromFile_JSON(t *testing.T) {
	input := `{"type":"probabilistic","name":"Test Policy","body":{"sampling_percentage":10}}`
	policy, err := traces.ReadPolicyFromReader(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, "probabilistic", policy.Type)
	assert.Equal(t, "Test Policy", policy.Name)
}

func TestReadPolicyFromFile_YAML(t *testing.T) {
	input := `type: probabilistic
name: Test Policy
body:
  sampling_percentage: 10
`
	policy, err := traces.ReadPolicyFromReader(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, "probabilistic", policy.Type)
	assert.Equal(t, "Test Policy", policy.Name)
}

func TestReadPolicyFromFile_Invalid(t *testing.T) {
	_, err := traces.ReadPolicyFromReader(strings.NewReader("<<<not valid>>>"))
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Integration-style: policies list via test server
// ---------------------------------------------------------------------------

func TestPoliciesList_Integration(t *testing.T) {
	policies := []traces.Policy{
		{ID: "p1", Name: "Sample 10%", Type: "probabilistic"},
		{ID: "p2", Name: "Rate limit", Type: "rate_limiting"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/adaptive-traces/api/v1/policies", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(policies)
	}))
	defer srv.Close()

	client := traces.NewClient(srv.URL, 42, "test-token", srv.Client())
	got, err := client.ListPolicies(t.Context())
	require.NoError(t, err)
	assert.Equal(t, policies, got)
}

// ---------------------------------------------------------------------------
// Integration-style: policies create via test server
// ---------------------------------------------------------------------------

func TestPoliciesCreate_Integration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/adaptive-traces/api/v1/policies", r.URL.Path)

		var p traces.Policy
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&p)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "probabilistic", p.Type)

		p.ID = "new-id"
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(p)
	}))
	defer srv.Close()

	client := traces.NewClient(srv.URL, 42, "test-token", srv.Client())
	policy := &traces.Policy{Type: "probabilistic", Name: "New Policy"}
	created, err := client.CreatePolicy(t.Context(), policy)
	require.NoError(t, err)
	assert.Equal(t, "new-id", created.ID)
	assert.Equal(t, "New Policy", created.Name)
}

// ---------------------------------------------------------------------------
// Integration-style: policies delete via test server
// ---------------------------------------------------------------------------

func TestPoliciesDelete_Integration(t *testing.T) {
	var deletedIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		id := strings.TrimPrefix(r.URL.Path, "/adaptive-traces/api/v1/policies/")
		deletedIDs = append(deletedIDs, id)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := traces.NewClient(srv.URL, 42, "test-token", srv.Client())

	require.NoError(t, client.DeletePolicy(t.Context(), "p1"))
	require.NoError(t, client.DeletePolicy(t.Context(), "p2"))
	assert.Equal(t, []string{"p1", "p2"}, deletedIDs)
}
