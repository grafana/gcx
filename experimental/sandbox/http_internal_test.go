package sandbox

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	egress := []Destination{{Host: "Stack.grafana.net"}, {Host: "localhost:8443"}, {Host: "grafana:3000", AllowHTTP: true}, {Host: "devgrafana", AllowHTTP: true}, {Host: "dup.example"}, {Host: "dup.example", AllowHTTP: true}}
	for _, tc := range []struct {
		url   string
		allow bool
	}{
		{"https://stack.grafana.net/api", true},
		{"https://stack.grafana.net:443/api", true},
		{"https://STACK.grafana.net/api", true},
		{"https://localhost:8443/x", true},
		{"https://stack.grafana.net:8443/api", false},
		{"https://other.grafana.net/api", false},
		{"https://evil.com/?stack.grafana.net", false},
		{"https://stack.grafana.net.evil.com/api", false},
		{"http://stack.grafana.net/api", false}, // https only
		{"https://localhost/x", false},
		// AllowHTTP opts a destination into plain http; the default port is 80.
		{"http://grafana:3000/api", true},
		{"https://grafana:3000/api", true},
		{"http://devgrafana/api", true},
		{"http://devgrafana:80/api", true},
		{"http://devgrafana:443/api", false},
		{"http://localhost:8443/x", false},
		{"ftp://stack.grafana.net/x", false},
		// Order doesn't matter: a later entry for the same host can allow http.
		{"http://dup.example/x", true},
	} {
		u, _ := url.Parse(tc.url)
		_, err := match(egress, u)
		if (err == nil) != tc.allow {
			t.Errorf("%s: allowed=%v, want %v (err %v)", tc.url, err == nil, tc.allow, err)
		}
	}
}

// wire describes req as the guest's transport does.
func wire(t *testing.T, req *http.Request) wireRequest {
	t.Helper()
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	return wireRequest{
		method:        req.Method,
		scheme:        req.URL.Scheme,
		authority:     cmp.Or(req.Host, req.URL.Host),
		pathWithQuery: req.URL.RequestURI(),
		header:        req.Header,
		body:          body,
	}
}

