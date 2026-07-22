package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRoundTripFunc func(*http.Request) (*http.Response, error)

func (f testRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRefreshTransport_ExpiredAccessWithoutRefreshFailsClosed(t *testing.T) {
	var refreshCalls, protectedCalls atomic.Int32
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
			} else {
				protectedCalls.Add(1)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_expired",
		ExpiresAt:     time.Now().Add(-time.Minute),
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	assert.Nil(t, resp)
	require.ErrorIs(t, err, auth.ErrRefreshTokenMissing)
	assert.Zero(t, refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load())
}

func TestRefreshTransport_AccessTokenWithUnknownExpiryIsUsable(t *testing.T) {
	var refreshCalls, protectedCalls atomic.Int32
	var authorization string
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
			} else {
				protectedCalls.Add(1)
				authorization = req.Header.Get("Authorization")
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_unknown_expiry",
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	assert.Zero(t, refreshCalls.Load())
	assert.Equal(t, int32(1), protectedCalls.Load())
	assert.Equal(t, "Bearer gat_unknown_expiry", authorization)
}

func TestRefreshTransport_LockAndReloadFailuresMakeNoHTTPCalls(t *testing.T) {
	tests := []struct {
		name   string
		lock   auth.TokenLocker
		reload auth.TokenReloader
		want   string
	}{
		{
			name: "lock failure",
			lock: func(context.Context) (func(), error) {
				return nil, errors.New("injected lock failure")
			},
			want: "injected lock failure",
		},
		{
			name: "reload failure",
			lock: func(context.Context) (func(), error) {
				return func() {}, nil
			},
			reload: func() (auth.StoredTokens, bool, error) {
				return auth.StoredTokens{}, false, errors.New("injected reload failure")
			},
			want: "injected reload failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var refreshCalls, protectedCalls atomic.Int32
			transport := &auth.RefreshTransport{
				Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Path == "/api/cli/v1/auth/refresh" {
						refreshCalls.Add(1)
					} else {
						protectedCalls.Add(1)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
				}),
				ProxyEndpoint: "https://proxy.invalid",
				Token:         "gat_old",
				RefreshToken:  "gar_old",
				ExpiresAt:     time.Now().Add(time.Minute),
				Lock:          tt.lock,
				Reload:        tt.reload,
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			if resp != nil {
				require.NoError(t, resp.Body.Close())
			}
			assert.Nil(t, resp)
			require.ErrorContains(t, err, tt.want)
			assert.Zero(t, refreshCalls.Load())
			assert.Zero(t, protectedCalls.Load())
		})
	}
}

