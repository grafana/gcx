package auth_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncBuffer is a writer that a running flow and the test can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForOutput waits until the flow has written want.
func waitForOutput(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Contains(out.String(), want) },
		5*time.Second, 10*time.Millisecond, "output never contained %q; got %q", want, out.String())
}

// localSessionForTest makes the session local and not agent mode: the state in
// which a flow opens the browser and the reopen shortcut can apply.
func localSessionForTest(t *testing.T) {
	t.Helper()
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Setenv(name, "")
	}
	t.Setenv("GCX_AGENT_MODE", "0")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}

// browserRecorder stands in for the browser and reports every URL a flow
// opens, in order.
type browserRecorder struct {
	opened chan string
}

func recordBrowser(t *testing.T) *browserRecorder {
	t.Helper()
	r := &browserRecorder{opened: make(chan string, 8)}
	t.Cleanup(auth.SwapOpenBrowser(func(u string) (bool, error) {
		r.opened <- u
		return true, nil
	}))
	return r
}

func (r *browserRecorder) next(t *testing.T) string {
	t.Helper()
	select {
	case u := <-r.opened:
		return u
	case <-time.After(5 * time.Second):
		t.Fatal("the flow did not open the browser")
		return ""
	}
}

type flowOutcome struct {
	result *auth.Result
	err    error
}

func startFlow(ctx context.Context, flow *auth.Flow) <-chan flowOutcome {
	done := make(chan flowOutcome, 1)
	go func() {
		result, err := flow.Run(ctx)
		done <- flowOutcome{result: result, err: err}
	}()
	return done
}

func waitForOutcome(t *testing.T, done <-chan flowOutcome) flowOutcome {
	t.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(5 * time.Second):
		t.Fatal("the flow did not finish")
		return flowOutcome{}
	}
}

func assertStillWaiting(t *testing.T, done <-chan flowOutcome) {
	t.Helper()
	select {
	case outcome := <-done:
		t.Fatalf("the flow ended early: result=%v err=%v", outcome.result, outcome.err)
	case <-time.After(200 * time.Millisecond):
	}
}

// consentParams returns the consent query of an opened URL. For the signup
// entry page it reads the query of the nested return target.
func consentParams(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	if to := q.Get("to"); to != "" {
		inner, err := url.Parse(to)
		require.NoError(t, err)
		q = inner.Query()
	}
	return q
}

func callbackURL(port string, q url.Values) string {
	return "http://127.0.0.1:" + port + "/callback?" + q.Encode()
}

func sendCallback(t *testing.T, method, rawURL string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, rawURL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// TestCallbackServerIgnoresCallbacksFromOtherAttempts pins the ownership gate:
// only a GET that carries this attempt's state may end the login. A tab left
// over from an earlier attempt lands on the same port when the new attempt
// picks it again, and before the gate its callback aborted the new login.
func TestCallbackServerIgnoresCallbacksFromOtherAttempts(t *testing.T) {
	localSessionForTest(t)
	browser := recordBrowser(t)
	var calls atomic.Int32
	exchange := newExchangeServer(t, &calls)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: &out}))

	params := consentParams(t, browser.next(t))
	port := params.Get("callback_port")
	ours := url.Values{
		"state":            {params.Get("state")},
		"code":             {"auth-code"},
		"endpoint":         {exchange.URL},
		"instanceEndpoint": {"https://mystack.grafana.net"},
	}

	foreign := url.Values{"state": {"state-from-an-earlier-attempt"}, "code": {"old-code"}, "endpoint": {exchange.URL}}
	status, body := sendCallback(t, http.MethodGet, callbackURL(port, foreign))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, body, "earlier login attempt")

	status, _ = sendCallback(t, http.MethodGet, callbackURL(port, url.Values{"code": {"no-state"}}))
	assert.Equal(t, http.StatusBadRequest, status)

	for _, method := range []string{http.MethodPost, http.MethodHead} {
		status, _ = sendCallback(t, method, callbackURL(port, ours))
		assert.Equal(t, http.StatusMethodNotAllowed, status, method)
	}

	assertStillWaiting(t, done)
	assert.Zero(t, calls.Load(), "no ignored callback may spend a code")

	status, body = sendCallback(t, http.MethodGet, callbackURL(port, ours))
	assert.Equal(t, http.StatusOK, status)
	// The browser page claims only the browser step: gcx validates and saves
	// the connection afterwards, and either can still fail.
	assert.Contains(t, body, "Authorization complete")
	assert.Contains(t, body, "Return to your terminal")
	assert.NotContains(t, body, "Connected")
	outcome := waitForOutcome(t, done)
	require.NoError(t, outcome.err)
	assert.Equal(t, "gat_token", outcome.result.Token)
	assert.Equal(t, int32(1), calls.Load())
}

