package sandbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

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

// The "gcx_http" host module, shaped after wasi:http@0.3.1. The ABI is
// documented in gcx's internal/httputils/wire_wasip1.go. The host never
// calls into the guest; each request runs in its own goroutine and the guest
// polls for its response and body.
//
// Host functions find their invocation's session through the context that
// wazero passes them, so concurrent Runs never see each other's requests.

// wasi:http@0.3.1 error-code case indices that the host reports.
const (
	codeDNSTimeout               = 0
	codeDNSError                 = 1
	codeConnectionRefused        = 6
	codeConnectionTerminated     = 7
	codeConnectionTimeout        = 8
	codeTLSProtocolError         = 12
	codeTLSCertificateError      = 13
	codeTLSAlertReceived         = 14
	codeHTTPRequestDenied        = 15
	codeHTTPRequestMethodInvalid = 18
	codeHTTPRequestURIInvalid    = 19
	codeHTTPResponseIncomplete   = 25
	codeInternalError            = 38
)

// callError is a failed exchange as the guest sees it.
type callError struct {
	code   uint32
	detail string
}

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
	// byID holds each exchange from request_new until the guest drops it or
	// Run returns, so a body the guest never closes lives until then.
	byID map[uint32]*exchange
}

// wireRequest is a request as the guest describes it.
type wireRequest struct {
	method, scheme, authority, pathWithQuery string
	header                                   http.Header
	body                                     []byte
}

// exchange is one request and its response, from request_new to drop.
// Guest calls arrive one at a time, so only fields that handle's goroutine
// writes need synchronizing.
type exchange struct {
	req    wireRequest
	cancel context.CancelFunc        // set by handle
	done   chan struct{}             // set by handle; closed once resp or err is set
	resp   *http.Response            // set before done closes
	chunks chan []byte               // the body, set with resp; closed at its end
	err    atomic.Pointer[callError] // why the request or its body failed
	unread []byte                    // the part of the last chunk the guest has not read
}

func newSession(egress []Destination, authorize func(*http.Request) error, transport http.RoundTripper) *session {
	return &session{egress: egress, authorize: authorize, transport: transport, byID: map[uint32]*exchange{}}
}

func instantiateHTTP(ctx context.Context, rt wazero.Runtime) error {
	_, err := rt.NewHostModuleBuilder("gcx_http").
		NewFunctionBuilder().WithFunc(hostRequestNew).Export("request_new").
		NewFunctionBuilder().WithFunc(hostHandle).Export("handle").
		NewFunctionBuilder().WithFunc(hostPoll).Export("poll").
		NewFunctionBuilder().WithFunc(hostGetStatusCode).Export("get_status_code").
		NewFunctionBuilder().WithFunc(hostGetHeaders).Export("get_headers").
		NewFunctionBuilder().WithFunc(hostBodyRead).Export("body_read").
		NewFunctionBuilder().WithFunc(hostErrorCode).Export("error_code").
		NewFunctionBuilder().WithFunc(hostErrorDetail).Export("error_detail").
		NewFunctionBuilder().WithFunc(hostDrop).Export("drop").
		Instantiate(ctx)
	return err
}

func hostRequestNew(ctx context.Context, m api.Module,
	methodPtr, methodLen, schemePtr, schemeLen, authorityPtr, authorityLen,
	pathPtr, pathLen, headersPtr, headersLen, bodyPtr, bodyLen uint32,
) uint32 {
	s := sessionFrom(ctx)
	x := &exchange{req: wireRequest{
		method:        string(read(m, methodPtr, methodLen)),
		scheme:        string(read(m, schemePtr, schemeLen)),
		authority:     string(read(m, authorityPtr, authorityLen)),
		pathWithQuery: string(read(m, pathPtr, pathLen)),
		header:        decodeHeaders(read(m, headersPtr, headersLen)),
		body:          read(m, bodyPtr, bodyLen),
	}}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	s.byID[s.nextID] = x
	return s.nextID
}

