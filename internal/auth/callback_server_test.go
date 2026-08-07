package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests drive the real callback listener. Until this change nothing in
// this package ever issued a request to it, which is why #1147 went unnoticed:
// the one-shot guard was claimed on arrival, so any request at all aborted the
// login and left the genuine callback to be answered with 410 Gone.

// syncWriter is the flow's Writer. The flow writes from its own goroutine while
// the test reads to recover the callback port and state, so a bare bytes.Buffer
// would be a data race.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// waitingMarker is the last line a flow prints before it blocks on the
// callback, so seeing it means the listener is up.
const waitingMarker = "Waiting for authentication"

// waitForListener blocks until the flow has printed waitingMarker, so the test
// never races ahead of the listener being up.
func (w *syncWriter) waitForListener(t *testing.T) string {
	t.Helper()
	const marker = waitingMarker
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if out := w.String(); strings.Contains(out, marker) {
			return out
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in flow output; got:\n%s", marker, w.String())
	return ""
}

var (
	callbackPortRe = regexp.MustCompile(`callback_port=(\d+)`)
	stateRe        = regexp.MustCompile(`[?&]state=([^&\s]+)`)
	redirectURIRe  = regexp.MustCompile(`[?&]redirect_uri=([^&\s]+)`)
)

func mustSubmatch(t *testing.T, re *regexp.Regexp, s, what string) string {
	t.Helper()
	m := re.FindStringSubmatch(s)
	require.Len(t, m, 2, "could not find the %s in the flow output", what)
	unescaped, err := url.QueryUnescape(m[1])
	require.NoError(t, err)
	return unescaped
}

// exchangeServer stands in for the assistant backend. block, when non-nil,
// holds the exchange open so a test can act while the flow is mid-claim.
func newCallbackExchangeServer(t *testing.T, block <-chan struct{}, entered chan<- struct{}) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cli/v1/auth/exchange", func(w http.ResponseWriter, _ *http.Request) {
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		if block != nil {
			<-block
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

// gcomServer records the redirect_uri the token exchange sent, so a test can
// prove it is byte-identical to the one on the authorize request. GCOM rejects
// the exchange otherwise.
type gcomServer struct {
	*httptest.Server

	mu                  sync.Mutex
	exchangeRedirectURI string
	exchanges           int
}

func newGCOMServer(t *testing.T, block <-chan struct{}) *gcomServer {
	t.Helper()
	g := &gcomServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		if block != nil {
			<-block
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)

		g.mu.Lock()
		g.exchangeRedirectURI = payload["redirect_uri"]
		g.exchanges++
		g.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "glc_test_token",
			"scope":        "stacks:read",
			"expires_in":   3600,
			"info":         map[string]string{"email": "user@example.com", "login": "user"},
		})
	})
	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Close)
	return g
}

type runningFlow struct {
	writer   *syncWriter
	state    string
	port     string
	results  chan flowOutcome
	exchange *httptest.Server
}

type flowOutcome struct {
	result *auth.Result
	err    error
}

// startStackFlow runs the stack OAuth flow on an automatically chosen port and
// waits until its listener is up. The caller must consume f.results.
func startStackFlow(t *testing.T, ctx context.Context, block <-chan struct{}, entered chan<- struct{}) *runningFlow {
	t.Helper()
	// Agent mode keeps the browser shut. It also makes startPasteWatcher return
	// nil, so these tests never touch /dev/tty.
	testutils.SetAgentMode(t, true)

	exchange := newCallbackExchangeServer(t, block, entered)
	writer := &syncWriter{}
	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: writer})

	results := make(chan flowOutcome, 1)
	go func() {
		result, err := flow.Run(ctx)
		results <- flowOutcome{result: result, err: err}
	}()
	// Join the flow goroutine before the test's global state is restored.
	t.Cleanup(func() {
		select {
		case <-results:
		case <-time.After(15 * time.Second):
			t.Error("the flow goroutine did not exit")
		}
	})

	out := writer.waitForListener(t)
	return &runningFlow{
		writer:   writer,
		state:    mustSubmatch(t, stateRe, out, "state"),
		port:     mustSubmatch(t, callbackPortRe, out, "callback port"),
		results:  results,
		exchange: exchange,
	}
}

func (f *runningFlow) callbackURL(query url.Values) string {
	return "http://127.0.0.1:" + f.port + "/callback?" + query.Encode()
}