// TestCallbackServerStopsOnBrowserCancel covers Cancel on the consent page: the
// flow ends with ErrBrowserCancelled, spends no code, and the browser shows a
// neutral page instead of a failure.
func TestCallbackServerStopsOnBrowserCancel(t *testing.T) {
	localSessionForTest(t)
	browser := recordBrowser(t)
	var calls atomic.Int32
	newExchangeServer(t, &calls)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: &out}))

	params := consentParams(t, browser.next(t))
	cancelled := url.Values{"state": {params.Get("state")}, "error": {"user_cancelled"}}
	status, body := sendCallback(t, http.MethodGet, callbackURL(params.Get("callback_port"), cancelled))
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "Login cancelled")
	assert.NotContains(t, body, "Authentication Failed")

	outcome := waitForOutcome(t, done)
	require.ErrorIs(t, outcome.err, auth.ErrBrowserCancelled)
	assert.Zero(t, calls.Load())
}

// TestFlowRun_ManualStopsOnBrowserCancel covers the manual paste route: a
// pasted cancellation ends the flow instead of asking for another URL.
func TestFlowRun_ManualStopsOnBrowserCancel(t *testing.T) {
	t.Parallel()

	var writer bytes.Buffer
	reader := &lazyReader{build: func() io.Reader {
		values := url.Values{"state": {stateFromWriter(t, &writer)}, "error": {"user_cancelled"}}
		return strings.NewReader("http://127.0.0.1:54321/callback?" + values.Encode() + "\n")
	}}

	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{Manual: true, Writer: &writer, Reader: reader})
	_, err := flow.Run(context.Background())
	require.ErrorIs(t, err, auth.ErrBrowserCancelled)
	assert.NotContains(t, writer.String(), "That URL did not work")
}

// TestPasteRouteStopsOnBrowserCancel covers the SSH paste route that runs
// alongside the callback server.
func TestPasteRouteStopsOnBrowserCancel(t *testing.T) {
	remoteSessionForTest(t)
	browser := recordBrowser(t)
	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	t.Cleanup(auth.SwapPasteTerminal(reader, true))

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: &out}))

	params := consentParams(t, browser.next(t))
	waitForOutput(t, &out, "Redirect URL")
	cancelled := url.Values{"state": {params.Get("state")}, "error": {"user_cancelled"}}
	_, err = writerEnd.WriteString(callbackURL(params.Get("callback_port"), cancelled) + "\n")
	require.NoError(t, err)

	outcome := waitForOutcome(t, done)
	require.ErrorIs(t, outcome.err, auth.ErrBrowserCancelled)
	assert.NotContains(t, out.String(), "That URL did not work")
}

