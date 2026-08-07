package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolvePaste is the arm of both run loops that handles a pasted redirect URL.
// It had no coverage at all: every flow-level test forces agent mode, which
// makes startPasteWatcher return nil, so `case pasted := <-paste.Input():` never
// ran. Turning agent mode off is not an option either — the flow would launch a
// real browser. So the arm lives in its own function and is driven directly.
//
// These tests are package-internal because resolvePaste, the arbiter and
// pasteWatcher are all unexported.

// pasteTerminal is a *pasteWatcher wired to an in-memory writer, so a test can
// read back exactly what the user would have seen.
func pasteTerminal(w *lockedBuffer) *pasteWatcher {
	return &pasteWatcher{writer: w}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// stackExchange serves the assistant token exchange. status != 200 makes the
// exchange fail *after* the authorization code has been sent — a spent code.
func stackExchange(t *testing.T, status int) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cli/v1/auth/exchange", func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]string{
				"token":        "gat_test_token",
				"email":        "user@example.com",
				"api_endpoint": srv.URL,
			},
		})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func pastedValues(endpoint string) url.Values {
	return url.Values{
		"state":            {flowState},
		"code":             {"pasted-authorization-code"},
		"endpoint":         {endpoint},
		"instanceEndpoint": {"https://mystack.grafana.net"},
	}
}

const flowState = "state-belonging-to-this-flow"

