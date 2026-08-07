package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
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

// Both legs must agree branch for branch, so the grafana.com one is driven
// through the same set. A divergence here is how the HTTP/paste split in the
// first round of this work went unnoticed.
func TestGCOMResolvePaste_MatchesTheStackLegBranchForBranch(t *testing.T) {
	const redirectURI = "http://127.0.0.1:54321/callback"
	ours := url.Values{"state": {flowState}, "code": {"cloud-code"}}

	tests := []struct {
		name string
		// tokenStatus drives the fake grafana.com token endpoint.
		tokenStatus int
		input       pastedInput
		// arrange runs before resolvePaste to put the arbiter in a given state.
		arrange      func(*callbackArbiter)
		wantDone     bool
		wantErr      bool
		wantOutput   string
		wantNoOutput string
		wantToken    string
		// wantClaimable is the verdict a later claim should get.
		wantClaim callbackClaim
	}{
		{
			name:        "success settles the flow",
			tokenStatus: http.StatusOK,
			input:       pastedInput{Values: ours},
			wantDone:    true,
			wantOutput:  "single-use code",
			wantToken:   "glc_test_token",
			wantClaim:   claimTaken,
		},
		{
			name:         "a spent code ends the flow and stays closed",
			tokenStatus:  http.StatusBadGateway,
			input:        pastedInput{Values: ours},
			wantDone:     true,
			wantErr:      true,
			wantNoOutput: "Redirect URL",
			wantClaim:    claimTaken,
		},
		{
			name:        "a failure before the exchange re-prompts",
			tokenStatus: http.StatusOK,
			// Our state but no code: rejected before anything is sent.
			input:      pastedInput{Values: url.Values{"state": {flowState}}},
			wantOutput: "Redirect URL",
			wantClaim:  claimGranted,
		},
		{
			name:        "a foreign URL never claims",
			tokenStatus: http.StatusOK,
			input:       pastedInput{Values: url.Values{"state": {"another-login"}, "code": {"x"}}},
			wantOutput:  "different login attempt",
			wantClaim:   claimGranted,
		},
		{
			name:        "a parse error re-prompts",
			tokenStatus: http.StatusOK,
			input:       pastedInput{Err: errManualForeignState},
			wantOutput:  "That URL did not work",
			wantClaim:   claimGranted,
		},
		{
			name:        "losing to the browser says so",
			tokenStatus: http.StatusOK,
			input:       pastedInput{Values: ours},
			arrange:     func(a *callbackArbiter) { a.claim() },
			wantOutput:  "browser callback arrived first",
			wantClaim:   claimTaken,
		},
		{
			name:         "after the deadline it stays quiet",
			tokenStatus:  http.StatusOK,
			input:        pastedInput{Values: ours},
			arrange:      func(a *callbackArbiter) { a.deadlineReached() },
			wantNoOutput: "Redirect URL",
			wantClaim:    claimExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
				if tt.tokenStatus != http.StatusOK {
					w.WriteHeader(tt.tokenStatus)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "glc_test_token",
					"scope":        "stacks:read",
				})
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			out := &lockedBuffer{}
			f := &GCOMFlow{opts: GCOMOptions{ClientID: "gcx", GCOMURL: srv.URL}, writer: out}
			arb := newCallbackArbiter(time.Hour)
			t.Cleanup(arb.stop)
			if tt.arrange != nil {
				tt.arrange(arb)
			}

			got := f.resolvePaste(context.Background(), arb, pasteTerminal(out), tt.input,
				flowState, "verifier", redirectURI)

			assert.Equal(t, tt.wantDone, got.done)
			if tt.wantToken != "" {
				require.NotNil(t, got.result)
				assert.Equal(t, tt.wantToken, got.result.AccessToken)
			}
			if tt.wantErr {
				require.Error(t, got.err)
			} else {
				require.NoError(t, got.err)
			}
			if tt.wantOutput != "" {
				assert.Contains(t, out.String(), tt.wantOutput)
			}
			if tt.wantNoOutput != "" {
				assert.NotContains(t, out.String(), tt.wantNoOutput)
			}
			assert.Equal(t, tt.wantClaim, arb.claim())
		})
	}
}

// Regression: an unusable paste must never own the flow, even briefly.
//
// It used to claim before anything checked it was usable, so a paste carrying
// our state but no code — the authorize URL pasted by mistake — owned the flow
// just long enough for the genuine browser callback to be answered 410 Gone and
// lost for good, after which the paste released and the login waited out its
// deadline. Same shape as #1147: something that is not the legitimate
// completion consuming the one-shot.
func TestResolvePaste_UnusablePasteNeverBurnsTheBrowserCallback(t *testing.T) {
	const state = "our-state"
	exchange := stackExchange(t, http.StatusOK)

	out := &lockedBuffer{}
	f := &Flow{writer: out}
	arb := newCallbackArbiter(time.Hour)
	t.Cleanup(arb.stop)

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test listener
	require.NoError(t, err)

	resultCh := make(chan *Result, 1)
	errCh := make(chan error, 1)
	server := f.startCallbackServer(t.Context(), listener, state, "verifier", arb, resultCh, errCh, nil)
	t.Cleanup(func() { _ = server.Close() })

	// Our state, no code. Retryable, so it must be rejected without claiming.
	got := f.resolvePaste(t.Context(), arb, pasteTerminal(out),
		pastedInput{Values: url.Values{"state": {state}}}, state, "verifier")
	require.False(t, got.done)
	assert.Contains(t, out.String(), "That URL did not work")

	// The genuine browser callback must still be accepted.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+listener.Addr().String()+"/callback?"+url.Values{
			"state":            {state},
			"code":             {"real-code"},
			"endpoint":         {exchange.URL},
			"instanceEndpoint": {"https://mystack.grafana.net"},
		}.Encode(), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	status := resp.StatusCode
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, status, "an unusable paste must not cost the real callback")

	select {
	case res := <-resultCh:
		assert.Equal(t, "gat_test_token", res.Token)
	case err := <-errCh:
		t.Fatalf("the browser callback should have completed the login: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the browser callback never completed the login")
	}
}