// TestLauncherAndSignupURLs pins where the browser goes. Without a stack the
// consent URL is the Grafana Cloud launcher on the configured portal. The
// signup entry is the account creation page, whose grafana.com-relative return
// target is exactly that launcher path and query.
func TestLauncherAndSignupURLs(t *testing.T) {
	t.Parallel()

	const consentPath = "/a/grafana-assistant-app/cli/auth"
	tests := []struct {
		name       string
		endpoint   string
		opts       auth.Options
		wantAuth   string // scheme, host and path of the consent URL
		wantSignup bool
	}{
		{
			name:     "stack login ignores the portal and signup options",
			endpoint: "https://mystack.grafana.net",
			opts:     auth.Options{LaunchOrigin: "https://grafana-dev.com", Signup: true},
			wantAuth: "https://mystack.grafana.net" + consentPath,
		},
		{
			name:     "launcher defaults to production",
			wantAuth: "https://grafana.com/launch" + consentPath,
		},
		{
			name:     "launcher follows the portal origin",
			opts:     auth.Options{LaunchOrigin: "https://grafana-dev.com/"},
			wantAuth: "https://grafana-dev.com/launch" + consentPath,
		},
		{
			name:       "signup starts on the account creation page",
			opts:       auth.Options{LaunchOrigin: "https://grafana-dev.com", Signup: true},
			wantAuth:   "https://grafana-dev.com/launch" + consentPath,
			wantSignup: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			flow := auth.NewFlow(tc.endpoint, tc.opts)
			authURL, entryURL := flow.AuthAndEntryURLs(54399, "synthetic-state", "synthetic-challenge")

			consent, err := url.Parse(authURL)
			require.NoError(t, err)
			assert.Equal(t, tc.wantAuth, consent.Scheme+"://"+consent.Host+consent.Path)
			for key, want := range map[string]string{
				"callback_port":         "54399",
				"state":                 "synthetic-state",
				"code_challenge":        "synthetic-challenge",
				"code_challenge_method": "S256",
			} {
				assert.Equal(t, want, consent.Query().Get(key), key)
			}

			if !tc.wantSignup {
				assert.Equal(t, authURL, entryURL)
				return
			}
			entry, err := url.Parse(entryURL)
			require.NoError(t, err)
			assert.Equal(t, consent.Scheme+"://"+consent.Host, entry.Scheme+"://"+entry.Host)
			assert.Equal(t, "/auth/sign-up/create-user", entry.Path)
			assert.Equal(t, []string{"to"}, keys(entry.Query()), "only the return target is top-level")
			returnTo := entry.Query().Get("to")
			assert.True(t, strings.HasPrefix(returnTo, "/launch/") && !strings.HasPrefix(returnTo, "//"),
				"the return target must be a grafana.com-relative path, got %q", returnTo)
			assert.Equal(t, consent.Scheme+"://"+consent.Host+returnTo, authURL,
				"the return target must be the launcher path and query, unchanged")
		})
	}
}

func keys(q url.Values) []string {
	out := make([]string, 0, len(q))
	for key := range q {
		out = append(out, key)
	}
	return out
}

// TestFlowRun_RejectsUntrustedLaunchOrigin checks the portal origin before the
// flow prints or binds anything, and only when no stack endpoint is known.
func TestFlowRun_RejectsUntrustedLaunchOrigin(t *testing.T) {
	t.Parallel()

	for _, origin := range []string{
		"https://example.invalid",
		"http://grafana-dev.com",
		"https://grafana-dev.com@example.invalid",
		"https://user@grafana-dev.com",
		"https://grafana-dev.com/launch",
		"https://grafana-dev.com?x=1",
		"https://grafana-dev.com#fragment",
	} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()

			var writer bytes.Buffer
			flow := auth.NewFlow("", auth.Options{LaunchOrigin: origin, Manual: true, Writer: &writer, Reader: strings.NewReader("")})
			_, err := flow.Run(context.Background())
			require.ErrorContains(t, err, "invalid Grafana Cloud URL for the browser login")
			assert.Zero(t, writer.Len(), "no instructions before the origin check")
		})
	}

	t.Run("a stack endpoint does not use the origin", func(t *testing.T) {
		t.Parallel()

		var writer bytes.Buffer
		flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
			LaunchOrigin: "https://example.invalid", Manual: true, Writer: &writer, Reader: strings.NewReader(""),
		})
		_, err := flow.Run(context.Background())
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "invalid Grafana Cloud URL")
	})
}

