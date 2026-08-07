package auth_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (g *gcomServer) redirectURISent() (string, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.exchangeRedirectURI, g.exchanges
}

// freePort returns a port that was free a moment ago. Used only where the test
// needs to know the port in advance; every other test lets the flow auto-pick.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // no context plumbing needed for a probe listener
	require.NoError(t, err)
	addr, ok := l.Addr().(*net.TCPAddr)
	require.True(t, ok, "expected *net.TCPAddr from listener")
	require.NoError(t, l.Close())
	return addr.Port
}

// #1148: the Cloud leg ignored the port the user fixed for the login and
// auto-picked from 54321-54399 instead, which is unreachable when only one
// forwarded port exists between the remote host and the browser.
func TestGCOMFlowRun_HonoursFixedCallbackPort(t *testing.T) {
	testutils.SetAgentMode(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)
	gcom := newGCOMServer(t, nil)
	writer := &syncWriter{}
	flow := auth.NewGCOMFlow(auth.GCOMOptions{
		ClientID: "gcx",
		GCOMURL:  gcom.URL,
		Scopes:   []string{"stacks:read"},
		Port:     port,
		Writer:   writer,
	})

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
	authorizeRedirectURI := mustSubmatch(t, redirectURIRe, out, "redirect URI")

	wantRedirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	assert.Equal(t, wantRedirectURI, authorizeRedirectURI, "the Cloud leg must listen on the port the user fixed")

	require.Equal(t, http.StatusOK,
		get(t, ctx, authorizeRedirectURI+"?"+url.Values{"state": {state}, "code": {"cloud-code"}}.Encode()).status)

	select {
	case outcome := <-results:
		results <- outcome
		require.NoError(t, outcome.err)
	case <-time.After(10 * time.Second):
		t.Fatal("the Cloud leg did not complete")
	}

	// The exchange must repeat the authorize request's redirect_uri exactly.
	sent, count := gcom.redirectURISent()
	assert.Equal(t, 1, count)
	assert.Equal(t, authorizeRedirectURI, sent, "authorize and token exchange must send an identical redirect_uri")
	assert.Equal(t, wantRedirectURI, sent)
}

// Auto-pick stays the default: a login with no port flag is unchanged.
func TestGCOMFlowRun_AutoPicksPortWhenUnset(t *testing.T) {
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

	require.True(t, strings.HasPrefix(redirectURI, "http://127.0.0.1:"), "got %q", redirectURI)
	port := strings.TrimSuffix(strings.TrimPrefix(redirectURI, "http://127.0.0.1:"), "/callback")
	assert.NotEqual(t, "0", port, "an unset port must resolve to a real bound port")

	require.Equal(t, http.StatusOK,
		get(t, ctx, redirectURI+"?"+url.Values{"state": {state}, "code": {"cloud-code"}}.Encode()).status)

	select {
	case outcome := <-results:
		results <- outcome
		require.NoError(t, outcome.err)
	case <-time.After(10 * time.Second):
		t.Fatal("the Cloud leg did not complete")
	}

	sent, _ := gcom.redirectURISent()
	assert.Equal(t, redirectURI, sent, "authorize and token exchange must send an identical redirect_uri")
}

func TestGCOMFlowRun_FailsBeforeBrowserOutputWhenFixedPortUnavailable(t *testing.T) {
	testutils.SetAgentMode(t, true)

	// Hold the port so the bind is guaranteed to fail.
	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // no context plumbing needed for a reserved listener
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "expected *net.TCPAddr from listener")
	port := addr.Port

	writer := &syncWriter{}
	flow := auth.NewGCOMFlow(auth.GCOMOptions{ClientID: "gcx", GCOMURL: "https://grafana.com", Port: port, Writer: writer})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = flow.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("callback port %d unavailable", port))
	assert.NotContains(t, err.Error(), "no available port", "a fixed port that is taken is not port-range exhaustion")
	assert.Empty(t, writer.String(), "no browser instructions before the bind fails")
}

func TestGCOMFlowRun_RejectsInvalidPortBeforeAnySideEffect(t *testing.T) {
	testutils.SetAgentMode(t, true)

	for _, port := range []int{-1, 65536, 99999} {
		t.Run(strconv.Itoa(port), func(t *testing.T) {
			writer := &syncWriter{}
			flow := auth.NewGCOMFlow(auth.GCOMOptions{ClientID: "gcx", GCOMURL: "https://grafana.com", Port: port, Writer: writer})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := flow.Run(ctx)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "callback port")
			assert.Empty(t, writer.String(), "no browser instructions before validation fails")
		})
	}
}

// Manual mode starts no listener, so a fixed port would be silently ignored.
// The stack flow already rejects the pair; the Cloud flow must agree.
func TestGCOMFlowRun_RejectsManualWithFixedPort(t *testing.T) {
	testutils.SetAgentMode(t, true)

	writer := &syncWriter{}
	flow := auth.NewGCOMFlow(auth.GCOMOptions{
		ClientID: "gcx",
		GCOMURL:  "https://grafana.com",
		Port:     54321,
		Manual:   true,
		Reader:   strings.NewReader(""),
		Writer:   writer,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := flow.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manual OAuth does not use a callback port")
	assert.Empty(t, writer.String())
}

func TestValidateCallbackPort(t *testing.T) {
	tests := []struct {
		port    int
		wantErr bool
	}{
		{0, false}, // auto-pick
		{1, false},
		{8250, false},
		{65535, false},
		{-1, true},
		{65536, true},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.port), func(t *testing.T) {
			err := auth.ValidateCallbackPort(tt.port)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// Manual mode is deliberately unbounded: readLineContext cannot cancel the
// terminal read it wraps, so a deadline there would return while an abandoned
// goroutine still owned the terminal and the user's half-typed redirect URL —
// carrying a live authorization code — would land in the shell.
func TestManualFlowRun_HasNoReadDeadline(t *testing.T) {
	testutils.SetAgentMode(t, true)

	blocked, writeEnd := io.Pipe()
	t.Cleanup(func() { _ = writeEnd.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writer := &syncWriter{}
	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{Manual: true, Reader: blocked, Writer: writer})
	auth.SetAcquireTimeout(flow, 50*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := flow.Run(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("manual mode must not time out on its own read; got %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	// Only the caller's context ends it.
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling the context must end manual mode")
	}
}

// The Cloud leg's manual mode must be unbounded for the same reason as the
// stack leg's. Reverting only one of the two used to leave no test failing.
func TestGCOMManualFlowRun_HasNoReadDeadline(t *testing.T) {
	testutils.SetAgentMode(t, true)

	blocked, writeEnd := io.Pipe()
	t.Cleanup(func() { _ = writeEnd.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writer := &syncWriter{}
	flow := auth.NewGCOMFlow(auth.GCOMOptions{
		ClientID: "gcx", GCOMURL: "https://grafana.com",
		Manual: true, Reader: blocked, Writer: writer,
	})
	auth.SetGCOMAcquireTimeout(flow, 50*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := flow.Run(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("manual mode must not time out on its own read; got %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling the context must end manual mode")
	}
}

// docs/reference/login.md and docs/architecture/auth-system.md both state this
// number to users. Pin it so the docs cannot silently drift from the code.
func TestDefaultCallbackAcquireTimeoutIsThirtyMinutes(t *testing.T) {
	assert.Equal(t, 30*time.Minute, auth.DefaultCallbackAcquireTimeout,
		"the wait bound is documented in login.md and auth-system.md; update both if you change it")
}
