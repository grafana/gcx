//nolint:testpackage // White-box test: needs access to unexported etagManager.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// MetricRule identity
// ---------------------------------------------------------------------------

func TestMetricRuleIdentity(t *testing.T) {
	r := MetricRule{Metric: "original"}
	assert.Equal(t, "original", r.GetResourceName())

	r.SetResourceName("renamed")
	assert.Equal(t, "renamed", r.Metric)
	assert.Equal(t, "renamed", r.GetResourceName())
}

// ---------------------------------------------------------------------------
// ValidateFn wiring via DryRun
// ---------------------------------------------------------------------------

func TestDryRunCallsCheckRulesInsteadOfMutation(t *testing.T) {
	var (
		checkRulesCalled atomic.Int32
		createCalled     atomic.Int32
		updateCalled     atomic.Int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/aggregations/rules":
			writeJSONEtag(w, []MetricRule{}, `"etag-v1"`)

		case r.Method == http.MethodGet && r.URL.Path == "/aggregations/rule/my-metric":
			writeJSONEtag(w, MetricRule{Metric: "my-metric", Aggregations: []string{"sum"}}, "")

		case r.Method == http.MethodPost && r.URL.Path == "/aggregations/check-rules":
			checkRulesCalled.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode([]string{}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

		case r.Method == http.MethodPost:
			createCalled.Add(1)
			w.Header().Set("Etag", `"etag-v2"`)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(MetricRule{Metric: "my-metric"}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

		case r.Method == http.MethodPut:
			updateCalled.Add(1)
			w.Header().Set("Etag", `"etag-v2"`)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(MetricRule{Metric: "my-metric"}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	client := newTestEtagClient(t, server)
	em := &etagManager{client: client, segment: ""}

	crud := &adapter.TypedCRUD[MetricRule]{
		ListFn:   em.list,
		GetFn:    em.get,
		CreateFn: em.create,
		UpdateFn: em.update,
		DeleteFn: em.delete,
		ValidateFn: func(ctx context.Context, items []*MetricRule) error {
			rules := make([]MetricRule, len(items))
			for i, r := range items {
				rules[i] = *r
			}
			errs, err := client.ValidateRules(ctx, rules, "")
			if err != nil {
				return err
			}
			if len(errs) > 0 {
				return fmt.Errorf("validation: %s", strings.Join(errs, "; "))
			}
			return nil
		},
		Namespace:  "default",
		Descriptor: ruleDescriptorVar,
	}

	a := crud.AsAdapter()
	ctx := context.Background()
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": RuleAPIVersion,
		"kind":       RuleKind,
		"metadata":   map[string]any{"name": "my-metric", "namespace": "default"},
		"spec": map[string]any{
			"metric":       "my-metric",
			"aggregations": []any{"sum"},
		},
	}}

	_, err := a.Create(ctx, obj, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	require.NoError(t, err)

	_, err = a.Update(ctx, obj, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
	require.NoError(t, err)

	assert.Equal(t, int32(2), checkRulesCalled.Load(), "check-rules must be called for each dry-run operation")
	assert.Equal(t, int32(0), createCalled.Load(), "real create must not be called during dry run")
	assert.Equal(t, int32(0), updateCalled.Load(), "real update must not be called during dry run")
}

// ---------------------------------------------------------------------------
// etagManager helpers
// ---------------------------------------------------------------------------

func newTestEtagClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	return NewClient(server.URL, 1, "token", nil)
}

func writeJSONEtag(w http.ResponseWriter, v any, etag string) {
	if etag != "" {
		w.Header().Set("Etag", etag)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// Lazy ETag fetch
// ---------------------------------------------------------------------------

func TestEtagManagerLazyFetch(t *testing.T) {
	var listCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			assert.Equal(t, "/aggregations/rules", r.URL.Path)
			listCalls.Add(1)
			writeJSONEtag(w, []MetricRule{}, `"etag-v1"`)

		case http.MethodPost:
			// CreateRule: verify If-Match was set (meaning ETag was fetched first).
			assert.Equal(t, `"etag-v1"`, r.Header.Get("If-Match"))
			w.Header().Set("Etag", `"etag-v2"`)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(MetricRule{Metric: "my-metric"}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

		default:
			http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	em := &etagManager{
		client:  newTestEtagClient(t, server),
		segment: "",
	}

	ctx := context.Background()
	rule := &MetricRule{Metric: "my-metric"}
	_, err := em.create(ctx, rule)
	require.NoError(t, err)

	// List must have been called exactly once to fetch the ETag.
	assert.Equal(t, int32(1), listCalls.Load())
	// ETag should now be updated to the one returned by create.
	assert.Equal(t, `"etag-v2"`, em.etag)
}

// ---------------------------------------------------------------------------
// Retry on 412
// ---------------------------------------------------------------------------

func TestEtagManagerRetryOn412(t *testing.T) {
	var createCalls atomic.Int32
	var listCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listCalls.Add(1)
			writeJSONEtag(w, []MetricRule{}, `"etag-v2"`)

		case http.MethodPost:
			n := createCalls.Add(1)
			if n == 1 {
				// First attempt — return 412 to trigger retry.
				w.WriteHeader(http.StatusPreconditionFailed)
				_, _ = w.Write([]byte("precondition failed"))
				return
			}
			// Second attempt — succeed with new ETag.
			assert.Equal(t, `"etag-v2"`, r.Header.Get("If-Match"))
			w.Header().Set("Etag", `"etag-v3"`)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(MetricRule{Metric: "my-metric"}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	em := &etagManager{
		client:  newTestEtagClient(t, server),
		segment: "",
	}
	em.etag = `"etag-v1"` // Pre-seed a stale ETag to avoid initial list call.

	ctx := context.Background()
	rule := &MetricRule{Metric: "my-metric"}
	_, err := em.create(ctx, rule)
	require.NoError(t, err)

	assert.Equal(t, int32(2), createCalls.Load())
	assert.Equal(t, int32(1), listCalls.Load())
	assert.Equal(t, `"etag-v3"`, em.etag)
}

// ---------------------------------------------------------------------------
// Delete invalidates ETag
// ---------------------------------------------------------------------------

func TestEtagManagerDeleteInvalidatesEtag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	em := &etagManager{
		client:  newTestEtagClient(t, server),
		segment: "",
	}
	em.etag = `"etag-v1"`

	ctx := context.Background()
	err := em.delete(ctx, "my-metric")
	require.NoError(t, err)

	// Delete returns no ETag — the manager should have invalidated the cached ETag.
	assert.Empty(t, em.etag)
}
