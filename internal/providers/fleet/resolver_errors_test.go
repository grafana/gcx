package fleet //nolint:testpackage // Exercises unexported ID/name resolution contracts.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fleetbase "github.com/grafana/gcx/internal/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fleetResolver struct {
	name    string
	resolve func(context.Context, *Client, string) error
}

func errorResolvers() []fleetResolver {
	return []fleetResolver{
		{"pipeline", func(ctx context.Context, client *Client, ref string) error {
			_, err := resolvePipeline(ctx, client, ref)
			return err
		}},
		{"collector", func(ctx context.Context, client *Client, ref string) error {
			_, err := resolveCollector(ctx, client, ref)
			return err
		}},
	}
}

func TestResolversPreserveGetFailures(t *testing.T) {
	for _, resolver := range errorResolvers() {
		for _, tc := range []struct {
			name   string
			status int
			body   string
		}{
			{"unauthorized", 401, `{"message":"expired"}`},
			{"forbidden", 403, `{"message":"denied"}`},
			{"unavailable", 500, `{"message":"unavailable"}`},
			{"unknown RPC", 404, "404 page not found"},
			{"plugin missing", 404, `{"message":"Plugin not found"}`},
			{"empty 404", 404, ""},
		} {
			t.Run(resolver.name+"/"+tc.name, func(t *testing.T) {
				var requests []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.URL.Path)
					if strings.Contains(r.URL.Path, "/List") {
						t.Error("resolver attempted list fallback after a failed Get")
						writeContractJSON(t, w, map[string]any{})
						return
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				}))
				t.Cleanup(server.Close)
				err := resolver.resolve(context.Background(), NewClient(context.Background(), server.URL, server.Client()), "missing-123")
				var httpErr *fleetbase.HTTPError
				require.ErrorAs(t, err, &httpErr)
				assert.Equal(t, tc.status, httpErr.Status)
				assert.Equal(t, tc.body, httpErr.Body)
				require.Len(t, requests, 1, "no alternative ID or list request may replace this failure")
			})
		}
	}
}

type failingResolverTransport struct {
	err   error
	calls int
}

func (tr *failingResolverTransport) RoundTrip(*http.Request) (*http.Response, error) {
	tr.calls++
	return nil, tr.err
}

func TestResolversPreserveNetworkFailure(t *testing.T) {
	for _, resolver := range errorResolvers() {
		t.Run(resolver.name, func(t *testing.T) {
			cause := errors.New("network unavailable")
			transport := &failingResolverTransport{err: cause}
			client := NewClient(context.Background(), "https://example.invalid", &http.Client{Transport: transport})
			err := resolver.resolve(context.Background(), client, "missing-123")
			require.ErrorIs(t, err, cause)
			assert.Equal(t, 1, transport.calls)
		})
	}
}

func TestResolversRetainMissingGetAfterEmptyList(t *testing.T) {
	for _, resolver := range errorResolvers() {
		t.Run(resolver.name, func(t *testing.T) {
			const body = `{"code":"not_found","message":"resource absent"}`
			var listed bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/List") {
					listed = true
					writeContractJSON(t, w, map[string]any{resolver.name + "s": []any{}})
					return
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)
			err := resolver.resolve(context.Background(), NewClient(context.Background(), server.URL, server.Client()), "missing-123")
			var httpErr *fleetbase.HTTPError
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, http.StatusNotFound, httpErr.Status)
			assert.JSONEq(t, body, httpErr.Body)
			assert.Contains(t, err.Error(), "missing-123")
			assert.True(t, listed)
		})
	}
}

func TestResolvePipelineNameFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathGetPipeline:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"not_found","message":"pipeline absent"}`))
		case pathListPipelines:
			writeContractJSON(t, w, map[string]any{"pipelines": []map[string]string{{"id": "202", "name": "pipeline-123"}, {"id": "303", "name": "plain-name"}}})
		default:
			t.Errorf("unexpected route %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(context.Background(), server.URL, server.Client())
	for _, tc := range []struct {
		ref string
		id  string
	}{{"pipeline-123", "202"}, {"plain-name", "303"}} {
		t.Run(tc.ref, func(t *testing.T) {
			pipeline, err := resolvePipeline(context.Background(), client, tc.ref)
			require.NoError(t, err)
			assert.Equal(t, tc.id, pipeline.ID)
		})
	}
}
