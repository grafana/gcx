package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Destination is a host the guest may send HTTPS requests to.
type Destination struct {
	// Host is matched exactly (case-insensitively) against the request's
	// host, e.g. "mystack.grafana.net" or "localhost:8443". A missing port
	// means the scheme's default (443, or 80 for http).
	Host string
	// AllowHTTP also permits plain http to Host, for local development.
	// Header values are then sent unencrypted.
	AllowHTTP bool
	// Header is set on every request to Host, replacing any values the
	// guest sent, e.g. {"Authorization": {"Bearer glsa_..."}}. Redirects are
	// checked hop by hop, so these never follow a redirect to another host.
	Header http.Header
}

// match returns the destination allowing u, or an error explaining why none does.
func match(egress []Destination, u *url.URL) (*Destination, error) {
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("egress denied: %s://%s: only http and https are supported", u.Scheme, u.Host)
	}
	host := canonicalHost(u.Host, u.Scheme)
	httpsOnly := false
	for i := range egress {
		if canonicalHost(egress[i].Host, u.Scheme) != host {
			continue
		}
		if u.Scheme == "http" && !egress[i].AllowHTTP {
			httpsOnly = true // a later entry for the same host may allow http
			continue
		}
		return &egress[i], nil
	}
	if httpsOnly {
		return nil, fmt.Errorf("egress denied: http://%s: only https is allowed", u.Host)
	}
	return nil, fmt.Errorf("egress denied: %s is not an allowed destination", u.Host)
}

// canonicalHost lower-cases h and drops the scheme's default port.
func canonicalHost(h, scheme string) string {
	h = strings.ToLower(h)
	if scheme == "http" {
		return strings.TrimSuffix(h, ":80")
	}
	return strings.TrimSuffix(h, ":443")
}

// The "gcx_http" host module. The ABI is documented in gcx's
// internal/httputils/wire_wasip1.go. The host never calls into the guest;
// each request runs in its own goroutine and the guest polls for the result.
//
// Host functions find their invocation's session through the context that
// wazero passes them, so concurrent Runs never see each other's requests.

type sessionKey struct{}

func withSession(ctx context.Context, s *session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

func sessionFrom(ctx context.Context) *session {
	s, ok := ctx.Value(sessionKey{}).(*session)
	if !ok {
		panic("gcx_http: host function called outside Run")
	}
	return s
}

type session struct {
	egress    []Destination
	authorize func(*http.Request) error // optional
	transport http.RoundTripper

	mu     sync.Mutex
	nextID uint32
	byID   map[uint32]*pendingRequest
}

type pendingRequest struct {
	cancel context.CancelFunc
	done   chan struct{}
	resp   []byte // raw HTTP/1.1 response, set when done
	err    string // set instead of resp on failure; never empty
}

func newSession(egress []Destination, authorize func(*http.Request) error, transport http.RoundTripper) *session {
	return &session{egress: egress, authorize: authorize, transport: transport, byID: map[uint32]*pendingRequest{}}
}

func instantiateHTTP(ctx context.Context, rt wazero.Runtime) error {
	_, err := rt.NewHostModuleBuilder("gcx_http").
		NewFunctionBuilder().WithFunc(hostStart).Export("start").
		NewFunctionBuilder().WithFunc(hostPoll).Export("poll").
		NewFunctionBuilder().WithFunc(hostTake).Export("take").
		NewFunctionBuilder().WithFunc(hostCancel).Export("cancel").
		Instantiate(ctx)
	return err
}

func hostStart(ctx context.Context, m api.Module, ptr, n uint32) uint32 {
	s := sessionFrom(ctx)
	raw, ok := m.Memory().Read(ptr, n)
	if !ok {
		panic("gcx_http.start: request out of bounds")
	}
	raw = bytes.Clone(raw) // Read aliases guest memory

	// ctx is the Run call's context, which Run cancels when it returns, so
	// requests still in flight then are abandoned.
	reqCtx, cancel := context.WithCancel(ctx)
	p := &pendingRequest{cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.byID[id] = p
	s.mu.Unlock()

	go func() {
		defer close(p.done)
		p.resp, p.err = s.roundTrip(reqCtx, raw)
	}()
	return id
}

func hostPoll(ctx context.Context, id uint32) int64 {
	p := sessionFrom(ctx).get(id)
	select {
	case <-p.done:
	default:
		return 0
	}
	if p.err != "" {
		return -int64(len(p.err))
	}
	return int64(len(p.resp))
}

func hostTake(ctx context.Context, m api.Module, id, ptr uint32) {
	s := sessionFrom(ctx)
	p := s.get(id)
	<-p.done
	out := p.resp
	if p.err != "" {
		out = []byte(p.err)
	}
	if !m.Memory().Write(ptr, out) {
		panic("gcx_http.take: buffer out of bounds")
	}
	s.forget(id)
}

func hostCancel(ctx context.Context, id uint32) {
	sessionFrom(ctx).forget(id)
}

func (s *session) get(id uint32) *pendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(id)
}

func (s *session) forget(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookup(id).cancel()
	delete(s.byID, id)
}

// lookup requires s.mu. An unknown id is a guest bug; panicking traps the guest.
func (s *session) lookup(id uint32) *pendingRequest {
	p, ok := s.byID[id]
	if !ok {
		panic("gcx_http: unknown request id")
	}
	return p
}

// roundTrip applies the egress policy and authorizer to a raw HTTP/1.1 request, performs
// it, and returns the raw response, buffered in full, or a non-empty error.
func (s *session) roundTrip(ctx context.Context, raw []byte) ([]byte, string) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return nil, "gcx_http: parse request: " + err.Error()
	}
	req.RequestURI = "" // must be empty on client requests
	dest, err := match(s.egress, req.URL)
	if err != nil {
		return nil, "gcx_http: " + err.Error()
	}
	if s.authorize != nil {
		body, err := io.ReadAll(req.Body) // in memory already: raw holds it
		if err != nil {
			return nil, "gcx_http: read request body: " + err.Error()
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		if err := s.authorize(req); err != nil {
			return nil, "gcx_http: request refused: " + err.Error()
		}
		req.Body = io.NopCloser(bytes.NewReader(body)) // in case authorize read it
	}
	for k, vs := range dest.Header {
		req.Header[http.CanonicalHeaderKey(k)] = vs
	}
	req.Host = "" // send the URL's host, which is what egress matched
	resp, err := s.transport.RoundTrip(req.WithContext(ctx))
	if err != nil {
		return nil, "gcx_http: " + err.Error() // never empty, as poll requires
	}
	defer resp.Body.Close()
	var b bytes.Buffer
	if err := resp.Write(&b); err != nil {
		return nil, "gcx_http: read response: " + err.Error()
	}
	return b.Bytes(), ""
}
