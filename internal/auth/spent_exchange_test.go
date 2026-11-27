package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatedExchange is a token endpoint that holds every request until the test
// releases it, then answers 500. It lets a test act while an exchange is in
// flight without sleeping.
type gatedExchange struct {
	*httptest.Server

	hits    atomic.Int32
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGatedExchange(t *testing.T) *gatedExchange {
	t.Helper()
	g := &gatedExchange{entered: make(chan struct{}, 8), release: make(chan struct{})}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.hits.Add(1)
		g.entered <- struct{}{}
		select {
		case <-g.release:
		case <-r.Context().Done():
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(g.Close)
	t.Cleanup(g.open)
	return g
}

func (g *gatedExchange) open() { g.once.Do(func() { close(g.release) }) }

func (g *gatedExchange) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the token exchange never started")
	}
}

func assertNotEnded(t *testing.T, done <-chan flowOutcome) {
	t.Helper()
	select {
	case outcome := <-done:
		t.Fatalf("the flow ended early: result=%v err=%v", outcome.result, outcome.err)
	default:
	}
}

func pasteTerminal(t *testing.T) *os.File {
	t.Helper()
	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	t.Cleanup(auth.SwapPasteTerminal(reader, true))
	return writerEnd
}

func stackPaste(state, endpoint string) url.Values {
	return url.Values{
		"state":            {state},
		"code":             {"auth-code"},
		"endpoint":         {endpoint},
		"instanceEndpoint": {"https://mystack.grafana.net"},
	}
}

// TestSpentRejectionsAfterTheExchange pins that a failure after the exchange
// guard is claimed is marked spent, whichever check fails, and that the guard
// stays claimed. A retryable mark here would re-prompt for a code that is used up.
func TestSpentRejectionsAfterTheExchange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		instance string
	}{
		{"unparsable instanceEndpoint", "http://[::1"},
		{"non-https instanceEndpoint", "http://mystack.grafana.net"},
		{"missing instanceEndpoint", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			exchange := newExchangeServer(t, &calls)
			q := stackPaste("st", exchange.URL)
			q.Set("instanceEndpoint", tt.instance)
			guard := &auth.ExchangeGuard{}

			got := auth.HandleCallbackOutcome(t.Context(), q, "st", "verifier", guard)
			require.Error(t, got.Err)
			assert.True(t, got.Spent, "the code is used up")
			assert.False(t, got.Retryable)
			assert.Equal(t, int32(1), calls.Load())
			assert.False(t, auth.ClaimExchange(guard), "the guard stays claimed")
		})
	}
}

// TestGCOMSpentExchangeFailure pins the spent mark on the grafana.com handler.
func TestGCOMSpentExchangeFailure(t *testing.T) {
	t.Parallel()

	gcom := newGatedExchange(t)
	gcom.open()
	guard := &auth.ExchangeGuard{}
	q := url.Values{"state": {"st"}, "code": {"auth-code"}}

	got := auth.HandleGCOMCallbackOutcome(t.Context(), gcom.URL, q, "st", guard)
	require.ErrorContains(t, got.Err, "token exchange failed")
	assert.True(t, got.Spent)
	assert.False(t, got.Retryable)
	assert.Equal(t, int32(1), gcom.hits.Load())
	assert.False(t, auth.ClaimExchange(guard), "the guard stays claimed")
}

// TestPreClaimRejectionsAreNotSpent pins that a rejection before the exchange
// spends nothing and leaves the guard free for the real callback.
func TestPreClaimRejectionsAreNotSpent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		q         url.Values
		retryable bool
	}{
		{"no code", url.Values{"state": {"st"}, "endpoint": {"http://127.0.0.1:1"}}, true},
		{"no endpoint", url.Values{"state": {"st"}, "code": {"c"}}, true},
		{"untrusted endpoint", url.Values{"state": {"st"}, "code": {"c"}, "endpoint": {"https://grafana.example.com"}}, true},
		{"bad state", url.Values{"state": {"other"}, "code": {"c"}, "endpoint": {"http://127.0.0.1:1"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			guard := &auth.ExchangeGuard{}
			got := auth.HandleCallbackOutcome(t.Context(), tt.q, "st", "verifier", guard)
			require.Error(t, got.Err)
			assert.False(t, got.Spent)
			assert.Equal(t, tt.retryable, got.Retryable)
			assert.True(t, auth.ClaimExchange(guard), "nothing claimed the guard")
		})
	}

	t.Run("gcom no code", func(t *testing.T) {
		t.Parallel()
		guard := &auth.ExchangeGuard{}
		got := auth.HandleGCOMCallbackOutcome(t.Context(), "https://grafana.com", url.Values{"state": {"st"}}, "st", guard)
		require.Error(t, got.Err)
		assert.False(t, got.Spent)
		assert.True(t, got.Retryable)
		assert.True(t, auth.ClaimExchange(guard))
	})
}

