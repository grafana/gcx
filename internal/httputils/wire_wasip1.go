//go:build wasip1

package httputils

import (
	"bytes"
	"cmp"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// wasip1 has no outbound sockets, so requests are delegated to the host,
// which must provide the "gcx_http" import module. Its shape follows
// wasi:http@0.3.1 (request, handler.handle, response, body stream,
// error-code), flattened to core wasm. Strings are (ptr, len) pairs.
//
//	request_new(method, scheme, authority, path_with_query, headers, body string) -> id u32
//	handle(id)                                send the request
//	poll(id) -> s32                           0 pending; 1 response ready; 2 failed
//	get_status_code(id) -> u32
//	get_headers(id, buf, cap u32) -> len u32  "name\0value\0..." for each value
//	body_read(id, buf, cap u32) -> s32        >0 bytes read; 0 pending; -1 end; -2 failed
//	error_code(id) -> u32                     a wasi:http error-code case index
//	error_detail(id, buf, cap u32) -> len u32
//	drop(id)                                  cancel anything in flight and forget id
//
// Rules the host must follow:
//   - Every call copies what it reads from guest memory before it returns;
//     pointers are only valid for the duration of the call.
//   - request_new and handle cannot fail. Any failure, including an
//     unparseable or refused request, is reported by poll as failed.
//   - Calls taking (buf, cap) return the full length and write only if it
//     fits, so the guest can retry with a bigger buffer.
//   - After poll reports a response, body_read streams its body. Once poll
//     or body_read reports failure, error_code and error_detail describe it.
//
// The host never calls back into the guest: entering a //go:wasmexport while
// an import is in flight lets the Go scheduler run other goroutines on the
// same wasm stack, which corrupts it. Requests run concurrently on the host;
// the guest polls, sleeping between polls so other goroutines keep running.
//
// TLS, proxies and DNS are the host's business; ClientOpts.TLSConfig and
// client-go TLS settings have no effect in this build.

//go:wasmimport gcx_http request_new
func hostRequestNew(method, scheme, authority, pathWithQuery, headers, body string) uint32

//go:wasmimport gcx_http handle
func hostHandle(id uint32)

//go:wasmimport gcx_http poll
func hostPoll(id uint32) int32

//go:wasmimport gcx_http get_status_code
func hostGetStatusCode(id uint32) uint32

//go:wasmimport gcx_http get_headers
func hostGetHeaders(id uint32, buf *byte, capacity uint32) uint32

//go:wasmimport gcx_http body_read
func hostBodyRead(id uint32, buf *byte, capacity uint32) int32

//go:wasmimport gcx_http error_code
func hostErrorCode(id uint32) uint32

//go:wasmimport gcx_http error_detail
func hostErrorDetail(id uint32, buf *byte, capacity uint32) uint32

//go:wasmimport gcx_http drop
func hostDrop(id uint32)

func init() {
	// Route clients that use http.DefaultTransport itself (directly, through a
	// nil Base, or via a type assertion as grafana-openapi-client-go does) to
	// the host. It must stay an *http.Transport for those type assertions.
	// Clones do not inherit RegisterProtocol registrations, so a library that
	// clones the default transport dials natively and fails; gcx's own
	// clients do not depend on that, as they all go through WireTransport.
	t := http.DefaultTransport.(*http.Transport)
	t.RegisterProtocol("http", hostTransport{})
	t.RegisterProtocol("https", hostTransport{})
}

// WireTransport replaces rt with the host transport. See the package comment above.
func WireTransport(http.RoundTripper) http.RoundTripper { return hostTransport{} }

type hostTransport struct{}

func (hostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	id := hostRequestNew(req.Method, req.URL.Scheme,
		cmp.Or(req.Host, req.URL.Host), // as http.Request.WriteProxy addresses it
		req.URL.RequestURI(), encodeHeaders(req.Header), string(body))
	hostHandle(id)

	var state int32
	err := waitFor(req.Context(), func() bool { state = hostPoll(id); return state != 0 })
	if err != nil {
		hostDrop(id)
		return nil, err
	}
	if state == 2 {
		err := takeError(id)
		hostDrop(id)
		return nil, err
	}

	code := int(hostGetStatusCode(id))
	resp := &http.Response{
		Status:     strconv.Itoa(code) + " " + http.StatusText(code),
		StatusCode: code,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     decodeHeaders(readSized(func(buf *byte, n uint32) uint32 { return hostGetHeaders(id, buf, n) })),
		Body:       &hostBody{id: id, ctx: req.Context()},
		Request:    req,
	}
	resp.ContentLength = -1
	if n, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil {
		resp.ContentLength = n
	}
	return resp, nil
}

// hostBody streams a response body from the host.
type hostBody struct {
	id     uint32
	ctx    context.Context
	closed bool
}

func (b *hostBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var n int32
	// Check closed before every host call: another goroutine may Close the
	// body (as client-go's StreamWatcher.Stop does) while this one sleeps,
	// and the host traps on a dropped id. wasm runs one goroutine at a time,
	// so nothing can close it between the check and the call.
	ready := func() bool {
		if b.closed {
			return true
		}
		n = hostBodyRead(b.id, &p[0], uint32(len(p)))
		return n != 0
	}
	if err := waitFor(b.ctx, ready); err != nil {
		return 0, err
	}
	if b.closed {
		return 0, http.ErrBodyReadAfterClose
	}
	switch n {
	case -1:
		return 0, io.EOF
	case -2:
		return 0, takeError(b.id)
	}
	return int(n), nil
}

func (b *hostBody) Close() error {
	if !b.closed {
		b.closed = true
		hostDrop(b.id)
	}
	return nil
}

// waitFor calls ready until it returns true, sleeping with backoff between
// calls so other goroutines run, or returns ctx's error once it is done.
func waitFor(ctx context.Context, ready func() bool) error {
	wait := 50 * time.Microsecond
	for !ready() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, 5*time.Millisecond)
	}
	return nil
}