func TestRoundTripPolicy(t *testing.T) {
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append([]byte("echo:"), body...))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	var authorized []string
	authorize := func(r *http.Request) error {
		_, _ = io.ReadAll(r.Body) // a policy may inspect the body; the server must still get it
		authorized = append(authorized, r.Method+" "+r.URL.Path+" auth="+r.Header.Get("Authorization"))
		if r.Method == http.MethodDelete {
			return errors.New("DELETE needs write access")
		}
		return nil
	}
	s := newSession([]Destination{{
		Host:   host,
		Header: http.Header{"authorization": {"Bearer host-secret"}},
	}}, authorize, srv.Client().Transport)

	// sendMethod returns the response body, or the error the guest would see.
	sendMethod := func(method, rawURL, hostHeader string) (string, *callError) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), method, rawURL, strings.NewReader("hi"))
		req.Header.Set("Authorization", "Bearer guest-supplied")
		if hostHeader != "" {
			req.Host = hostHeader
		}
		resp, cerr := s.roundTrip(context.Background(), wire(t, req))
		if cerr != nil {
			return "", cerr
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body), nil
	}
	send := func(rawURL, hostHeader string) (string, *callError) {
		t.Helper()
		return sendMethod(http.MethodPost, rawURL, hostHeader)
	}

	body, cerr := send(srv.URL+"/api/x", "")
	if cerr != nil {
		t.Fatal(cerr.detail)
	}
	if body != "echo:hi" {
		t.Errorf("body %q", body)
	}
	if gotAuth != "Bearer host-secret" {
		t.Errorf("server saw Authorization %q, want the host's", gotAuth)
	}

	for _, tc := range []struct{ url, hostHeader string }{
		{"https://other.example/x", ""},
		{"http://" + host + "/x", ""},
		// The guest sends req.Host as the authority when set, so overriding
		// Host redirects the request, and egress judges that.
		{srv.URL + "/x", "other.example"},
	} {
		if _, cerr := send(tc.url, tc.hostHeader); cerr == nil || cerr.code != codeHTTPRequestDenied || !strings.Contains(cerr.detail, "egress denied") {
			t.Errorf("%+v: got %+v, want egress denied", tc, cerr)
		}
	}
	// The authorizer sees each allowed request before credentials are added,
	// and its refusal stops the request.
	if got, want := authorized[0], "POST /api/x auth=Bearer guest-supplied"; got != want {
		t.Errorf("authorizer saw %q, want %q", got, want)
	}
	gotAuth = ""
	if _, cerr := sendMethod(http.MethodDelete, srv.URL+"/api/x", ""); cerr == nil || cerr.code != codeHTTPRequestDenied || cerr.detail != "request refused: DELETE needs write access" {
		t.Errorf("DELETE: got %+v, want refusal", cerr)
	}
	if gotAuth != "" {
		t.Error("refused request reached the server")
	}

	// An authority that could carry another host, or a malformed request, is rejected.
	for _, tc := range []struct {
		w    wireRequest
		code uint32
	}{
		{wireRequest{method: "GET", scheme: "https", authority: "evil.example/@" + host, pathWithQuery: "/"}, codeHTTPRequestURIInvalid},
		{wireRequest{method: "GET", scheme: "https", authority: "user@" + host, pathWithQuery: "/"}, codeHTTPRequestURIInvalid},
		{wireRequest{method: "GET", scheme: "https", authority: host, pathWithQuery: "x"}, codeHTTPRequestURIInvalid},
		{wireRequest{method: "GET", scheme: "https", authority: "", pathWithQuery: "/"}, codeHTTPRequestURIInvalid},
		{wireRequest{method: "BAD METHOD", scheme: "https", authority: host, pathWithQuery: "/"}, codeHTTPRequestMethodInvalid},
	} {
		resp, cerr := s.roundTrip(context.Background(), tc.w)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if cerr == nil || cerr.code != tc.code {
			t.Errorf("%+v: got %+v, want code %d", tc.w, cerr, tc.code)
		}
	}
}

// failingReader returns its data, then err.
type failingReader struct {
	r   io.Reader
	err error
}

func (f failingReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, f.err
	}
	return n, err
}

// drain reads x's body the way the guest does, 1000 bytes at a time.
func drain(x *exchange) (string, int32, int) {
	var out strings.Builder
	reads := 0
	for {
		b, n := x.readBody(1000)
		switch {
		case n > 0:
			out.Write(b)
			reads++
		case n < 0:
			return out.String(), n, reads
		}
	}
}

func TestBodyStream(t *testing.T) {
	big := strings.Repeat("0123456789", 10_000) // 100 KB, several chunks

	x := &exchange{chunks: make(chan []byte, 1)}
	if _, n := x.readBody(10); n != 0 {
		t.Errorf("before any data: %d, want 0 (pending)", n)
	}
	go x.pump(t.Context(), &http.Response{Body: io.NopCloser(strings.NewReader(big))})
	got, end, reads := drain(x)
	if got != big || end != -1 {
		t.Errorf("read %d bytes ending %d, want %d bytes ending -1", len(got), end, len(big))
	}
	if reads < len(big)/1000 {
		t.Errorf("%d reads, want reads capped at 1000 bytes", reads)
	}

	x = &exchange{chunks: make(chan []byte, 1)}
	go x.pump(t.Context(), &http.Response{Body: io.NopCloser(failingReader{strings.NewReader("partial"), io.ErrUnexpectedEOF})})
	got, end, _ = drain(x)
	if got != "partial" || end != -2 {
		t.Errorf("got %q ending %d, want \"partial\" ending -2", got, end)
	}
	if err := x.err.Load(); err == nil || err.code != codeConnectionTerminated {
		t.Errorf("err %+v, want connection-terminated", err)
	}
}