// TestPasteRouteRepromptsOnPreClaimRejections pins that rejected pastes keep
// the wait alive and the exchange unspent: the real URL pasted afterwards
// still logs in, with exactly one exchange.
func TestPasteRouteRepromptsOnPreClaimRejections(t *testing.T) {
	remoteSessionForTest(t)
	browser := recordBrowser(t)
	writerEnd := pasteTerminal(t)
	var calls atomic.Int32
	exchange := newExchangeServer(t, &calls)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: &out}))

	params := consentParams(t, browser.next(t))
	waitForOutput(t, &out, "Redirect URL")
	port, state := params.Get("callback_port"), params.Get("state")

	bad := []url.Values{
		{"state": {state}, "endpoint": {exchange.URL}},
		{"state": {state}, "code": {"auth-code"}},
		{"state": {state}, "code": {"auth-code"}, "endpoint": {"https://grafana.example.com"}},
		{"state": {"other"}, "code": {"auth-code"}, "endpoint": {exchange.URL}},
	}
	for _, q := range bad {
		_, err := writerEnd.WriteString(callbackURL(port, q) + "\n")
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { return countOf(out.String(), "That URL did not work") == len(bad) },
		5*time.Second, 10*time.Millisecond, "each rejected paste re-prompts; got %q", out.String())
	assertStillWaiting(t, done)
	assert.Zero(t, calls.Load(), "a pre-claim rejection may not spend the code")

	_, err := writerEnd.WriteString(callbackURL(port, stackPaste(state, exchange.URL)) + "\n")
	require.NoError(t, err)
	outcome := waitForOutcome(t, done)
	require.NoError(t, outcome.err)
	assert.Equal(t, "gat_token", outcome.result.Token)
	assert.Equal(t, int32(1), calls.Load())
}

func countOf(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

// TestGCOMPasteRouteEndsOnExchangeFailure is the grafana.com flow's half of the
// paste exchange failure: the flow ends with the error, and a browser callback
// that arrives during the exchange neither starts a second one nor ends the wait.
func TestGCOMPasteRouteEndsOnExchangeFailure(t *testing.T) {
	remoteSessionForTest(t)
	browser := recordBrowser(t)
	writerEnd := pasteTerminal(t)
	gcom := newGatedExchange(t)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	flow := auth.NewGCOMFlow(auth.GCOMOptions{ClientID: "gcx", GCOMURL: gcom.URL, Writer: &out})
	type gcomOutcome struct{ err error }
	done := make(chan gcomOutcome, 1)
	go func() {
		_, err := flow.Run(ctx)
		done <- gcomOutcome{err}
	}()

	opened := browser.next(t)
	waitForOutput(t, &out, "Redirect URL")
	u, err := url.Parse(opened)
	require.NoError(t, err)
	redirect, err := url.Parse(u.Query().Get("redirect_uri"))
	require.NoError(t, err)
	q := url.Values{"state": {u.Query().Get("state")}, "code": {"auth-code"}}

	_, err = writerEnd.WriteString(callbackURL(redirect.Port(), q) + "\n")
	require.NoError(t, err)
	gcom.waitEntered(t)

	status, _ := sendCallback(t, http.MethodGet, callbackURL(redirect.Port(), q))
	assert.Equal(t, http.StatusOK, status, "the claimed guard answers the browser without exchanging")
	select {
	case o := <-done:
		t.Fatalf("a losing callback ended the flow: %v", o.err)
	default:
	}
	gcom.open()

	select {
	case o := <-done:
		require.ErrorContains(t, o.err, "token exchange failed")
	case <-time.After(5 * time.Second):
		t.Fatal("the flow did not end after the exchange failed")
	}
	assert.Equal(t, int32(1), gcom.hits.Load(), "the code is exchanged once")
	assert.NotContains(t, out.String(), "That URL did not work")
}

// TestCallbackRouteEndsOnExchangeFailure pins that a browser callback whose
// exchange fails ends the login with the exchange error.
func TestCallbackRouteEndsOnExchangeFailure(t *testing.T) {
	localSessionForTest(t)
	capture.Reset()
	t.Cleanup(capture.Reset)
	browser := recordBrowser(t)
	exchange := newGatedExchange(t)
	exchange.open()

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: &out}))

	params := consentParams(t, browser.next(t))
	status, body := sendCallback(t, http.MethodGet, callbackURL(params.Get("callback_port"), stackPaste(params.Get("state"), exchange.URL)))
	assert.NotEqual(t, http.StatusOK, status)
	assert.Contains(t, body, "Token exchange failed")

	outcome := waitForOutcome(t, done)
	require.ErrorContains(t, outcome.err, "token exchange failed")
	var statusErr *auth.ExchangeStatusError
	require.ErrorAs(t, outcome.err, &statusErr)
	assert.Equal(t, http.StatusInternalServerError, statusErr.StatusCode)
	assert.Equal(t, int32(1), exchange.hits.Load())
	assert.Equal(t, "callback", capture.CurrentLoginStage())
}

// TestCtrlCDuringPasteExchangeStaysACancellation pins that an interrupt during
// a pasted URL's exchange returns the context's own error, which main exits on
// silently (isSilentCancellation matches context.Canceled), not a wrapped
// exchange failure that prints.
func TestCtrlCDuringPasteExchangeStaysACancellation(t *testing.T) {
	remoteSessionForTest(t)
	browser := recordBrowser(t)
	writerEnd := pasteTerminal(t)
	exchange := newGatedExchange(t)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	runCtx, interrupt := context.WithCancel(ctx)
	defer interrupt()
	done := startFlow(runCtx, auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: &out}))

	params := consentParams(t, browser.next(t))
	waitForOutput(t, &out, "Redirect URL")
	_, err := writerEnd.WriteString(callbackURL(params.Get("callback_port"), stackPaste(params.Get("state"), exchange.URL)) + "\n")
	require.NoError(t, err)
	exchange.waitEntered(t)

	interrupt()
	outcome := waitForOutcome(t, done)
	require.Error(t, outcome.err)
	assert.Equal(t, context.Canceled, outcome.err, "Ctrl-C returns ctx.Err() itself")
}