// TestFlowRun_SignupReopensTheLauncherOnEnter covers the recovery shortcut:
// Enter opens the launcher again (never the signup page) with the same state,
// and the login still completes through the callback.
func TestFlowRun_SignupReopensTheLauncherOnEnter(t *testing.T) {
	localSessionForTest(t)
	browser := recordBrowser(t)
	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	t.Cleanup(auth.SwapPasteTerminal(reader, true))
	var calls atomic.Int32
	exchange := newExchangeServer(t, &calls)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("", auth.Options{
		Writer:        &out,
		LaunchOrigin:  "https://grafana-dev.com",
		Signup:        true,
		ReopenOnEnter: true,
	}))

	first, err := url.Parse(browser.next(t))
	require.NoError(t, err)
	assert.Equal(t, "/auth/sign-up/create-user", first.Path)
	waitForOutput(t, &out, "Create your Grafana Cloud account")
	waitForOutput(t, &out, "press Enter to open it again")

	_, err = writerEnd.WriteString("\n")
	require.NoError(t, err)
	second := browser.next(t)
	assert.Equal(t, "https://grafana-dev.com"+first.Query().Get("to"), second,
		"Enter reopens the launcher that the signup page returns to")
	assert.Contains(t, out.String(), "Opening the login page again")

	params := consentParams(t, second)
	ours := url.Values{
		"state":            {params.Get("state")},
		"code":             {"auth-code"},
		"endpoint":         {exchange.URL},
		"instanceEndpoint": {"https://mystack.grafana-dev.net"},
	}
	status, _ := sendCallback(t, http.MethodGet, callbackURL(params.Get("callback_port"), ours))
	assert.Equal(t, http.StatusOK, status)
	outcome := waitForOutcome(t, done)
	require.NoError(t, outcome.err)
	assert.Equal(t, "https://mystack.grafana-dev.net", outcome.result.InstanceEndpoint)
	assert.Equal(t, int32(1), calls.Load())
}

// TestFlowRun_ReopenOnlyForTheLauncherInALocalSession keeps the shortcut
// narrow: a stack login keeps its old waiting line, and over SSH the paste
// watcher keeps the terminal.
func TestFlowRun_ReopenOnlyForTheLauncherInALocalSession(t *testing.T) {
	tests := []struct {
		name     string
		remote   bool
		endpoint string
		opts     auth.Options
		want     string
	}{
		{name: "stack login", endpoint: "https://mystack.grafana.net", opts: auth.Options{ReopenOnEnter: true}, want: "Waiting for authentication..."},
		{name: "launcher without the option", opts: auth.Options{}, want: "Waiting for authentication..."},
		{name: "launcher over SSH", remote: true, opts: auth.Options{ReopenOnEnter: true}, want: "Redirect URL"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.remote {
				remoteSessionForTest(t)
			} else {
				localSessionForTest(t)
			}
			recordBrowser(t)
			reader, writerEnd, err := os.Pipe()
			require.NoError(t, err)
			t.Cleanup(func() { _ = writerEnd.Close() })
			t.Cleanup(auth.SwapPasteTerminal(reader, true))

			var out syncBuffer
			ctx, cancel := context.WithCancel(t.Context())
			opts := tc.opts
			opts.Writer = &out
			done := startFlow(ctx, auth.NewFlow(tc.endpoint, opts))

			waitForOutput(t, &out, tc.want)
			assert.NotContains(t, out.String(), "press Enter to open it again")
			cancel()
			outcome := waitForOutcome(t, done)
			require.ErrorIs(t, outcome.err, context.Canceled)
		})
	}
}

// TestReopenNeverFollowsAFinishedLogin covers a keypress that arrives with the
// consent callback. Both are ready in the same select round, which Go resolves
// at random, so the loop must check for the result before it reopens:
// reopening after success would leave a consent tab that cannot return.
func TestReopenNeverFollowsAFinishedLogin(t *testing.T) {
	t.Parallel()

	for range 200 {
		reopened := false
		result, err := auth.AwaitReadyResult(t.Context(), auth.ReopenRequestForTest(), &auth.Result{Token: "gat_token"},
			func() { reopened = true })
		require.NoError(t, err)
		require.Equal(t, "gat_token", result.Token)
		require.False(t, reopened, "reopened the login page after the login finished")
	}
}

