package synth_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// proxyPathFor is the datasource-proxy path the transport builds for a logical
// SM API path (e.g. "check/list").
func proxyPathFor(smPath string) string {
	return "/api/datasources/proxy/uid/" + testDSUID + "/sm/" + smPath
}

// fakeFallback is a direct-SM-API credential resolver for fallback tests. It
// records how many times it was invoked so tests can assert the fallback was
// (or was not) taken, and exactly once.
type fakeFallback struct {
	baseURL string
	token   string
	err     error
	calls   int
}

func (f *fakeFallback) LoadSMConfig(context.Context) (string, string, string, error) {
	f.calls++
	return f.baseURL, f.token, "", f.err
}

func TestTransport_ProxySuccessNoFallback(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, proxyPathFor("check/list"), r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":1}]`))
	}))
	defer proxySrv.Close()

	fallback := &fakeFallback{baseURL: "http://unused", token: "x"}
	tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, fallback)
	require.NoError(t, err)

	status, body, err := tr.Do(context.Background(), http.MethodGet, "check/list", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `[{"id":1}]`, string(body))
	assert.Equal(t, 0, fallback.calls, "a 2xx proxy response must not trigger the fallback")
}

func TestTransport_ForwardsPostBodyThroughProxy(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, proxyPathFor("check/add"), r.URL.Path)
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer proxySrv.Close()

	tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, nil)
	require.NoError(t, err)

	status, body, err := tr.Do(context.Background(), http.MethodPost, "check/add", []byte(`{"job":"x"}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"id":7}`, string(body))
	assert.Equal(t, "application/json", gotContentType)
	assert.JSONEq(t, `{"job":"x"}`, string(gotBody))
}

// TestTransport_FallsBackToDirectOn403 is the core dual-mode contract: a 403 from
// the proxy drops the request to the direct SM API, using the resolved token and
// the /api/v1 path.
func TestTransport_FallsBackToDirectOn403(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, proxyPathFor("check/list"), r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"plugin proxy route access denied"}`))
	}))
	defer proxySrv.Close()

	var directHit bool
	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHit = true
		assert.Equal(t, "/api/v1/check/list", r.URL.Path)
		assert.Equal(t, "Bearer direct-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":7}]`))
	}))
	defer directSrv.Close()

	fallback := &fakeFallback{baseURL: directSrv.URL, token: "direct-token"}
	tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, fallback)
	require.NoError(t, err)

	status, body, err := tr.Do(context.Background(), http.MethodGet, "check/list", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `[{"id":7}]`, string(body))
	assert.True(t, directHit, "direct SM API must be hit after a proxy 403")
	assert.Equal(t, 1, fallback.calls, "fallback credentials must be resolved exactly once")
}

// TestTransport_NoFallbackOnNon403 verifies that statuses other than 403 are
// returned verbatim and never trigger the fallback (404 must stay an SM
// not-found; 5xx must surface honestly rather than doubling requests).
func TestTransport_NoFallbackOnNon403(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"err":"upstream"}`))
			}))
			defer proxySrv.Close()

			fallback := &fakeFallback{baseURL: "http://unused", token: "x"}
			tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, fallback)
			require.NoError(t, err)

			gotStatus, body, err := tr.Do(context.Background(), http.MethodGet, "check/42", nil)
			require.NoError(t, err)
			assert.Equal(t, status, gotStatus)
			assert.JSONEq(t, `{"err":"upstream"}`, string(body))
			assert.Equal(t, 0, fallback.calls, "only a 403 may trigger the direct fallback")
		})
	}
}

// TestTransport_DirectOnlyWhenNoUID verifies that an empty datasource UID skips
// the proxy entirely and goes straight to the direct SM API.
func TestTransport_DirectOnlyWhenNoUID(t *testing.T) {
	var directHit bool
	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHit = true
		assert.Equal(t, "/api/v1/check/list", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":1}]`))
	}))
	defer directSrv.Close()

	fallback := &fakeFallback{baseURL: directSrv.URL, token: "direct-token"}
	tr, err := synth.NewTransport(config.NamespacedRESTConfig{}, "", fallback)
	require.NoError(t, err)

	status, _, err := tr.Do(context.Background(), http.MethodGet, "check/list", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.True(t, directHit)
	assert.Equal(t, 1, fallback.calls)
}

// TestTransport_Proxy403NoFallbackConfigured verifies that a proxy 403 with no
// fallback loader surfaces an error rather than silently succeeding.
func TestTransport_Proxy403NoFallbackConfigured(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxySrv.Close()

	tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, nil)
	require.NoError(t, err)

	_, _, err = tr.Do(context.Background(), http.MethodGet, "check/list", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no direct SM API fallback")
}

// TestTransport_BothPathsReturnUpstreamError verifies that when the proxy 403s
// and the direct SM API returns a non-2xx, the transport returns that status and
// body verbatim (error mapping is the typed client's job, not the transport's).
func TestTransport_BothPathsReturnUpstreamError(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxySrv.Close()

	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"direct boom"}`))
	}))
	defer directSrv.Close()

	fallback := &fakeFallback{baseURL: directSrv.URL, token: "direct-token"}
	tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, fallback)
	require.NoError(t, err)

	status, body, err := tr.Do(context.Background(), http.MethodGet, "check/list", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Contains(t, string(body), "direct boom")
}

// TestTransport_FallbackCredentialErrorSurfaces verifies that a failure to
// resolve direct credentials surfaces as an error from Do.
func TestTransport_FallbackCredentialErrorSurfaces(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxySrv.Close()

	fallback := &fakeFallback{err: errors.New("no SM token")}
	tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, fallback)
	require.NoError(t, err)

	_, _, err = tr.Do(context.Background(), http.MethodGet, "check/list", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving direct SM API credentials")
}

// TestTransport_FallbackResolvedOnceAcrossCalls verifies the sync.Once contract:
// repeated fallbacks resolve credentials a single time.
func TestTransport_FallbackResolvedOnceAcrossCalls(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxySrv.Close()

	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer directSrv.Close()

	fallback := &fakeFallback{baseURL: directSrv.URL, token: "direct-token"}
	tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, fallback)
	require.NoError(t, err)

	for range 3 {
		_, _, err := tr.Do(context.Background(), http.MethodGet, "check/list", nil)
		require.NoError(t, err)
	}
	assert.Equal(t, 1, fallback.calls, "credentials must be resolved exactly once across calls")
}

func TestTransport_UnsupportedMethod(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer proxySrv.Close()

	tr, err := synth.NewTransport(restCfg(proxySrv.URL), testDSUID, nil)
	require.NoError(t, err)

	_, _, err = tr.Do(context.Background(), http.MethodPut, "check/update", []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported method")
}

func restCfg(host string) config.NamespacedRESTConfig {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: host}}
}