// endless is a body that never ends, closing closed when closed. With block
// set, Read waits for cancel and fails like a real body whose request was
// cancelled; otherwise it always has data.
type endless struct {
	cancel <-chan struct{}
	block  bool
	closed chan struct{}
}

func (e endless) Read(p []byte) (int, error) {
	if e.block {
		<-e.cancel
		return 0, context.Canceled
	}
	return len(p), nil
}

func (e endless) Close() error { close(e.closed); return nil }

// Dropping an exchange cancels its context; the pump must then stop and
// close the body, whether it is waiting on the body or on the guest.
func TestPumpStopsOnCancel(t *testing.T) {
	for _, block := range []bool{true, false} {
		ctx, cancel := context.WithCancel(t.Context())
		body := endless{cancel: ctx.Done(), block: block, closed: make(chan struct{})}
		x := &exchange{chunks: make(chan []byte, 1)}
		done := make(chan struct{})
		go func() { x.pump(ctx, &http.Response{Body: body}); close(done) }()
		x.readBody(10) // with data, the pump now blocks sending to a full channel
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("block=%v: pump still running after cancel", block)
		}
		select {
		case <-body.closed:
		default:
			t.Errorf("block=%v: body not closed", block)
		}
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want uint32
	}{
		{&net.DNSError{IsTimeout: true}, codeDNSTimeout},
		{fmt.Errorf("dial: %w", &net.DNSError{Err: "no such host"}), codeDNSError},
		{&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, codeConnectionRefused},
		{io.ErrUnexpectedEOF, codeConnectionTerminated},
		{&tls.CertificateVerificationError{Err: errors.New("bad")}, codeTLSCertificateError},
		{tls.AlertError(40), codeTLSAlertReceived},
		{tls.RecordHeaderError{Msg: "bad"}, codeTLSProtocolError},
		{timeoutError{}, codeConnectionTimeout},
		{&net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}, codeDestinationUnavailable},
		{&net.OpError{Op: "write", Err: syscall.EPIPE}, codeConnectionTerminated},
		{errors.New("something else"), codeInternalError},
	} {
		if got := classify(tc.err, codeInternalError); got.code != tc.want || got.detail != tc.err.Error() {
			t.Errorf("%v: got %+v, want code %d", tc.err, got, tc.want)
		}
	}
}

type panickingBody struct{}

func (panickingBody) Read([]byte) (int, error) { panic("body bug") }
func (panickingBody) Close() error             { return nil }

// A panic in the embedder's code fails the request instead of the host.
func TestHostPanics(t *testing.T) {
	t.Run("in Authorize", func(t *testing.T) {
		s := newSession([]Destination{{Host: "a.example"}}, func(*http.Request) error { panic("policy bug") }, http.DefaultTransport)
		s.nextID, s.byID[1] = 1, &exchange{req: wireRequest{method: "GET", scheme: "https", authority: "a.example", pathWithQuery: "/"}}
		ctx := withSession(t.Context(), s)
		hostHandle(ctx, 1)
		for hostPoll(ctx, 1) == 0 {
			time.Sleep(time.Millisecond)
		}
		if err := s.failure(1); err.code != codeInternalError || err.detail != "host panic: policy bug" {
			t.Errorf("got %+v", err)
		}
	})
	t.Run("reading the body", func(t *testing.T) {
		x := &exchange{chunks: make(chan []byte, 1)}
		go x.pump(t.Context(), &http.Response{Body: panickingBody{}})
		if _, end, _ := drain(x); end != -2 {
			t.Fatalf("body ended with %d, want -2 (failed)", end)
		}
		if err := x.err.Load(); err == nil || err.detail != "host panic: body bug" {
			t.Errorf("got %+v", err)
		}
	})
}

func TestEncodeHeadersSkipsNUL(t *testing.T) {
	got := decodeHeaders(encodeHeaders(http.Header{"A": {"b\x00c", "d"}, "E": {"f"}}))
	if want := (http.Header{"A": {"d"}, "E": {"f"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
