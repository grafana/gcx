package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCallbackInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "full https url",
			input: "https://example.grafana.net/callback?code=c1&state=s1",
			want:  map[string]string{"code": "c1", "state": "s1"},
		},
		{
			name:  "loopback callback url",
			input: "http://127.0.0.1:54321/callback?code=c1&state=s1&endpoint=https%3A%2F%2Fapi.grafana.net",
			want:  map[string]string{"code": "c1", "state": "s1", "endpoint": "https://api.grafana.net"},
		},
		{
			name:  "surrounding whitespace",
			input: "   http://127.0.0.1:54321/callback?code=c1   ",
			want:  map[string]string{"code": "c1"},
		},
		{
			name:  "trailing carriage return",
			input: "http://127.0.0.1:54321/callback?code=c1\r",
			want:  map[string]string{"code": "c1"},
		},
		{
			name:  "double quoted",
			input: `"http://127.0.0.1:54321/callback?code=c1"`,
			want:  map[string]string{"code": "c1"},
		},
		{
			name:  "single quoted",
			input: `'http://127.0.0.1:54321/callback?code=c1'`,
			want:  map[string]string{"code": "c1"},
		},
		{
			name:  "scheme hidden by the browser",
			input: "127.0.0.1:54321/callback?code=c1&state=s1",
			want:  map[string]string{"code": "c1", "state": "s1"},
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "not a url",
			input:   "yes",
			wantErr: true,
		},
		{
			name:    "url without a query",
			input:   "http://127.0.0.1:54321/callback",
			wantErr: true,
		},
		{
			name:    "malformed query escape",
			input:   "http://127.0.0.1:54321/callback?code=%zz",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values, err := auth.ParseCallbackInput(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			for key, want := range tc.want {
				assert.Equal(t, want, values.Get(key), "parameter %q", key)
			}
		})
	}
}

// lazyReader builds its input on the first Read, after Flow.Run has printed
// the auth URL. This is how a test recovers the random state parameter.
type lazyReader struct {
	build func() io.Reader
	inner io.Reader
}

func (l *lazyReader) Read(p []byte) (int, error) {
	if l.inner == nil {
		l.inner = l.build()
	}
	return l.inner.Read(p)
}

var stateParamRE = regexp.MustCompile(`[?&]state=([^&\s]+)`)

// stateFromWriter extracts the state parameter from the printed auth URL.
func stateFromWriter(t *testing.T, w *bytes.Buffer) string {
	t.Helper()
	match := stateParamRE.FindStringSubmatch(w.String())
	require.Len(t, match, 2, "auth URL with a state parameter, got %q", w.String())
	state, err := url.QueryUnescape(match[1])
	require.NoError(t, err)
	return state
}

// newExchangeServer serves the assistant token exchange endpoint and counts
// the calls it receives.
func newExchangeServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/api/cli/v1/auth/exchange", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{
			"token":"gat_token",
			"email":"user@example.com",
			"expires_at":"2030-01-01T00:00:00Z",
			"api_endpoint":"https://mystack.grafana.net",
			"refresh_token":"gar_refresh",
			"refresh_expires_at":"2031-01-01T00:00:00Z"
		}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFlowRun_ManualExchangesPastedURL(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newExchangeServer(t, &calls)

	var writer bytes.Buffer
	reader := &lazyReader{build: func() io.Reader {
		values := url.Values{}
		values.Set("code", "auth-code")
		values.Set("state", stateFromWriter(t, &writer))
		values.Set("endpoint", server.URL)
		values.Set("instanceEndpoint", "https://mystack.grafana.net")
		values.Set("device", "my-box")
		return strings.NewReader("http://127.0.0.1:54321/callback?" + values.Encode() + "\n")
	}}

	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
		Manual: true,
		Writer: &writer,
		Reader: reader,
	})

	result, err := flow.Run(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "gat_token", result.Token)
	assert.Equal(t, "user@example.com", result.Email)
	assert.Equal(t, "my-box", result.DeviceName)
	assert.Equal(t, "https://mystack.grafana.net", result.APIEndpoint)
	assert.Equal(t, "2030-01-01T00:00:00Z", result.ExpiresAt)
	assert.Equal(t, "gar_refresh", result.RefreshToken)
	assert.Equal(t, "2031-01-01T00:00:00Z", result.RefreshExpiresAt)
	assert.Equal(t, "https://mystack.grafana.net", result.InstanceEndpoint)

	out := writer.String()
	assert.Contains(t, out, "callback_port="+strconv.Itoa(auth.ManualCallbackPort))
	assert.NotContains(t, out, "Waiting for authentication")
	assert.Contains(t, out, "single-use code")
}