func TestRefreshTransport_CancellationBeforeRotationMakesNoCalls(t *testing.T) {
	var lockCalls, reloadCalls, refreshCalls, protectedCalls atomic.Int32
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
			} else {
				protectedCalls.Add(1)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		Lock: func(context.Context) (func(), error) {
			lockCalls.Add(1)
			return func() {}, nil
		},
		Reload: func() (auth.StoredTokens, bool, error) {
			reloadCalls.Add(1)
			return auth.StoredTokens{}, false, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://proxy.invalid/protected", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	assert.Nil(t, resp)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, lockCalls.Load())
	assert.Zero(t, reloadCalls.Load())
	assert.Zero(t, refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load())
}

func TestRefreshTransport_ReloadedGenerationWithoutAccessTokenRefreshesBeforeUse(t *testing.T) {
	var refreshCalls, protectedCalls atomic.Int32
	var presentedRefresh, authorization atomic.Value
	presentedRefresh.Store("")
	authorization.Store("")
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
				var body struct {
					RefreshToken string `json:"refresh_token"`
				}
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				presentedRefresh.Store(body.RefreshToken)
				response := `{"data":{"token":"gat_final","expires_at":"2099-01-01T00:00:00Z","refresh_token":"gar_final","refresh_expires_at":"2099-02-01T00:00:00Z"}}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header), Request: req}, nil
			}
			protectedCalls.Add(1)
			authorization.Store(req.Header.Get("Authorization"))
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		Lock: func(context.Context) (func(), error) {
			return func() {}, nil
		},
		Reload: func() (auth.StoredTokens, bool, error) {
			return auth.StoredTokens{
				Token:            "",
				RefreshToken:     "gar_reloaded",
				ExpiresAt:        time.Now().Add(time.Hour),
				RefreshExpiresAt: time.Now().Add(24 * time.Hour),
			}, true, nil
		},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "gar_reloaded", presentedRefresh.Load())
	assert.Equal(t, "Bearer gat_final", authorization.Load())
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(1), protectedCalls.Load())
}

func TestRefreshTransport_CancellationWhileWaitingForLockStopsBeforeRotation(t *testing.T) {
	lockEntered := make(chan struct{})
	var refreshCalls, protectedCalls atomic.Int32
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
			} else {
				protectedCalls.Add(1)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		Lock: func(ctx context.Context) (func(), error) {
			close(lockEntered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://proxy.invalid/protected", nil)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()
	<-lockEntered
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("refresh did not stop after caller cancellation while waiting for the lock")
	}
	assert.Zero(t, refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load())
}

func TestRefreshTransport_CancellationDuringReloadStopsBeforeRotation(t *testing.T) {
	reloadEntered := make(chan struct{})
	allowReload := make(chan struct{})
	var refreshCalls, protectedCalls atomic.Int32
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
			} else {
				protectedCalls.Add(1)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		Lock: func(ctx context.Context) (func(), error) {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("persistence lock inherited caller cancellation: %w", err)
			}
			return func() {}, nil
		},
		Reload: func() (auth.StoredTokens, bool, error) {
			close(reloadEntered)
			<-allowReload
			return auth.StoredTokens{
				Token:        "gat_old",
				RefreshToken: "gar_old",
				ExpiresAt:    time.Now().Add(time.Minute),
			}, true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://proxy.invalid/protected", nil)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()
	<-reloadEntered
	cancel()
	close(allowReload)

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("refresh did not stop after caller cancellation during reload")
	}
	assert.Zero(t, refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load())
}

func TestRefreshTransport_ConcurrentWaitersReceiveLeaderFailure(t *testing.T) {
	lockEntered := make(chan struct{})
	allowFailure := make(chan struct{})
	var lockCalls atomic.Int32
	transport := &auth.RefreshTransport{
		Token:        "gat_old",
		RefreshToken: "gar_old",
		ExpiresAt:    time.Now().Add(time.Minute),
		Lock: func(context.Context) (func(), error) {
			if lockCalls.Add(1) == 1 {
				close(lockEntered)
			}
			<-allowFailure
			return nil, errors.New("injected lock failure")
		},
	}

	const callers = 8
	start := make(chan struct{})
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = transport.FreshToken(context.Background())
		}(i)
	}
	close(start)
	<-lockEntered
	time.Sleep(20 * time.Millisecond)
	close(allowFailure)
	wg.Wait()
	assert.Equal(t, int32(1), lockCalls.Load())
	for _, err := range errs {
		require.ErrorContains(t, err, "injected lock failure")
	}
}

func TestRefreshTransport_RetriesPendingPersistenceWithoutSecondRefresh(t *testing.T) {
	var refreshCalls atomic.Int32
	base := testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/cli/v1/auth/refresh", req.URL.Path)
		refreshCalls.Add(1)
		body := `{"data":{"token":"gat_new","expires_at":"2099-01-01T00:00:00Z","refresh_token":"gar_new","refresh_expires_at":"2099-02-01T00:00:00Z"}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	var persistCalls atomic.Int32
	transport := &auth.RefreshTransport{
		Base:          base,
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		Lock: func(context.Context) (func(), error) {
			return func() {}, nil
		},
		OnRefresh: func(previousRefreshToken, token, refreshToken, _, _ string) error {
			require.Equal(t, "gar_old", previousRefreshToken)
			require.Equal(t, "gat_new", token)
			require.Equal(t, "gar_new", refreshToken)
			if persistCalls.Add(1) == 1 {
				return errors.New("injected persistence failure")
			}
			return nil
		},
	}

	_, err := transport.FreshToken(context.Background())
	require.ErrorContains(t, err, "injected persistence failure")
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	token, err := transport.FreshToken(cancelledCtx)
	require.NoError(t, err)
	assert.Equal(t, "gat_new", token)
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(2), persistCalls.Load())
}

func TestRefreshTransport_ConcurrentPersistenceFailureBlocksProtectedAPI(t *testing.T) {
	var refreshCalls, protectedCalls, persistCalls atomic.Int32
	base := testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/cli/v1/auth/refresh" {
			refreshCalls.Add(1)
			body := `{"data":{"token":"gat_new","expires_at":"2099-01-01T00:00:00Z","refresh_token":"gar_new","refresh_expires_at":"2099-02-01T00:00:00Z"}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
		}
		protectedCalls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
	})
	persistEntered := make(chan struct{})
	allowFailure := make(chan struct{})
	transport := &auth.RefreshTransport{
		Base:          base,
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		Lock: func(context.Context) (func(), error) {
			return func() {}, nil
		},
		OnRefresh: func(previousRefreshToken, _, _, _, _ string) error {
			if previousRefreshToken != "gar_old" {
				return fmt.Errorf("unexpected previous generation %q", previousRefreshToken)
			}
			if persistCalls.Add(1) == 1 {
				close(persistEntered)
				<-allowFailure
				return errors.New("injected persistence failure")
			}
			return nil
		},
	}

	const callers = 12
	start := make(chan struct{})
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
			if err != nil {
				errs[i] = err
				return
			}
			resp, err := transport.RoundTrip(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			errs[i] = err
		}(i)
	}
	close(start)
	<-persistEntered
	time.Sleep(20 * time.Millisecond)
	close(allowFailure)
	wg.Wait()

	for _, err := range errs {
		require.ErrorContains(t, err, "injected persistence failure")
	}
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(1), persistCalls.Load())
	assert.Zero(t, protectedCalls.Load())

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, int32(1), refreshCalls.Load(), "persistence retry must not rotate again")
	assert.Equal(t, int32(2), persistCalls.Load())
	assert.Equal(t, int32(1), protectedCalls.Load())
}

func TestRefreshTransport_SetsAuthorizationHeader(t *testing.T) {
	var gotHeader string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: backend.URL,
		Token:         "gat_test-token",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, backend.URL+"/api/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if gotHeader != "Bearer gat_test-token" {
		t.Fatalf("expected Authorization header %q, got %q", "Bearer gat_test-token", gotHeader)
	}
}

func TestRefreshTransport_PreservesExistingAuthorizationHeader(t *testing.T) {
	var gotHeader string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: backend.URL,
		Token:         "gat_test-token",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, backend.URL+"/api/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req.SetBasicAuth("123", "cloud-api-token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if !strings.HasPrefix(gotHeader, "Basic ") {
		t.Fatalf("expected Authorization header to start with %q, got %q", "Basic ", gotHeader)
	}
}

func TestRefreshTransport_SkipsRefreshWhenAuthorizationPreset(t *testing.T) {
	var refreshCalls atomic.Int32
	var gotHeader string
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			refreshCalls.Add(1)
		}
		gotHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(1 * time.Minute), // within refresh threshold
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req.SetBasicAuth("123", "cloud-api-token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if refreshCalls.Load() != 0 {
		t.Fatalf("expected no refresh calls, got %d", refreshCalls.Load())
	}
	if !strings.HasPrefix(gotHeader, "Basic ") {
		t.Fatalf("expected Authorization header to start with %q, got %q", "Basic ", gotHeader)
	}
}

func TestRefreshTransport_SkipsRefreshWhenTokenFresh(t *testing.T) {
	var refreshCalls atomic.Int32
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			refreshCalls.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_fresh",
		RefreshToken:  "gar_refresh",
		ExpiresAt:     time.Now().Add(1 * time.Hour), // well above threshold
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if refreshCalls.Load() != 0 {
		t.Fatalf("expected no refresh calls, got %d", refreshCalls.Load())
	}
}

func TestRefreshTransport_RefreshesWhenTokenExpiring(t *testing.T) {
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_refreshed",
					"expires_at":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
					"refresh_token":      "gar_new-refresh",
					"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(1 * time.Minute), // within refresh threshold
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if transport.Token != "gat_refreshed" {
		t.Fatalf("expected token to be refreshed to %q, got %q", "gat_refreshed", transport.Token)
	}
}

func TestRefreshTransport_CallsOnRefreshCallback(t *testing.T) {
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_new",
					"expires_at":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
					"refresh_token":      "gar_new",
					"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	var callbackCalled atomic.Bool
	var savedToken, savedRefresh string

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_expiring",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(1 * time.Minute), // within threshold
		OnRefresh: func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
			if previousRefreshToken != "gar_old" {
				t.Fatalf("expected previous refresh token %q, got %q", "gar_old", previousRefreshToken)
			}
			callbackCalled.Store(true)
			savedToken = token
			savedRefresh = refreshToken
			return nil
		},
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if !callbackCalled.Load() {
		t.Fatal("expected OnRefresh callback to be called")
	}
	if savedToken != "gat_new" {
		t.Fatalf("expected saved token %q, got %q", "gat_new", savedToken)
	}
	if savedRefresh != "gar_new" {
		t.Fatalf("expected saved refresh token %q, got %q", "gar_new", savedRefresh)
	}
}

func TestRefreshTransport_BlankExpiriesPersistUsableGenerationAcrossTransports(t *testing.T) {
	tests := []struct {
		name             string
		expiresAt        string
		refreshExpiresAt string
	}{
		{name: "both blank"},
		{name: "both whitespace", expiresAt: "  \t", refreshExpiresAt: " \t "},
		{name: "access expiry blank", refreshExpiresAt: "2099-02-01T00:00:00Z"},
		{name: "refresh expiry blank", expiresAt: "2099-01-01T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var refreshCalls, protectedCalls, persistCalls, secondLockCalls atomic.Int32
			var persistedToken, persistedRefresh, persistedExpires, persistedRefreshExpires string
			var protectedAuthorizations []string
			base := testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/cli/v1/auth/refresh" {
					call := refreshCalls.Add(1)
					body := fmt.Sprintf(
						`{"data":{"token":"gat_new_%d","expires_at":%q,"refresh_token":"gar_new_%d","refresh_expires_at":%q}}`,
						call,
						tt.expiresAt,
						call,
						tt.refreshExpiresAt,
					)
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
				}
				protectedCalls.Add(1)
				protectedAuthorizations = append(protectedAuthorizations, req.Header.Get("Authorization"))
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
			})
			persist := func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
				persistCalls.Add(1)
				assert.Equal(t, "gar_old", previousRefreshToken)
				persistedToken = token
				persistedRefresh = refreshToken
				persistedExpires = expiresAt
				persistedRefreshExpires = refreshExpiresAt
				return nil
			}
			first := &auth.RefreshTransport{
				Base:          base,
				ProxyEndpoint: "https://proxy.invalid",
				Token:         "gat_old",
				RefreshToken:  "gar_old",
				ExpiresAt:     time.Now().Add(time.Minute),
				OnRefresh:     persist,
			}
			request := func(transport *auth.RefreshTransport) error {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
				require.NoError(t, err)
				resp, err := transport.RoundTrip(req)
				if resp != nil {
					require.NoError(t, resp.Body.Close())
				}
				return err
			}

			require.NoError(t, request(first))
			assert.Equal(t, "gat_new_1", persistedToken)
			assert.Equal(t, "gar_new_1", persistedRefresh)
			if strings.TrimSpace(tt.expiresAt) == "" {
				assert.Empty(t, persistedExpires)
			} else {
				assert.Equal(t, tt.expiresAt, persistedExpires)
			}
			if strings.TrimSpace(tt.refreshExpiresAt) == "" {
				assert.Empty(t, persistedRefreshExpires)
			} else {
				assert.Equal(t, tt.refreshExpiresAt, persistedRefreshExpires)
			}
			assert.Equal(t, int32(1), refreshCalls.Load())
			assert.Equal(t, int32(1), persistCalls.Load())
			assert.Equal(t, []string{"Bearer gat_new_1"}, protectedAuthorizations)

			parsePersisted := func(value string) time.Time {
				if value == "" {
					return time.Time{}
				}
				parsed, err := time.Parse(time.RFC3339, value)
				require.NoError(t, err)
				return parsed
			}
			// Reconstruct the transport exactly as a fresh gcx process does from
			// the persisted strings. Unknown expiry must not immediately consume
			// the newly rotated refresh generation again.
			second := &auth.RefreshTransport{
				Base:             base,
				ProxyEndpoint:    "https://proxy.invalid",
				Token:            persistedToken,
				RefreshToken:     persistedRefresh,
				ExpiresAt:        parsePersisted(persistedExpires),
				RefreshExpiresAt: parsePersisted(persistedRefreshExpires),
				Lock: func(context.Context) (func(), error) {
					secondLockCalls.Add(1)
					return func() {}, nil
				},
				OnRefresh: func(_, _, _, _, _ string) error {
					t.Fatal("fresh transport unexpectedly refreshed the persisted generation")
					return nil
				},
			}
			require.NoError(t, request(second))
			assert.Equal(t, int32(1), refreshCalls.Load())
			assert.Equal(t, int32(1), persistCalls.Load())
			assert.Zero(t, secondLockCalls.Load(), "usable unknown-expiry token must not enter the refresh transaction")
			assert.Equal(t, []string{"Bearer gat_new_1", "Bearer gat_new_1"}, protectedAuthorizations)
		})
	}
}

func TestRefreshTransport_PersistsRotatedGenerationThenBlocksOnMalformedExpiries(t *testing.T) {
	var refreshCalls, protectedCalls, persistCalls atomic.Int32
	var persistedToken, persistedRefresh, persistedExpires, persistedRefreshExpires string
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
				body := `{"data":{"token":"gat_new","expires_at":"not-rfc3339","refresh_token":"gar_new","refresh_expires_at":"also-not-rfc3339"}}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			}
			protectedCalls.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		OnRefresh: func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
			persistCalls.Add(1)
			assert.Equal(t, "gar_old", previousRefreshToken)
			persistedToken = token
			persistedRefresh = refreshToken
			persistedExpires = expiresAt
			persistedRefreshExpires = refreshExpiresAt
			return nil
		},
	}
	request := func() error {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		return err
	}

	require.ErrorIs(t, request(), auth.ErrInvalidRefreshResponse)
	assert.Equal(t, "gat_new", persistedToken)
	assert.Equal(t, "gar_new", persistedRefresh)
	recoveryExpiry, err := time.Parse(time.RFC3339, persistedExpires)
	require.NoError(t, err)
	assert.True(t, recoveryExpiry.Before(time.Now()))
	assert.Empty(t, persistedRefreshExpires)
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(1), persistCalls.Load())
	assert.Zero(t, protectedCalls.Load())

	require.ErrorIs(t, request(), auth.ErrInvalidRefreshResponse)
	assert.Equal(t, int32(1), refreshCalls.Load(), "invalid generation must not trigger another refresh in-process")
	assert.Equal(t, int32(1), persistCalls.Load())
	assert.Zero(t, protectedCalls.Load())
}