func hostHandle(ctx context.Context, id uint32) {
	s := sessionFrom(ctx)
	x := s.get(id)
	if x.done != nil {
		panic("gcx_http.handle: request already sent")
	}
	// ctx is the Run call's context, which Run cancels when it returns, so
	// requests and bodies still in flight then are abandoned.
	reqCtx, cancel := context.WithCancel(ctx)
	x.cancel, x.done = cancel, make(chan struct{})
	go func() {
		resp, err := s.roundTrip(reqCtx, x.req)
		if err != nil {
			x.err.Store(err)
			close(x.done)
			return
		}
		x.resp, x.chunks = resp, make(chan []byte, 1)
		close(x.done)
		x.pump(reqCtx, resp)
	}()
}

func hostPoll(ctx context.Context, id uint32) int32 {
	x := sessionFrom(ctx).get(id)
	if x.done == nil {
		panic("gcx_http.poll: request not sent")
	}
	return x.state()
}

func hostGetStatusCode(ctx context.Context, id uint32) uint32 {
	code := sessionFrom(ctx).ready(id).resp.StatusCode
	if code < 0 || code > math.MaxUint16 { // net/http only returns 3-digit codes
		panic("gcx_http.get_status_code: invalid status code")
	}
	return uint32(code)
}

func hostGetHeaders(ctx context.Context, m api.Module, id, ptr, capacity uint32) uint32 {
	return writeSized(m, ptr, capacity, encodeHeaders(sessionFrom(ctx).ready(id).resp.Header))
}

func hostBodyRead(ctx context.Context, m api.Module, id, ptr, capacity uint32) int32 {
	b, n := sessionFrom(ctx).ready(id).readBody(int(capacity))
	if !m.Memory().Write(ptr, b) {
		panic("gcx_http.body_read: buffer out of bounds")
	}
	return n
}

func hostErrorCode(ctx context.Context, id uint32) uint32 {
	return sessionFrom(ctx).failure(id).code
}

func hostErrorDetail(ctx context.Context, m api.Module, id, ptr, capacity uint32) uint32 {
	return writeSized(m, ptr, capacity, []byte(sessionFrom(ctx).failure(id).detail))
}

func hostDrop(ctx context.Context, id uint32) {
	s := sessionFrom(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if x := s.lookup(id); x.cancel != nil {
		x.cancel() // stops the request, or the body pump, which closes the body
	}
	delete(s.byID, id)
}

// readBody returns up to capacity bytes of the body and body_read's result.
func (x *exchange) readBody(capacity int) ([]byte, int32) {
	if len(x.unread) == 0 {
		select {
		case c, ok := <-x.chunks:
			if !ok {
				if x.err.Load() != nil {
					return nil, -2
				}
				return nil, -1
			}
			x.unread = c
		default:
			return nil, 0
		}
	}
	n := min(capacity, len(x.unread))
	if n < 0 || n > math.MaxInt32 { // n is at most one chunk
		panic("gcx_http.body_read: chunk too large")
	}
	b := x.unread[:n]
	x.unread = x.unread[n:]
	return b, int32(n)
}

// pump feeds resp.Body to the guest a chunk at a time, so the host holds at most
// a couple of chunks however large the body is.
func (x *exchange) pump(ctx context.Context, resp *http.Response) {
	defer resp.Body.Close()
	defer close(x.chunks)
	for {
		buf := make([]byte, 32<<10)
		n, err := resp.Body.Read(buf)
		if n > 0 {
			select {
			case x.chunks <- buf[:n]:
			case <-ctx.Done():
				return
			}
		}
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			x.err.Store(classify(err, codeHTTPResponseIncomplete))
			return
		}
	}
}

func (s *session) get(id uint32) *exchange {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(id)
}

// ready returns id's exchange, whose response the guest must have seen poll report.
func (s *session) ready(id uint32) *exchange {
	x := s.get(id)
	if x.state() != 1 {
		panic("gcx_http: no response yet")
	}
	return x
}

// failure returns why id failed, which the guest must have seen poll or body_read report.
func (s *session) failure(id uint32) *callError {
	err := s.get(id).err.Load()
	if err == nil {
		panic("gcx_http: request has not failed")
	}
	return err
}

// state is poll's result: 0 pending (or not sent), 1 response, 2 failed.
func (x *exchange) state() int32 {
	if x.done == nil {
		return 0
	}
	select {
	case <-x.done:
	default:
		return 0
	}
	if x.resp == nil {
		return 2
	}
	return 1
}

// lookup requires s.mu. An unknown id is a guest bug; panicking traps the guest.
func (s *session) lookup(id uint32) *exchange {
	x, ok := s.byID[id]
	if !ok {
		panic("gcx_http: unknown request id")
	}
	return x
}