func TestFlowRun_ManualRejectsForeignState(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newExchangeServer(t, &calls)

	var writer bytes.Buffer
	values := url.Values{}
	values.Set("code", "auth-code")
	values.Set("state", "state-from-another-attempt")
	values.Set("endpoint", server.URL)
	values.Set("instanceEndpoint", "https://mystack.grafana.net")

	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
		Manual: true,
		Writer: &writer,
		Reader: strings.NewReader("http://127.0.0.1:54321/callback?" + values.Encode() + "\n"),
	})

	_, err := flow.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different login attempt")
	assert.Equal(t, int32(0), calls.Load(), "the exchange must not run on a state mismatch")

	// A state mismatch is the failure that leaves the most on screen: the code
	// stays in the scrollback, and the user runs the command again. The notice
	// must therefore cover the failure paths, not the success path alone.
	assert.Contains(t, writer.String(), "single-use code")
}

func TestFlowRun_ManualDoesNotBindAPort(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(auth.ManualCallbackPort))
	if err != nil {
		t.Skipf("port %d is not available for this test: %v", auth.ManualCallbackPort, err)
	}
	defer func() { _ = listener.Close() }()

	var calls atomic.Int32
	server := newExchangeServer(t, &calls)

	var writer bytes.Buffer
	reader := &lazyReader{build: func() io.Reader {
		values := url.Values{}
		values.Set("code", "auth-code")
		values.Set("state", stateFromWriter(t, &writer))
		values.Set("endpoint", server.URL)
		values.Set("instanceEndpoint", "https://mystack.grafana.net")
		return strings.NewReader("http://127.0.0.1:54321/callback?" + values.Encode() + "\n")
	}}

	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
		Manual: true,
		Writer: &writer,
		Reader: reader,
	})

	result, err := flow.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "gat_token", result.Token)
}

func TestFlowRun_ManualRejectsFixedPort(t *testing.T) {
	t.Parallel()

	var writer bytes.Buffer
	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
		Manual: true,
		Port:   1234,
		Writer: &writer,
		Reader: strings.NewReader("\n"),
	})

	_, err := flow.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manual OAuth does not use a callback port")
	assert.Zero(t, writer.Len(), "no instructions before the option check")
}

func TestFlowRun_ManualHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	var writer bytes.Buffer
	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
		Manual: true,
		Writer: &writer,
		Reader: pr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := flow.Run(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestFlowRun_ManualErrorsDoNotEchoPastedURL(t *testing.T) {
	t.Parallel()

	const secret = "SECRETCODE"

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "state mismatch",
			input: "http://127.0.0.1:54321/callback?code=" + secret + "&state=other\n",
		},
		{
			name:  "parse failure",
			input: "not-a-url-but-holds-" + secret + "\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var writer bytes.Buffer
			flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
				Manual: true,
				Writer: &writer,
				Reader: strings.NewReader(tc.input),
			})

			_, err := flow.Run(context.Background())
			require.Error(t, err)
			assert.NotContains(t, err.Error(), secret)
		})
	}
}

func TestFlowRun_ManualReportsMissingInput(t *testing.T) {
	t.Parallel()

	var writer bytes.Buffer
	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
		Manual: true,
		Writer: &writer,
		Reader: strings.NewReader(""),
	})

	_, err := flow.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no input received")
}