// TestReopenWatcher covers the local watcher on its own: every line, an empty
// one included, is a reopen request; it prints nothing itself; and it stays
// out of remote sessions and agent mode.
func TestReopenWatcher(t *testing.T) {
	t.Run("delivers a reopen request per line", func(t *testing.T) {
		localSessionForTest(t)
		reader, writerEnd, err := os.Pipe()
		require.NoError(t, err)
		t.Cleanup(func() { _ = writerEnd.Close() })
		t.Cleanup(auth.SwapPasteTerminal(reader, true))

		var out bytes.Buffer
		watcher := auth.StartReopenWatcher(&out)
		require.NotNil(t, watcher)
		t.Cleanup(watcher.Close)

		for _, line := range []string{"\n", "anything\n"} {
			_, err = writerEnd.WriteString(line)
			require.NoError(t, err)
			select {
			case in := <-watcher.Input():
				assert.True(t, in.Reopen, "line %q", line)
				assert.Nil(t, in.Values)
			case <-time.After(5 * time.Second):
				t.Fatalf("no reopen request for %q", line)
			}
		}
		assert.Zero(t, out.Len())
	})

	t.Run("reports that the terminal ended", func(t *testing.T) {
		localSessionForTest(t)
		reader, writerEnd, err := os.Pipe()
		require.NoError(t, err)
		t.Cleanup(auth.SwapPasteTerminal(reader, true))

		watcher := auth.StartReopenWatcher(io.Discard)
		require.NotNil(t, watcher)
		require.NoError(t, writerEnd.Close())
		select {
		case in := <-watcher.Input():
			assert.True(t, in.Closed)
			assert.False(t, in.Reopen)
		case <-time.After(5 * time.Second):
			t.Fatal("the watcher did not report that the terminal ended")
		}
		watcher.Close()
	})

	for name, setup := range map[string]func(*testing.T){
		"remote session": remoteSessionForTest,
		"agent mode": func(t *testing.T) {
			t.Helper()
			localSessionForTest(t)
			t.Setenv("GCX_AGENT_MODE", "1")
			agent.ResetForTesting()
		},
	} {
		t.Run("not in "+name, func(t *testing.T) {
			setup(t)
			reader, writerEnd, err := os.Pipe()
			require.NoError(t, err)
			t.Cleanup(func() { _ = writerEnd.Close(); _ = reader.Close() })
			t.Cleanup(auth.SwapPasteTerminal(reader, true))

			assert.Nil(t, auth.StartReopenWatcher(io.Discard))
		})
	}
}