// readSized calls get with a buffer, retrying once with the length it
// reports if that did not fit.
func readSized(get func(buf *byte, capacity uint32) uint32) []byte {
	buf := make([]byte, 1024)
	n := get(&buf[0], uint32(len(buf)))
	if int(n) > len(buf) {
		buf = make([]byte, n)
		n = get(&buf[0], n)
	}
	return buf[:n]
}

func encodeHeaders(h http.Header) string {
	var b strings.Builder
	for k, vs := range h {
		if http.CanonicalHeaderKey(k) == "Host" { // sent as the authority
			continue
		}
		for _, v := range vs {
			b.WriteString(k)
			b.WriteByte(0)
			b.WriteString(v)
			b.WriteByte(0)
		}
	}
	return b.String()
}

func decodeHeaders(b []byte) http.Header {
	h := http.Header{}
	parts := bytes.Split(b, []byte{0})
	for i := 0; i+1 < len(parts); i += 2 {
		h.Add(string(parts[i]), string(parts[i+1]))
	}
	return h
}

func takeError(id uint32) error {
	return &hostError{
		code:   hostErrorCode(id),
		detail: string(readSized(func(buf *byte, n uint32) uint32 { return hostErrorDetail(id, buf, n) })),
	}
}

// hostError is a wasi:http error-code reported by the host.
type hostError struct {
	code   uint32
	detail string
}

func (e *hostError) Error() string {
	name := "error-code " + strconv.Itoa(int(e.code))
	if int(e.code) < len(errorCodeNames) {
		name = errorCodeNames[e.code]
	}
	if e.detail == "" {
		return "gcx_http: " + name
	}
	return "gcx_http: " + name + ": " + e.detail
}

// errorCodeNames lists wasi:http@0.3.1's error-code cases in order.
var errorCodeNames = []string{
	"DNS-timeout", "DNS-error", "destination-not-found", "destination-unavailable",
	"destination-IP-prohibited", "destination-IP-unroutable", "connection-refused",
	"connection-terminated", "connection-timeout", "connection-read-timeout",
	"connection-write-timeout", "connection-limit-reached", "TLS-protocol-error",
	"TLS-certificate-error", "TLS-alert-received", "HTTP-request-denied",
	"HTTP-request-length-required", "HTTP-request-body-size", "HTTP-request-method-invalid",
	"HTTP-request-URI-invalid", "HTTP-request-URI-too-long", "HTTP-request-header-section-size",
	"HTTP-request-header-size", "HTTP-request-trailer-section-size", "HTTP-request-trailer-size",
	"HTTP-response-incomplete", "HTTP-response-header-section-size", "HTTP-response-header-size",
	"HTTP-response-body-size", "HTTP-response-trailer-section-size", "HTTP-response-trailer-size",
	"HTTP-response-transfer-coding", "HTTP-response-content-coding", "HTTP-response-timeout",
	"HTTP-upgrade-failed", "HTTP-protocol-error", "loop-detected", "configuration-error",
	"internal-error",
}