func (f *runningFlow) validQuery() url.Values {
	return url.Values{
		"state":            {f.state},
		"code":             {"valid-authorization-code"},
		"endpoint":         {f.exchange.URL},
		"instanceEndpoint": {"https://mystack.grafana.net"},
	}
}

// reply is what the tests assert on. The helper returns fields rather than the
// *http.Response so the body is provably closed at exactly one place.
type reply struct {
	status int
	allow  string
	body   string
}

func do(t *testing.T, ctx context.Context, method, rawURL string) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return reply{status: resp.StatusCode, allow: resp.Header.Get("Allow"), body: string(body)}
}

func get(t *testing.T, ctx context.Context, rawURL string) reply {
	t.Helper()
	return do(t, ctx, http.MethodGet, rawURL)
}

func requireStillRunning(t *testing.T, f *runningFlow) {
	t.Helper()
	select {
	case outcome := <-f.results:
		f.results <- outcome // put it back so the cleanup drain succeeds
		t.Fatalf("the flow ended early: result=%v err=%v", outcome.result, outcome.err)
	case <-time.After(150 * time.Millisecond):
	}
}

// The #1147 repro: a request carrying the wrong state used to consume the
// one-shot handler and abort the login, so the real callback that followed got
// 410 Gone and Run returned "invalid state - possible CSRF attack".
func TestCallbackServer_WrongStateDoesNotConsumeTheFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	flow := startStackFlow(t, ctx, nil, nil)

	// Three foreign requests, of the shapes a stale tab or another local
	// process actually produces.
	foreign := []url.Values{
		{"state": {"state-from-an-earlier-login"}, "code": {"stale-code"}},
		{"code": {"code-with-no-state"}},
		{"state": {""}},
	}
	for _, q := range foreign {
		got := get(t, ctx, flow.callbackURL(q))
		assert.Equal(t, http.StatusBadRequest, got.status, "a foreign callback is answered, not accepted")
		assert.NotContains(t, got.body, "CSRF", "a stale callback must not be reported as an attack")
		requireStillRunning(t, flow)
	}

	// The genuine callback still succeeds.
	assert.Equal(t, http.StatusOK, get(t, ctx, flow.callbackURL(flow.validQuery())).status)

	outcome := <-flow.results
	flow.results <- outcome
	require.NoError(t, outcome.err, "a legitimate callback after rejected ones must still complete the login")
	require.NotNil(t, outcome.result)
	assert.Equal(t, "gat_test_token", outcome.result.Token)
}

func TestCallbackServer_RejectsNonGETMethods(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	flow := startStackFlow(t, ctx, nil, nil)

	// HEAD is called out explicitly: a "GET /callback" ServeMux pattern would
	// also match it, which would let a HEAD request run the token exchange.
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			got := do(t, ctx, method, flow.callbackURL(flow.validQuery()))
			assert.Equal(t, http.StatusMethodNotAllowed, got.status)
			assert.Equal(t, http.MethodGet, got.allow)
			requireStillRunning(t, flow)
		})
	}

	// Even carrying a perfectly valid query, none of them consumed the flow.
	assert.Equal(t, http.StatusOK, get(t, ctx, flow.callbackURL(flow.validQuery())).status)

	outcome := <-flow.results
	flow.results <- outcome
	require.NoError(t, outcome.err)
}

// A replay must stay rejected. The window that matters is while the first
// exchange is still running, so the exchange is held open for it.
func TestCallbackServer_ReplayIsRejectedWhileTheFirstExchangeRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseExchange := func() { releaseOnce.Do(func() { close(release) }) }

	entered := make(chan struct{}, 1)
	flow := startStackFlow(t, ctx, release, entered)

	// Registered *after* startStackFlow, and that ordering is the whole point.
	// t.Cleanup is LIFO, and startStackFlow registers the httptest server's
	// Close, which waits for outstanding requests. So the release has to run
	// first, which means it has to be registered last. Registering it before —
	// the obvious-looking order — deadlocks the entire test binary on any early
	// t.Fatal instead of reporting the failure, which is the exact hang this is
	// meant to prevent.
	t.Cleanup(releaseExchange)

	// Issued from a goroutine because the exchange is held open below, so this
	// request does not return until the test releases it. No assertions here:
	// they would run off the test's own goroutine.
	firstCallback := make(chan struct{})
	go func() {
		defer close(firstCallback)
		req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodGet, flow.callbackURL(flow.validQuery()), nil)
		if err != nil {
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the exchange never started")
	}

	// The flow is claimed and mid-exchange. A replay must not start a second one.
	assert.Equal(t, http.StatusGone, get(t, ctx, flow.callbackURL(flow.validQuery())).status)

	releaseExchange()
	<-firstCallback

	outcome := <-flow.results
	flow.results <- outcome
	require.NoError(t, outcome.err)

	// The flow shuts the listener down once it returns, so the
	// already-settled case is pinned on the arbiter instead:
	// TestCallbackArbiter_FirstClaimWinsAndReplayIsRejected.
}

