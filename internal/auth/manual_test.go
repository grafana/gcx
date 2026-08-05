package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