func TestResolvePaste_SucceedsAndSettles(t *testing.T) {
	exchange := stackExchange(t, http.StatusOK)
	out := &lockedBuffer{}
	f := &Flow{writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	out2 := f.resolvePaste(
		context.Background(), arb, pasteTerminal(out),
		pastedInput{Values: pastedValues(exchange.URL)}, flowState, "verifier")

	require.True(t, out2.done)
	require.NoError(t, out2.err)
	require.NotNil(t, out2.result)
	assert.Equal(t, "gat_test_token", out2.result.Token)
	assert.Contains(t, out.String(), "single-use code", "the hygiene notice must still print")
	assert.Equal(t, claimTaken, arb.claim(), "a settled flow must reject a later claim")
}

// The #0 finding: a failed exchange used to release the flow, which let the
// browser callback — carrying the very same code — claim and exchange it again.
func TestResolvePaste_SpentCodeEndsTheFlowInsteadOfReleasingIt(t *testing.T) {
	exchange := stackExchange(t, http.StatusInternalServerError)
	out := &lockedBuffer{}
	f := &Flow{writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	got := f.resolvePaste(
		context.Background(), arb, pasteTerminal(out),
		pastedInput{Values: pastedValues(exchange.URL)}, flowState, "verifier")

	require.True(t, got.done, "a spent authorization code must end the flow")
	require.Error(t, got.err)
	assert.Nil(t, got.result)
	assert.Contains(t, got.err.Error(), "token exchange failed")

	assert.Equal(t, claimTaken, arb.claim(),
		"the flow must stay closed so the browser callback cannot exchange the same code again")
	assert.NotContains(t, out.String(), "Redirect URL",
		"gcx must not ask for another paste when the code is already spent")
}

// A failure *before* the exchange spends nothing, so #1136's re-prompt survives.
func TestResolvePaste_FailureBeforeExchangeReleasesAndReprompts(t *testing.T) {
	out := &lockedBuffer{}
	f := &Flow{writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	// A URL with our state but no authorization code: rejected by
	// handleCallbackParams before anything reaches the token endpoint.
	values := url.Values{"state": {flowState}}

	got := f.resolvePaste(
		context.Background(), arb, pasteTerminal(out), pastedInput{Values: values}, flowState, "verifier")

	require.False(t, got.done, "nothing was spent, so the flow stays alive")
	require.NoError(t, got.err)
	assert.Nil(t, got.result)
	assert.Contains(t, out.String(), "That URL did not work")
	assert.Contains(t, out.String(), "Redirect URL", "the user must be asked again")
	assert.Equal(t, claimGranted, arb.claim(), "the flow must be claimable again")
}

func TestResolvePaste_ForeignURLRepromptsWithoutClaiming(t *testing.T) {
	out := &lockedBuffer{}
	f := &Flow{writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	values := url.Values{"state": {"from-a-different-login"}, "code": {"other"}}
	got := f.resolvePaste(
		context.Background(), arb, pasteTerminal(out), pastedInput{Values: values}, flowState, "verifier")

	require.False(t, got.done)
	require.NoError(t, got.err)
	assert.Contains(t, out.String(), "different login attempt")
	assert.Equal(t, claimGranted, arb.claim(), "a foreign paste must not consume the flow")
}

func TestResolvePaste_ParseErrorRepromptsWithoutClaiming(t *testing.T) {
	out := &lockedBuffer{}
	f := &Flow{writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	got := f.resolvePaste(
		context.Background(), arb, pasteTerminal(out),
		pastedInput{Err: errManualForeignState}, flowState, "verifier")

	require.False(t, got.done)
	require.NoError(t, got.err)
	assert.Contains(t, out.String(), "That URL did not work")
	assert.Equal(t, claimGranted, arb.claim())
}

// The #6 finding: losing the race to the browser used to print nothing, so the
// terminal looked frozen while an exchange the user could not see ran.
func TestResolvePaste_SupersededTellsTheUserWhy(t *testing.T) {
	out := &lockedBuffer{}
	f := &Flow{writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	require.Equal(t, claimGranted, arb.claim(), "the browser callback got there first")

	got := f.resolvePaste(
		context.Background(), arb, pasteTerminal(out),
		pastedInput{Values: pastedValues("http://127.0.0.1:1")}, flowState, "verifier")

	require.False(t, got.done)
	require.NoError(t, got.err)
	assert.Contains(t, out.String(), "browser callback arrived first")
}

// The #2 finding: when the deadline fires mid-exchange, release() expires the
// flow — and gcx used to print a fresh prompt one instant before giving up.
func TestResolvePaste_ExpiredDoesNotPromptForInputItCannotAccept(t *testing.T) {
	out := &lockedBuffer{}
	f := &Flow{writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	// A paste arriving after the deadline has already passed.
	arb.deadlineReached()
	got := f.resolvePaste(
		context.Background(), arb, pasteTerminal(out),
		pastedInput{Values: pastedValues("http://127.0.0.1:1")}, flowState, "verifier")

	require.False(t, got.done)
	require.NoError(t, got.err)
	assert.Empty(t, out.String(), "the expiry case reports the timeout; this must stay quiet")
}

func TestResolvePaste_DeadlineDuringExchangeSkipsTheReprompt(t *testing.T) {
	out := &lockedBuffer{}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	// Claim, then let the deadline land mid-exchange, then fail before spending
	// the code. release() must expire the flow rather than reopen it.
	require.Equal(t, claimGranted, arb.claim())
	arb.deadlineReached()
	require.False(t, arb.release(), "a late deadline must close the flow on release")

	select {
	case <-arb.expired():
	default:
		t.Fatal("the flow must have expired")
	}
	assert.NotContains(t, out.String(), "Redirect URL")
}

// Both legs must agree branch for branch; the grafana.com one is pinned too.
func TestGCOMResolvePaste_SpentCodeEndsTheFlow(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	out := &lockedBuffer{}
	f := &GCOMFlow{opts: GCOMOptions{ClientID: "gcx", GCOMURL: srv.URL}, writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	values := url.Values{"state": {flowState}, "code": {"cloud-code"}}
	got := f.resolvePaste(
		context.Background(), arb, pasteTerminal(out), pastedInput{Values: values},
		flowState, "verifier", "http://127.0.0.1:54321/callback")

	require.True(t, got.done)
	require.Error(t, got.err)
	assert.Equal(t, claimTaken, arb.claim(), "a spent Cloud code must not be exchangeable again")
	assert.NotContains(t, out.String(), "Redirect URL")
}