func TestCallbackServer_DenialForThisFlowIsTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	flow := startStackFlow(t, ctx, nil, nil)

	// A denial that does not carry our state is somebody else's business.
	assert.Equal(t, http.StatusBadRequest,
		get(t, ctx, flow.callbackURL(url.Values{"error": {"access_denied"}, "state": {"other-login"}})).status)
	requireStillRunning(t, flow)

	// A denial for this flow ends it, as it always did.
	assert.Equal(t, http.StatusBadRequest,
		get(t, ctx, flow.callbackURL(url.Values{"error": {"access_denied"}, "state": {flow.state}})).status)

	outcome := <-flow.results
	flow.results <- outcome
	require.Error(t, outcome.err)
	assert.Contains(t, outcome.err.Error(), "authentication denied")
}

func TestCallbackServer_MissingCodeForThisFlowIsTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	flow := startStackFlow(t, ctx, nil, nil)

	assert.Equal(t, http.StatusBadRequest, get(t, ctx, flow.callbackURL(url.Values{"state": {flow.state}})).status)

	outcome := <-flow.results
	flow.results <- outcome
	require.Error(t, outcome.err)
	assert.Contains(t, outcome.err.Error(), "no authorization code")
}

// Nothing the caller of a rejected callback supplies may come back out, in the
// browser page, the flow's output, or the error. The authorization URL
// necessarily carries this flow's own state, so that is not what is asserted.
func TestCallbackServer_RejectedCallbackValuesAreNotEchoed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	flow := startStackFlow(t, ctx, nil, nil)

	const (
		foreignState = "attacker-supplied-state-value"
		foreignCode  = "attacker-supplied-code-value"
	)
	got := get(t, ctx, flow.callbackURL(url.Values{"state": {foreignState}, "code": {foreignCode}}))
	assert.NotContains(t, got.body, foreignState)
	assert.NotContains(t, got.body, foreignCode)

	cancel()
	outcome := <-flow.results
	flow.results <- outcome
	require.Error(t, outcome.err)
	assert.NotContains(t, outcome.err.Error(), foreignState)
	assert.NotContains(t, outcome.err.Error(), foreignCode)

	out := flow.writer.String()
	assert.NotContains(t, out, foreignState)
	assert.NotContains(t, out, foreignCode)
}

func TestCallbackServer_TimesOutWhenNoCallbackArrives(t *testing.T) {
	testutils.SetAgentMode(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writer := &syncWriter{}
	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: writer})
	auth.SetAcquireTimeout(flow, 150*time.Millisecond)

	start := time.Now()
	_, err := flow.Run(ctx)

	require.Error(t, err, "a flow that never receives a callback must not wait forever")
	assert.Contains(t, err.Error(), "timed out")
	assert.Less(t, time.Since(start), 20*time.Second)
	assert.NoError(t, ctx.Err(), "the bound must be the flow's own, not the caller's context")
}

// A callback that lands inside the deadline is not cut off by it.
func TestCallbackServer_ValidCallbackBeatsTheDeadline(t *testing.T) {
	testutils.SetAgentMode(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exchange := newCallbackExchangeServer(t, nil, nil)
	writer := &syncWriter{}
	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: writer})
	auth.SetAcquireTimeout(flow, 10*time.Second)

	results := make(chan flowOutcome, 1)
	go func() {
		result, err := flow.Run(ctx)
		results <- flowOutcome{result: result, err: err}
	}()

	out := writer.waitForListener(t)
	state := mustSubmatch(t, stateRe, out, "state")
	port := mustSubmatch(t, callbackPortRe, out, "callback port")

	assert.Equal(t, http.StatusOK, get(t, ctx, "http://127.0.0.1:"+port+"/callback?"+url.Values{
		"state":            {state},
		"code":             {"valid-authorization-code"},
		"endpoint":         {exchange.URL},
		"instanceEndpoint": {"https://mystack.grafana.net"},
	}.Encode()).status)

	select {
	case outcome := <-results:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
	case <-time.After(15 * time.Second):
		t.Fatal("the flow goroutine did not exit")
	}
}