func TestRefreshTransport_EmptyAccessTokenPersistsRecoveryWithoutExposure(t *testing.T) {
	var refreshCalls, protectedCalls, persistCalls atomic.Int32
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
				body := `{"data":{"token":"","expires_at":"2099-01-01T00:00:00Z","refresh_token":"gar_new","refresh_expires_at":"2099-02-01T00:00:00Z"}}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			}
			protectedCalls.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		OnRefresh: func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
			persistCalls.Add(1)
			assert.Equal(t, "gar_old", previousRefreshToken)
			assert.Equal(t, "gat_old", token, "recovery must retain a nonempty access placeholder")
			assert.Equal(t, "gar_new", refreshToken)
			recoveryExpiry, err := time.Parse(time.RFC3339, expiresAt)
			require.NoError(t, err)
			assert.True(t, recoveryExpiry.Before(time.Now()), "recovery access token must never look fresh")
			assert.Equal(t, "2099-02-01T00:00:00Z", refreshExpiresAt)
			return nil
		},
	}

	for range 2 {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		require.ErrorIs(t, err, auth.ErrInvalidRefreshResponse)
	}
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(1), persistCalls.Load())
	assert.Zero(t, protectedCalls.Load(), "an empty access token must never reach the protected API")
}

func TestRefreshTransport_Malformed200WithoutRotatedRefreshBlocksPermanently(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{"data":`},
		{name: "empty refresh token", body: `{"data":{"token":"gat_new","expires_at":"2099-01-01T00:00:00Z","refresh_token":"","refresh_expires_at":"2099-02-01T00:00:00Z"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var refreshCalls, protectedCalls, persistCalls atomic.Int32
			transport := &auth.RefreshTransport{
				Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Path == "/api/cli/v1/auth/refresh" {
						refreshCalls.Add(1)
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header), Request: req}, nil
					}
					protectedCalls.Add(1)
					return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
				}),
				ProxyEndpoint: "https://proxy.invalid",
				Token:         "gat_old",
				RefreshToken:  "gar_old",
				ExpiresAt:     time.Now().Add(time.Minute),
				OnRefresh: func(_, _, _, _, _ string) error {
					persistCalls.Add(1)
					return nil
				},
			}
			for range 2 {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
				require.NoError(t, err)
				resp, roundTripErr := transport.RoundTrip(req)
				if resp != nil {
					require.NoError(t, resp.Body.Close())
				}
				err = roundTripErr
				require.ErrorIs(t, err, auth.ErrInvalidRefreshResponse)
				assert.NotContains(t, err.Error(), "gat_new")
				assert.NotContains(t, err.Error(), "gar_old")
			}
			token, err := transport.FreshToken(t.Context())
			require.ErrorIs(t, err, auth.ErrInvalidRefreshResponse)
			assert.Empty(t, token)
			assert.Equal(t, int32(1), refreshCalls.Load(), "the consumed old refresh token must not be retried")
			assert.Zero(t, persistCalls.Load(), "the old or absent generation must not be persisted")
			assert.Zero(t, protectedCalls.Load())
			assert.Empty(t, transport.RefreshToken)
		})
	}
}

func TestRefreshTransport_NonRotatingRefreshTokenRemainsCompatible(t *testing.T) {
	var refreshCalls, protectedCalls, persistCalls atomic.Int32
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
				body := `{"data":{"token":"gat_new","expires_at":"2099-01-01T00:00:00Z","refresh_token":"gar_same","refresh_expires_at":"2099-02-01T00:00:00Z"}}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			}
			protectedCalls.Add(1)
			assert.Equal(t, "Bearer gat_new", req.Header.Get("Authorization"))
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_same",
		ExpiresAt:     time.Now().Add(time.Minute),
		OnRefresh: func(previousRefreshToken, token, refreshToken, _, _ string) error {
			persistCalls.Add(1)
			assert.Equal(t, "gar_same", previousRefreshToken)
			assert.Equal(t, "gar_same", refreshToken)
			assert.Equal(t, "gat_new", token)
			return nil
		},
	}

	for range 2 {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(1), persistCalls.Load())
	assert.Equal(t, int32(2), protectedCalls.Load())
}