// roundTrip applies the egress policy and authorizer to a request, performs
// it, and returns the response, whose body the caller must close.
func (s *session) roundTrip(ctx context.Context, w wireRequest) (*http.Response, *callError) {
	// The authority alone decides the destination, so it can't smuggle in a
	// path, userinfo or another host.
	if w.authority == "" || strings.ContainsAny(w.authority, "/?#@\\") || !strings.HasPrefix(w.pathWithQuery, "/") {
		return nil, &callError{codeHTTPRequestURIInvalid, fmt.Sprintf("authority %q, path %q", w.authority, w.pathWithQuery)}
	}
	rawURL := w.scheme + "://" + w.authority + w.pathWithQuery
	if _, err := url.Parse(rawURL); err != nil {
		return nil, &callError{codeHTTPRequestURIInvalid, err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, w.method, rawURL, bytes.NewReader(w.body))
	if err != nil { // the URL parsed, so only the method can be wrong
		return nil, &callError{codeHTTPRequestMethodInvalid, err.Error()}
	}
	req.Header = w.header
	dest, err := match(s.egress, req.URL)
	if err != nil {
		return nil, &callError{codeHTTPRequestDenied, err.Error()}
	}
	if s.authorize != nil {
		if err := s.authorize(req); err != nil {
			return nil, &callError{codeHTTPRequestDenied, "request refused: " + err.Error()}
		}
		req.Body, _ = req.GetBody() // in case authorize read it; NewRequest's GetBody never fails
	}
	for k, vs := range dest.Header {
		req.Header[http.CanonicalHeaderKey(k)] = slices.Clone(vs) // a Transport may modify its request's headers
	}
	resp, err := s.transport.RoundTrip(req)
	if err != nil {
		return nil, classify(err, codeInternalError)
	}
	return resp, nil
}

// classify maps a transport error to the closest wasi:http error-code.
func classify(err error, fallback uint32) *callError {
	var (
		dns   *net.DNSError
		cert  *tls.CertificateVerificationError
		alert tls.AlertError
		rec   tls.RecordHeaderError
		ne    net.Error
	)
	code := fallback
	switch {
	case errors.As(err, &dns) && dns.IsTimeout:
		code = codeDNSTimeout
	case errors.As(err, &dns):
		code = codeDNSError
	case errors.Is(err, syscall.ECONNREFUSED):
		code = codeConnectionRefused
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, io.ErrUnexpectedEOF):
		code = codeConnectionTerminated
	case errors.As(err, &cert):
		code = codeTLSCertificateError
	case errors.As(err, &alert):
		code = codeTLSAlertReceived
	case errors.As(err, &rec):
		code = codeTLSProtocolError
	case errors.As(err, &ne) && ne.Timeout():
		code = codeConnectionTimeout
	}
	return &callError{code, err.Error()}
}

// read copies n bytes of guest memory at ptr.
func read(m api.Module, ptr, n uint32) []byte {
	b, ok := m.Memory().Read(ptr, n)
	if !ok {
		panic("gcx_http: guest memory out of bounds")
	}
	return bytes.Clone(b) // Read aliases guest memory
}

// writeSized writes b to the guest's buffer if it fits, and returns its length.
func writeSized(m api.Module, ptr, capacity uint32, b []byte) uint32 {
	n := len(b)
	if uint64(n) > math.MaxUint32 { // uint64, so this compiles where int is 32 bits
		panic("gcx_http: value too large for guest memory")
	}
	if n <= int(capacity) && !m.Memory().Write(ptr, b) {
		panic("gcx_http: buffer out of bounds")
	}
	return uint32(n)
}

// encodeHeaders and decodeHeaders use the ABI's "name\0value\0..." form.
func encodeHeaders(h http.Header) []byte {
	var b bytes.Buffer
	for k, vs := range h {
		for _, v := range vs {
			b.WriteString(k)
			b.WriteByte(0)
			b.WriteString(v)
			b.WriteByte(0)
		}
	}
	return b.Bytes()
}

func decodeHeaders(b []byte) http.Header {
	h := http.Header{}
	parts := bytes.Split(b, []byte{0})
	for i := 0; i+1 < len(parts); i += 2 {
		h.Add(string(parts[i]), string(parts[i+1]))
	}
	return h
}