func TestCallbackServer_ContextCancellationEndsTheFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	flow := startStackFlow(t, ctx, nil, nil)
	cancel()

	select {
	case outcome := <-flow.results:
		flow.results <- outcome
		require.ErrorIs(t, outcome.err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling the context must end the flow")
	}
}

// The grafana.com leg shares newCallbackServer, so it inherits the same gate.
// Pinned here because it is a separate entry point and the two legs run
// back to back on overlapping ports, which is how #1147 was hit in practice.
func TestGCOMCallbackServer_ForeignCallbackDoesNotConsumeTheFlow(t *testing.T) {
	testutils.SetAgentMode(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gcom := newGCOMServer(t, nil)
	writer := &syncWriter{}
	flow := auth.NewGCOMFlow(auth.GCOMOptions{ClientID: "gcx", GCOMURL: gcom.URL, Scopes: []string{"stacks:read"}, Writer: writer})

	results := make(chan flowOutcome, 1)
	go func() {
		_, err := flow.Run(ctx)
		results <- flowOutcome{err: err}
	}()
	t.Cleanup(func() {
		select {
		case <-results:
		case <-time.After(15 * time.Second):
			t.Error("the flow goroutine did not exit")
		}
	})

	out := writer.waitForListener(t)
	state := mustSubmatch(t, stateRe, out, "state")
	redirectURI := mustSubmatch(t, redirectURIRe, out, "redirect URI")

	// A stale callback from the preceding stack leg.
	stale := get(t, ctx, redirectURI+"?state=state-from-the-stack-leg&code=stale")
	assert.Equal(t, http.StatusBadRequest, stale.status)
	assert.NotContains(t, stale.body, "CSRF")

	head := do(t, ctx, http.MethodHead, redirectURI+"?state="+url.QueryEscape(state))
	assert.Equal(t, http.StatusMethodNotAllowed, head.status)

	select {
	case outcome := <-results:
		results <- outcome
		t.Fatalf("the Cloud leg ended early: %v", outcome.err)
	case <-time.After(150 * time.Millisecond):
	}

	// The genuine Cloud callback still works.
	assert.Equal(t, http.StatusOK,
		get(t, ctx, redirectURI+"?"+url.Values{"state": {state}, "code": {"cloud-code"}}.Encode()).status)

	select {
	case outcome := <-results:
		results <- outcome
		require.NoError(t, outcome.err)
	case <-time.After(10 * time.Second):
		t.Fatal("the Cloud leg did not complete")
	}
}

// A discarded callback used to be completely invisible: nothing in the browser
// said the login was unaffected, and nothing in the terminal said a request had
// arrived at all. With a 30-minute bound, silence is a long time to stare at.
func TestCallbackServer_ForeignCallbackIsAnnouncedOnceAndReadsAsBenign(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	flow := startStackFlow(t, ctx, nil, nil)

	got := get(t, ctx, flow.callbackURL(url.Values{"state": {"from-an-earlier-attempt"}, "code": {"stale"}}))

	// The browser page must not read as a failed login: this callback changed
	// nothing, and the sign-in it belongs to is still running.
	assert.NotContains(t, got.body, "Authentication Failed")
	assert.NotContains(t, got.body, "try again")
	assert.Contains(t, got.body, "still running")

	// The terminal says something, exactly once, from the flow's own goroutine.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(flow.writer.String(), "Ignored a request") {
		time.Sleep(2 * time.Millisecond)
	}
	assert.Contains(t, flow.writer.String(), "Ignored a request",
		"the user must learn that a stray callback arrived")

	for range 3 {
		_ = get(t, ctx, flow.callbackURL(url.Values{"state": {"another"}, "code": {"stale"}}))
	}
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, strings.Count(flow.writer.String(), "Ignored a request"),
		"a flood of stray callbacks must not flood the terminal")

	requireStillRunning(t, flow)
}