func TestGCOMFlowRun_ManualSendsMatchingRedirectURI(t *testing.T) {
	t.Parallel()

	var gotRedirectURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/oauth2/token", r.URL.Path)
		var body map[string]string
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotRedirectURI = body["redirect_uri"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"gcom-token","scope":"stacks:read","expires_in":3600}`))
	}))
	defer server.Close()

	var writer bytes.Buffer
	reader := &lazyReader{build: func() io.Reader {
		values := url.Values{}
		values.Set("code", "auth-code")
		values.Set("state", stateFromWriter(t, &writer))
		return strings.NewReader("http://127.0.0.1:54321/callback?" + values.Encode() + "\n")
	}}

	flow := auth.NewGCOMFlow(auth.GCOMOptions{
		ClientID: "gcx",
		GCOMURL:  server.URL,
		Scopes:   []string{"stacks:read"},
		Manual:   true,
		Writer:   &writer,
		Reader:   reader,
	})

	result, err := flow.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "gcom-token", result.AccessToken)

	wantRedirectURI := "http://127.0.0.1:" + strconv.Itoa(auth.ManualCallbackPort) + "/callback"
	assert.Equal(t, wantRedirectURI, gotRedirectURI)
	assert.Contains(t, writer.String(), "redirect_uri="+url.QueryEscape(wantRedirectURI))
}

func TestReadLineDoesNotOverRead(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("first\nsecond")

	line, err := auth.ReadLine(reader)
	require.NoError(t, err)
	assert.Equal(t, "first", line)

	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "second", string(rest))
}

func TestPrintRemoteSessionHint(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantOut bool
	}{
		{
			name:    "local session",
			env:     map[string]string{},
			wantOut: false,
		},
		{
			name:    "ssh session",
			env:     map[string]string{"SSH_CONNECTION": "10.0.0.1 51234 10.0.0.2 22"},
			wantOut: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
				t.Setenv(name, "")
			}
			for name, value := range tc.env {
				t.Setenv(name, value)
			}

			var writer bytes.Buffer
			auth.PrintRemoteSessionHint(&writer, 54321, "gcx login --oauth-manual")

			if !tc.wantOut {
				assert.Zero(t, writer.Len())
				return
			}
			out := writer.String()
			assert.Contains(t, out, "ssh -L 54321:127.0.0.1:54321")
			assert.Contains(t, out, "gcx login --oauth-manual")
		})
	}
}

// TestStartPasteWatcherRequiresRemoteSession pins the gate on the paste race:
// it must not take over the terminal for a local login or in agent mode.
func TestStartPasteWatcherRequiresRemoteSession(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantStart bool
	}{
		{
			name:      "local session",
			env:       map[string]string{},
			wantStart: false,
		},
		{
			name:      "agent mode",
			env:       map[string]string{"SSH_CONNECTION": "10.0.0.1 1 10.0.0.2 22", "GCX_AGENT_MODE": "1"},
			wantStart: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
				t.Setenv(name, "")
			}
			t.Setenv("GCX_AGENT_MODE", "0")
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			agent.ResetForTesting()
			t.Cleanup(agent.ResetForTesting)

			var writer bytes.Buffer
			watcher := auth.StartPasteWatcher(&writer, 54321)
			if watcher != nil {
				defer watcher.Close()
			}
			assert.Equal(t, tc.wantStart, watcher != nil)
			if watcher == nil {
				assert.Zero(t, writer.Len(), "a watcher that does not start prints nothing")
			}
		})
	}
}

// remoteSessionForTest makes IsRemoteSession report true and agent mode false,
// which is the only state in which the paste watcher starts.
func remoteSessionForTest(t *testing.T) {
	t.Helper()
	t.Setenv("SSH_CONNECTION", "10.0.0.1 51234 10.0.0.2 22")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("GCX_AGENT_MODE", "0")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}

// TestPasteWatcherCloseReleasesTheTerminal pins the property that makes the
// paste race safe: Close must unblock the pending read and wait for the reader
// goroutine. A stale reader would compete with the prompts that run after
// login.
func TestPasteWatcherCloseReleasesTheTerminal(t *testing.T) {
	remoteSessionForTest(t)

	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	restore := auth.SwapPasteTerminal(reader, true)
	t.Cleanup(restore)

	var out bytes.Buffer
	watcher := auth.StartPasteWatcher(&out, 54321)
	require.NotNil(t, watcher)
	assert.Contains(t, out.String(), "-L 54321:127.0.0.1:54321")
	assert.Contains(t, out.String(), "Redirect URL")

	// Close must return, which it only can once the reader goroutine ended.
	done := make(chan struct{})
	go func() {
		watcher.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not release the terminal reader")
	}
}

// TestPasteWatcherDeliversAndRejects covers both watcher outcomes: a usable
// redirect URL reaches the caller, and an unusable one re-prompts instead of
// ending the flow, because the callback server is still listening.
func TestPasteWatcherDeliversAndRejects(t *testing.T) {
	remoteSessionForTest(t)

	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	restore := auth.SwapPasteTerminal(reader, true)
	t.Cleanup(restore)

	var out bytes.Buffer
	watcher := auth.StartPasteWatcher(&out, 54321)
	require.NotNil(t, watcher)
	t.Cleanup(watcher.Close)

	// An unusable line reaches the caller as an error, not as parameters. The
	// caller then re-prompts, because the callback server is still listening.
	_, err = writerEnd.WriteString("not-a-url\n")
	require.NoError(t, err)
	select {
	case pasted := <-watcher.Input():
		require.Error(t, pasted.Err)
		assert.Nil(t, pasted.Values)
		watcher.Reject(pasted.Err)
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not report the unusable line")
	}
	assert.Contains(t, out.String(), "That URL did not work")
	assert.Contains(t, out.String(), "Redirect URL")

	// A usable line reaches the caller.
	_, err = writerEnd.WriteString("http://127.0.0.1:54321/callback?code=c1&state=s1\n")
	require.NoError(t, err)
	select {
	case pasted := <-watcher.Input():
		require.NoError(t, pasted.Err)
		assert.Equal(t, "c1", pasted.Values.Get("code"))
		assert.Equal(t, "s1", pasted.Values.Get("state"))
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not deliver the pasted URL")
	}
}

// TestPasteWatcherKeepsReadingAfterCallerRejection is the regression guard for
// a reader that stopped after the first delivery.
//
// The watcher only parses. The caller runs the semantic checks (state, code,
// token exchange) and calls Reject when they fail, which re-prompts. A watcher
// that returned after the first delivery left that prompt with no reader: the
// second pasted line stayed in the terminal buffer, and the shell read it once
// gcx exited, which wrote the authorization code into the shell history.
func TestPasteWatcherKeepsReadingAfterCallerRejection(t *testing.T) {
	remoteSessionForTest(t)

	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	restore := auth.SwapPasteTerminal(reader, true)
	t.Cleanup(restore)

	var out bytes.Buffer
	watcher := auth.StartPasteWatcher(&out, 54321)
	require.NotNil(t, watcher)
	t.Cleanup(watcher.Close)

	// The first URL parses and reaches the caller.
	_, err = writerEnd.WriteString("http://127.0.0.1:54321/callback?code=c1&state=stale\n")
	require.NoError(t, err)
	select {
	case pasted := <-watcher.Input():
		require.NoError(t, pasted.Err)
		assert.Equal(t, "stale", pasted.Values.Get("state"))
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not deliver the first pasted URL")
	}

	// The caller rejects it, exactly as a state mismatch does in the flow.
	watcher.Reject(errors.New("the pasted URL belongs to a different login attempt"))

	// The re-prompt must have a reader, so a second paste must also arrive.
	_, err = writerEnd.WriteString("http://127.0.0.1:54321/callback?code=c2&state=fresh\n")
	require.NoError(t, err)
	select {
	case pasted := <-watcher.Input():
		require.NoError(t, pasted.Err)
		assert.Equal(t, "c2", pasted.Values.Get("code"))
		assert.Equal(t, "fresh", pasted.Values.Get("state"))
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher stopped reading after the caller rejected the first URL")
	}
}

// TestPasteWatcherDropsTheEmptyLine covers the Enter that step 1 of the
// instructions asks for. The user presses Enter, then presses ~C, to add a port
// forward to the running SSH session. A watcher that parsed that empty line
// answered "That URL did not work: no URL supplied" on the route that the
// instructions recommend first.
func TestPasteWatcherDropsTheEmptyLine(t *testing.T) {
	remoteSessionForTest(t)

	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	restore := auth.SwapPasteTerminal(reader, true)
	t.Cleanup(restore)

	var out bytes.Buffer
	watcher := auth.StartPasteWatcher(&out, 54321)
	require.NotNil(t, watcher)
	t.Cleanup(watcher.Close)

	// A bare Enter, and a line of spaces, must both reach the caller as nothing.
	_, err = writerEnd.WriteString("\n   \n")
	require.NoError(t, err)

	// The next real URL must still arrive. That both proves the reader survived
	// the empty lines and gives the empty lines time to reach the watcher.
	_, err = writerEnd.WriteString("http://127.0.0.1:54321/callback?code=c1&state=s1\n")
	require.NoError(t, err)
	select {
	case pasted := <-watcher.Input():
		require.NoError(t, pasted.Err)
		assert.False(t, pasted.Closed)
		assert.Equal(t, "c1", pasted.Values.Get("code"))
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not deliver the pasted URL after the empty lines")
	}
	assert.NotContains(t, out.String(), "That URL did not work")
}

// TestPasteWatcherReportsThatTheReaderEnded covers Ctrl-D and a terminal that
// reports an error. A watcher that returned without a word left the prompt
// "Redirect URL (or wait for the browser):" on screen with no reader behind it.
// The caller must learn that the paste route ended, so it can say so and keep
// waiting for the callback.
func TestPasteWatcherReportsThatTheReaderEnded(t *testing.T) {
	remoteSessionForTest(t)

	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	restore := auth.SwapPasteTerminal(reader, true)
	t.Cleanup(restore)

	var out bytes.Buffer
	watcher := auth.StartPasteWatcher(&out, 54321)
	require.NotNil(t, watcher)
	t.Cleanup(watcher.Close)

	// Closing the write end is the pipe equivalent of Ctrl-D on a terminal.
	require.NoError(t, writerEnd.Close())

	select {
	case pasted := <-watcher.Input():
		assert.True(t, pasted.Closed, "the watcher must report that the reader ended")
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher ended without a word: the prompt keeps no reader")
	}

	// The reader must not spin on the permanent read error. No second report
	// may arrive.
	select {
	case pasted := <-watcher.Input():
		t.Fatalf("the watcher kept reading after the terminal ended: %+v", pasted)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestPasteWatcherRejectionDoesNotEchoTheURL keeps the no-echo rule on the
// re-prompt path: the pasted line holds a single-use authorization code.
func TestPasteWatcherRejectionDoesNotEchoTheURL(t *testing.T) {
	remoteSessionForTest(t)

	const secret = "SECRETCODE"

	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	restore := auth.SwapPasteTerminal(reader, true)
	t.Cleanup(restore)

	var out bytes.Buffer
	watcher := auth.StartPasteWatcher(&out, 54321)
	require.NotNil(t, watcher)
	t.Cleanup(watcher.Close)

	_, err = writerEnd.WriteString("http://127.0.0.1:54321/callback-" + secret + "\n")
	require.NoError(t, err)
	select {
	case pasted := <-watcher.Input():
		require.Error(t, pasted.Err)
		assert.NotContains(t, pasted.Err.Error(), secret)
		watcher.Reject(pasted.Err)
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not report the unusable line")
	}
	assert.Contains(t, out.String(), "That URL did not work")
	assert.NotContains(t, out.String(), secret)
}

// TestExchangeGuardGrantsOneClaim covers the guard that stops the second token
// exchange. The callback server and the paste reader accept the same
// single-use code, so only one of them may exchange it.
func TestExchangeGuardGrantsOneClaim(t *testing.T) {
	t.Parallel()

	guard := &auth.ExchangeGuard{}
	assert.True(t, auth.ClaimExchange(guard), "the first route must get the claim")
	assert.False(t, auth.ClaimExchange(guard), "the second route must not get the claim")

	// The manual flow runs no callback server, so it passes a nil guard.
	assert.True(t, auth.ClaimExchange(nil), "a nil guard must always grant the claim")
}

// TestTakenGuardStopsTheSecondExchange proves that the loser of the race never
// reaches the token exchange. Before the guard, the loser ran the exchange,
// received "token exchange failed" for a code that the winner had already
// used, and reported that failure to the user one moment before the success.
func TestTakenGuardStopsTheSecondExchange(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newExchangeServer(t, &calls)

	values := url.Values{}
	values.Set("code", "auth-code")
	values.Set("state", "the-state")
	values.Set("endpoint", server.URL)
	values.Set("instanceEndpoint", "https://mystack.grafana.net")

	guard := &auth.ExchangeGuard{}

	// The winner exchanges the code.
	require.NoError(t, auth.HandleCallbackParams(context.Background(), values, "the-state", "verifier", guard))
	assert.Equal(t, int32(1), calls.Load())

	// The loser must stop before the exchange, and it must report the sentinel
	// so the caller stays silent.
	err := auth.HandleCallbackParams(context.Background(), values, "the-state", "verifier", guard)
	require.ErrorIs(t, err, auth.ErrExchangeClaimed)
	assert.Equal(t, int32(1), calls.Load(), "the loser must not run a second exchange")
}

// TestGuardClaimFollowsTheStateCheck pins the order of the two checks. A URL
// from an older attempt fails the state check, so it must leave the claim for
// the real callback. A guard that the wrong URL consumed would block the login.
func TestGuardClaimFollowsTheStateCheck(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newExchangeServer(t, &calls)

	stale := url.Values{}
	stale.Set("code", "old-code")
	stale.Set("state", "state-from-another-attempt")
	stale.Set("endpoint", server.URL)
	stale.Set("instanceEndpoint", "https://mystack.grafana.net")

	guard := &auth.ExchangeGuard{}
	err := auth.HandleCallbackParams(context.Background(), stale, "the-state", "verifier", guard)
	require.ErrorIs(t, err, auth.ErrStateMismatch)

	// The claim must still be free for the real callback.
	assert.True(t, auth.ClaimExchange(guard), "a state mismatch must not consume the claim")
}

// TestFlushTerminalInputHandlesANonTerminal covers the flush that Close runs.
// The pipe in these tests is not a terminal, so the flush reports an error. It
// must not panic, because Close ignores that error.
func TestFlushTerminalInputHandlesANonTerminal(t *testing.T) {
	t.Parallel()

	reader, writerEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerEnd.Close() })
	t.Cleanup(func() { _ = reader.Close() })

	assert.NotPanics(t, func() { _ = auth.FlushTerminalInput(reader) })
}

// TestFlushTerminalInputClearsTheQueue runs the flush on a real controlling
// terminal. The flush discards what the user typed without a newline, which the
// shell would otherwise read after gcx exits.
func TestFlushTerminalInputClearsTheQueue(t *testing.T) {
	tty, ok := auth.OpenPasteTerminal()
	if !ok {
		t.Skip("no controlling terminal available")
	}
	t.Cleanup(func() { _ = tty.Close() })

	require.NoError(t, auth.FlushTerminalInput(tty))
}

// TestOpenPasteTerminalReadIsCancellable is the regression guard for the trap
// that makes the paste race hang: File.Fd puts the descriptor back into
// blocking mode, the read then blocks inside the syscall instead of the
// poller, and Close can no longer stop it. The login hangs forever after the
// browser callback arrives.
//
// The test needs a controlling terminal, so it skips where there is none.
func TestOpenPasteTerminalReadIsCancellable(t *testing.T) {
	tty, ok := auth.OpenPasteTerminal()
	if !ok {
		t.Skip("no controlling terminal available")
	}

	read := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := tty.Read(buf)
		read <- err
	}()

	// Give the goroutine time to enter the read before closing.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, tty.Close())

	select {
	case err := <-read:
		require.ErrorIs(t, err, os.ErrClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not unblock the read: the descriptor is in blocking mode, " +
			"which makes the paste race hang after the browser callback arrives")
	}
}