func TestRefreshTransport_ValidResponsePersistsAndReachesProtectedAPI(t *testing.T) {
	var refreshCalls, protectedCalls, persistCalls atomic.Int32
	var authorization atomic.Value
	authorization.Store("")
	transport := &auth.RefreshTransport{
		Base: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/cli/v1/auth/refresh" {
				refreshCalls.Add(1)
				body := `{"data":{"token":"gat_new","expires_at":"2099-01-01T00:00:00Z","refresh_token":"gar_new","refresh_expires_at":"2099-02-01T00:00:00Z"}}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			}
			protectedCalls.Add(1)
			authorization.Store(req.Header.Get("Authorization"))
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
		}),
		ProxyEndpoint: "https://proxy.invalid",
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(time.Minute),
		OnRefresh: func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
			persistCalls.Add(1)
			assert.Equal(t, "gar_old", previousRefreshToken)
			assert.Equal(t, "gat_new", token)
			assert.Equal(t, "gar_new", refreshToken)
			assert.Equal(t, "2099-01-01T00:00:00Z", expiresAt)
			assert.Equal(t, "2099-02-01T00:00:00Z", refreshExpiresAt)
			return nil
		},
	}

	for range 2 {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://proxy.invalid/protected", nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(1), persistCalls.Load())
	assert.Equal(t, int32(2), protectedCalls.Load())
	assert.Equal(t, "Bearer gat_new", authorization.Load())
}

func TestRefreshTransport_ReturnsErrRefreshTokenExpired_On401(t *testing.T) {
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"statusCode":401,"message":"invalid or expired refresh token"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_old",
		RefreshToken:  "gar_stale",
		ExpiresAt:     time.Now().Add(1 * time.Minute), // within refresh threshold
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error for 401 refresh response, got nil")
	}
	if !errors.Is(err, auth.ErrRefreshTokenExpired) {
		t.Fatalf("expected ErrRefreshTokenExpired, got: %v", err)
	}
}

// TestRefreshTransport_NetworkRefreshSurvivesRequestCancellation guards the
// same invariant as the parse-failure fix: a successful refresh response must
// never be dropped, because the server has already rotated the refresh token.
// If the caller's context is cancelled while the refresh is in flight, the
// refresh must still complete — otherwise we lose the newly rotated tokens and
// the next invocation is locked out with a 401.
func TestRefreshTransport_NetworkRefreshSurvivesRequestCancellation(t *testing.T) {
	refreshReceived := make(chan struct{})
	serverCanRespond := make(chan struct{})

	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			close(refreshReceived)
			<-serverCanRespond
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_new",
					"expires_at":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
					"refresh_token":      "gar_new",
					"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(1 * time.Minute),
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the caller's context the moment the refresh request arrives at
	// the server — simulating a user Ctrl+C (or parent timeout) while the
	// server is about to rotate the token.
	go func() {
		<-refreshReceived
		cancel()
		// Give the cancellation a moment to propagate, then let the server
		// finish rotating and writing the response.
		time.Sleep(50 * time.Millisecond)
		close(serverCanRespond)
	}()

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, _ := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}

	// The outer request will fail because ctx is cancelled, but the refresh
	// must have completed and the rotated tokens must have been adopted.
	if transport.Token != "gat_new" {
		t.Fatalf("expected refresh to survive ctx cancellation (token=%q); server rotated the refresh token but client never learned it", transport.Token)
	}
	if transport.RefreshToken != "gar_new" {
		t.Fatalf("expected refresh token to be rotated to %q, got %q", "gar_new", transport.RefreshToken)
	}
}

func TestDoRefresh_ErrorFormat_Unauthorized(t *testing.T) {
	secretBody := `{"access_token":"leaked-secret"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(secretBody))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: server.URL,
		Token:         "gat_old",
		RefreshToken:  "gar_stale",
		ExpiresAt:     time.Now().Add(1 * time.Minute),
	}

	client := &http.Client{Transport: transport}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/test", nil)
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, auth.ErrRefreshTokenExpired) {
		t.Fatalf("expected ErrRefreshTokenExpired to be wrapped, got: %v", err)
	}
	if strings.Contains(err.Error(), "leaked-secret") {
		t.Fatalf("error message must not contain response body, got: %v", err)
	}
	if !strings.Contains(err.Error(), "oauth refresh failed") {
		t.Fatalf("error message should match oauth format, got: %v", err)
	}
}

func TestDoRefresh_ErrorFormat_ServerError(t *testing.T) {
	secretBody := `{"error":"internal","secret":"do-not-leak"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(secretBody))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: server.URL,
		Token:         "gat_old",
		RefreshToken:  "gar_valid",
		ExpiresAt:     time.Now().Add(1 * time.Minute),
	}

	client := &http.Client{Transport: transport}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/test", nil)
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("error message must not contain response body, got: %v", err)
	}
	if !strings.Contains(err.Error(), "oauth refresh failed") {
		t.Fatalf("error message should match oauth format, got: %v", err)
	}
}

func TestRefreshTransport_RejectsExpiredRefreshToken(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	transport := &auth.RefreshTransport{
		Base:             http.DefaultTransport,
		ProxyEndpoint:    backend.URL,
		Token:            "gat_old",
		RefreshToken:     "gar_expired",
		ExpiresAt:        time.Now().Add(1 * time.Minute), // within refresh threshold
		RefreshExpiresAt: time.Now().Add(-1 * time.Hour),  // already expired
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, backend.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error for expired refresh token, got nil")
	}
	if !errors.Is(err, auth.ErrRefreshTokenExpired) {
		t.Fatalf("expected ErrRefreshTokenExpired, got: %v", err)
	}
}
