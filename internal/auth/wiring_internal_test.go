package auth

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The arbiter tests prove the contract. This one proves the wiring: that the
// real HTTP handler built by newCallbackServer and the real paste gate used by
// both flow loops arbitrate through the *same* arbiter instance.
//
// It matters because over SSH gcx runs both at once — it keeps the callback
// server listening while it reads a pasted redirect URL. Two separate guards
// would each let their own path start a token exchange for the same
// authorization code. The test is package-internal so it can call the
// production helpers directly, and it never opens a browser or a real flow.
func TestHTTPAndPasteGatesShareOneArbiter(t *testing.T) {
	const (
		state    = "state-for-this-flow"
		attempts = 100
	)
	pasted := url.Values{"state": {state}, "code": {"authcode"}}

	for range attempts {
		var (
			mu        sync.Mutex
			exchanges int
		)
		countExchange := func() {
			mu.Lock()
			exchanges++
			mu.Unlock()
		}

		arb := newCallbackArbiter(time.Hour)
		listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // no context plumbing needed for a test listener
		require.NoError(t, err)

		errCh := make(chan error, 1)
		server := newCallbackServer(listener, state, arb, errCh, nil, func(w http.ResponseWriter, _ *http.Request) {
			countExchange()
			w.WriteHeader(http.StatusOK)
		})

		callbackURL := "http://" + listener.Addr().String() + "/callback?state=" + url.QueryEscape(state) + "&code=authcode"

		var (
			start = make(chan struct{})
			wg    sync.WaitGroup
		)

		wg.Go(func() { // the browser callback
			<-start
			req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, callbackURL, nil)
			if reqErr != nil {
				return
			}
			resp, doErr := http.DefaultClient.Do(req)
			if doErr == nil {
				_ = resp.Body.Close()
			}
		})

		wg.Go(func() { // the pasted redirect URL, through the real gate
			<-start
			f := &Flow{writer: io.Discard}
			out := f.resolvePaste(context.Background(), arb, nil,
				pastedInput{Values: pasted}, state, "verifier")
			if out.done && out.err == nil {
				countExchange()
			}
		})

		close(start)
		wg.Wait()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = server.Shutdown(shutdownCtx)
		cancel()
		arb.stop()

		mu.Lock()
		got := exchanges
		mu.Unlock()
		require.Equal(t, 1, got, "exactly one of the browser callback and the pasted URL may run the exchange")
	}
}

// A foreign request must not consume the flow, whichever path it arrives on.
// Both gates are checked here together so they cannot drift apart.
func TestHTTPAndPasteGatesAgreeOnOwnership(t *testing.T) {
	const state = "state-for-this-flow"

	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // no context plumbing needed for a test listener
	require.NoError(t, err)

	// atomic, not a plain bool: this is written on the server's goroutine and
	// read on the test's. It does not race today only because the handler is
	// never reached, so a regression that made it reachable would surface as a
	// race-detector failure instead of the assertion this test is here for.
	var handled atomic.Bool
	errCh := make(chan error, 1)
	server := newCallbackServer(listener, state, arb, errCh, nil, func(w http.ResponseWriter, _ *http.Request) {
		handled.Store(true)
		w.WriteHeader(http.StatusOK)
	})
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	})

	base := "http://" + listener.Addr().String() + "/callback"

	// A wrong state over HTTP: answered, but the flow is untouched.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"?state=someone-elses&code=x", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	status := resp.StatusCode
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusBadRequest, status)
	assert.False(t, handled.Load(), "a foreign callback must not reach the exchange")

	// The same wrong state pasted: rejected, and the flow is still untouched.
	foreign := url.Values{"state": {"someone-elses"}, "code": {"x"}}
	f := &Flow{writer: io.Discard}
	out := f.resolvePaste(t.Context(), arb, nil, pastedInput{Values: foreign}, state, "verifier")
	assert.False(t, out.done, "a foreign paste must not end the flow")

	// After both, the legitimate callback still owns the flow.
	assert.Equal(t, claimGranted, arb.claim())

	select {
	case err := <-errCh:
		t.Fatalf("a foreign request must not report an error on the flow: %v", err)
	default:
	}
}

// A granted claim disarms the deadline, so a handler that claims and then never
// delivers would hang the flow forever. net/http recovers a panic per
// connection, which is exactly how that happens by accident.
func TestCallbackHandlerPanicStillEndsTheFlow(t *testing.T) {
	const state = "state-for-this-flow"

	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // no context plumbing needed for a test listener
	require.NoError(t, err)

	errCh := make(chan error, 1)
	server := newCallbackServer(listener, state, arb, errCh, nil, func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+listener.Addr().String()+"/callback?state="+url.QueryEscape(state)+"&code=x", nil)
	require.NoError(t, err)
	resp, doErr := http.DefaultClient.Do(req)
	require.NoError(t, doErr)
	status := resp.StatusCode
	require.NoError(t, resp.Body.Close())

	// Recovering the panic means the handler returns normally, and a handler
	// that returns without writing sends an empty 200 — so the browser would
	// show a blank success for a login that failed.
	assert.Equal(t, http.StatusInternalServerError, status,
		"a recovered panic must not look like success in the browser")

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "boom", "the panic value may carry request data")
	case <-time.After(10 * time.Second):
		t.Fatal("a panicking handler must still end the flow, not hang it")
	}
}