// TestFlowRun_ReopenWaitsOutAnExchangeAndDebounces covers two reopen guards.
// Repeated Enter presses open one page, not one per press. Once the callback
// has arrived and its token exchange is running, Enter opens nothing, because
// a new consent tab could only end on 410 Gone.
func TestFlowRun_ReopenWaitsOutAnExchangeAndDebounces(t *testing.T) {
	localSessionForTest(t)
	browser := recordBrowser(t)
	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	t.Cleanup(auth.SwapPasteTerminal(reader, true))

	entered := make(chan struct{})
	var enteredOnce sync.Once
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseExchange := func() { releaseOnce.Do(func() { close(release) }) }
	exchange := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"token":"gat_token","api_endpoint":"https://mystack.grafana.net"}}`))
	}))
	t.Cleanup(exchange.Close)
	// Cleanups run last-in first-out: a failed assertion releases the held
	// exchange before Close waits for it, so the test fails instead of hanging.
	t.Cleanup(releaseExchange)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("", auth.Options{Writer: &out, ReopenOnEnter: true}))
	params := consentParams(t, browser.next(t))
	waitForOutput(t, &out, "press Enter to open it again")

	_, err = writerEnd.WriteString("\n\n\n")
	require.NoError(t, err)
	browser.next(t)
	assertNoOpen(t, browser)

	ours := url.Values{
		"state":            {params.Get("state")},
		"code":             {"auth-code"},
		"endpoint":         {exchange.URL},
		"instanceEndpoint": {"https://mystack.grafana.net"},
	}
	// The callback blocks until the exchange is released, so it runs in a
	// goroutine. It must not call t: it may still be running when a failed
	// assertion ends the test.
	callbackDone := make(chan int, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, callbackURL(params.Get("callback_port"), ours), nil)
		if err != nil {
			callbackDone <- 0
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			callbackDone <- 0
			return
		}
		_ = resp.Body.Close()
		callbackDone <- resp.StatusCode
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the callback never reached the token exchange")
	}
	time.Sleep(2500 * time.Millisecond) // past the debounce, so only the exchange guard can stop a reopen
	_, err = writerEnd.WriteString("\n")
	require.NoError(t, err)
	assertNoOpen(t, browser)

	releaseExchange()
	assert.Equal(t, http.StatusOK, <-callbackDone)
	outcome := waitForOutcome(t, done)
	require.NoError(t, outcome.err)
	assert.Equal(t, "gat_token", outcome.result.Token)
}

func assertNoOpen(t *testing.T, browser *browserRecorder) {
	t.Helper()
	select {
	case u := <-browser.opened:
		t.Fatalf("unexpected browser open: %s", u)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestFlowRun_ManualSignupPrintsTheSteps keeps the signup guidance when the
// browser runs on another computer.
func TestFlowRun_ManualSignupPrintsTheSteps(t *testing.T) {
	t.Parallel()

	var writer bytes.Buffer
	flow := auth.NewFlow("", auth.Options{Signup: true, Manual: true, Writer: &writer, Reader: strings.NewReader("")})
	_, err := flow.Run(context.Background())
	require.Error(t, err)
	out := writer.String()
	assert.Contains(t, out, "/auth/sign-up/create-user?to=")
	// One numbered list, in the order the user acts: open the URL, create the
	// account, approve, then paste the redirect URL back.
	openURL := strings.Index(out, "1. Open this URL")
	createAccount := strings.Index(out, "2. Create your Grafana Cloud account")
	verify := strings.Index(out, "3. Verification code")
	copyAddress := strings.Index(out, "Copy the full address")
	require.NotEqual(t, -1, openURL, out)
	require.NotEqual(t, -1, createAccount, out)
	assert.Less(t, openURL, createAccount)
	assert.Less(t, createAccount, verify)
	assert.Less(t, verify, copyAddress)
	assert.NotContains(t, out, "In the browser:", "the callback route's separate list does not repeat here")
}

// TestFlowRun_LauncherSSHHintKeepsCloud checks the rerun command that the SSH
// hint names on the no-stack path: without --cloud, a rerun with no server has
// no launcher to go to.
func TestFlowRun_LauncherSSHHintKeepsCloud(t *testing.T) {
	remoteSessionForTest(t)
	t.Setenv("GCX_AGENT_MODE", "1") // no paste watcher, so the hint prints
	agent.ResetForTesting()
	recordBrowser(t)

	var out syncBuffer
	ctx, cancel := context.WithCancel(t.Context())
	done := startFlow(ctx, auth.NewFlow("", auth.Options{Writer: &out}))
	waitForOutput(t, &out, "Waiting for authentication...")
	assert.Contains(t, out.String(), "gcx login --cloud --oauth-manual")
	cancel()
	require.ErrorIs(t, waitForOutcome(t, done).err, context.Canceled)
}

// TestTerminalDeviceCandidates pins which paths the macOS fallback may open:
// terminal devices only, never /dev/tty itself or a descriptor alias such as
// /dev/stdin or /dev/fd/0, which would share the shell's open file description.
func TestTerminalDeviceCandidates(t *testing.T) {
	t.Parallel()

	got := auth.TerminalDeviceCandidates(
		[]string{"tty", "ttys003", "ttyp0", "stdin", "stderr", "fd", "null", "ptmx"},
		[]string{"/dev/pts/4", "/dev/pts/ptmx"},
	)
	assert.Equal(t, []string{"/dev/pts/4", "/dev/ttys003", "/dev/ttyp0"}, got)
}

// TestCallbackServerKeepsWaitingAfterAnIncompleteCallback pins that a callback
// carrying this attempt's state, but not what the token exchange needs, cannot
// use up the one-shot handler. It spent nothing, so the login keeps waiting and
// the real callback that follows still completes it. A provider error is the
// consent page's own answer and still ends the login.
func TestCallbackServerKeepsWaitingAfterAnIncompleteCallback(t *testing.T) {
	localSessionForTest(t)
	browser := recordBrowser(t)
	var calls atomic.Int32
	exchange := newExchangeServer(t, &calls)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: &out}))

	params := consentParams(t, browser.next(t))
	port := params.Get("callback_port")
	state := params.Get("state")
	ours := url.Values{
		"state":            {state},
		"code":             {"auth-code"},
		"endpoint":         {exchange.URL},
		"instanceEndpoint": {"https://mystack.grafana.net"},
	}

	for name, q := range map[string]url.Values{
		"no code":            {"state": {state}, "endpoint": {exchange.URL}},
		"no endpoint":        {"state": {state}, "code": {"auth-code"}},
		"untrusted endpoint": {"state": {state}, "code": {"auth-code"}, "endpoint": {"https://grafana.example.com"}},
	} {
		status, body := sendCallback(t, http.MethodGet, callbackURL(port, q))
		assert.Equal(t, http.StatusBadRequest, status, name)
		assert.Contains(t, body, "still running", name)
		assert.NotContains(t, body, "Authentication Failed", name)
	}

	assertStillWaiting(t, done)
	assert.Zero(t, calls.Load(), "an incomplete callback may not spend a code")
	assert.Contains(t, out.String(), "gcx ignored it and is still waiting", "the terminal says why nothing happened")

	status, body := sendCallback(t, http.MethodGet, callbackURL(port, ours))
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "Authorization complete")
	outcome := waitForOutcome(t, done)
	require.NoError(t, outcome.err)
	assert.Equal(t, "gat_token", outcome.result.Token)
	assert.Equal(t, int32(1), calls.Load())
}

func TestCallbackServerEndsOnProviderErrorAfterAnIncompleteCallback(t *testing.T) {
	localSessionForTest(t)
	browser := recordBrowser(t)

	var out syncBuffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := startFlow(ctx, auth.NewFlow("https://mystack.grafana.net", auth.Options{Writer: &out}))

	params := consentParams(t, browser.next(t))
	port := params.Get("callback_port")
	state := params.Get("state")

	status, _ := sendCallback(t, http.MethodGet, callbackURL(port, url.Values{"state": {state}}))
	assert.Equal(t, http.StatusBadRequest, status)
	assertStillWaiting(t, done)

	status, _ = sendCallback(t, http.MethodGet, callbackURL(port, url.Values{"state": {state}, "error": {"access_denied"}}))
	assert.Equal(t, http.StatusBadRequest, status)
	outcome := waitForOutcome(t, done)
	require.Error(t, outcome.err)
	assert.Contains(t, outcome.err.Error(), "access_denied")
}

// TestGCOMCallbackServerKeepsWaitingAfterACallbackWithoutACode is the
// grafana.com flow's half of the incomplete-callback rule. Its callback carries
// no endpoint, so a missing code is the one check that comes before its token
// exchange.
func TestGCOMCallbackServerKeepsWaitingAfterACallbackWithoutACode(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server, errCh := auth.StartGCOMCallbackServer(t.Context(), listener, "gcom-state")
	t.Cleanup(func() { _ = server.Close() })
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)

	status, body := sendCallback(t, http.MethodGet, callbackURL(port, url.Values{"state": {"gcom-state"}}))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, body, "still running")
	select {
	case err := <-errCh:
		t.Fatalf("a callback without a code ended the login: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	status, _ = sendCallback(t, http.MethodGet, callbackURL(port, url.Values{"state": {"gcom-state"}, "error": {"access_denied"}}))
	assert.Equal(t, http.StatusBadRequest, status)
	select {
	case err := <-errCh:
		assert.Contains(t, err.Error(), "access_denied")
	case <-time.After(5 * time.Second):
		t.Fatal("a provider error did not end the login")
	}

	status, _ = sendCallback(t, http.MethodGet, callbackURL(port, url.Values{"state": {"gcom-state"}, "code": {"late"}}))
	assert.Equal(t, http.StatusGone, status)
}

// TestFlowRun_LauncherSSHHintUsesTheManualCommand pins that the remote session
// hint repeats the command the caller names, which signup sets to a sign in:
// by the time the person finds the callback unreachable, the account exists.
func TestFlowRun_LauncherSSHHintUsesTheManualCommand(t *testing.T) {
	remoteSessionForTest(t)
	t.Setenv("GCX_AGENT_MODE", "1") // no paste watcher, so the hint prints
	agent.ResetForTesting()
	recordBrowser(t)

	var out syncBuffer
	ctx, cancel := context.WithCancel(t.Context())
	const manual = "gcx login mystack --cloud --oauth-manual --config ./c.yaml"
	done := startFlow(ctx, auth.NewFlow("", auth.Options{Signup: true, ManualCommand: manual, Writer: &out}))
	waitForOutput(t, &out, "Waiting for authentication...")
	assert.Contains(t, out.String(), "Run it again with "+manual+".")
	cancel()
	require.ErrorIs(t, waitForOutcome(t, done).err, context.Canceled)
}
