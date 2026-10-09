//go:build wasip1

package httputils

import (
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
//	                                          headers as for get_headers, without Host
//	                                          (the authority carries it); body is complete
//	handle(id)                                send the request (separate from request_new
//	                                          so a body_write can stream request bodies
//	                                          between them later)
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
//   - One id serves the whole exchange: request_new's request, then after
//     handle its response, then the body body_read streams.
//   - The host owns framing: it sets Content-Length from body, and may
//     decompress the response as long as its headers then match the body.
//   - After poll reports a response, body_read streams its body. Once poll
//     or body_read reports failure, error_code and error_detail describe it.
//   - Guest misuse (an unknown or dropped id, handle twice, reading a
//     response before poll reports it, body_read with cap 0, an
//     out-of-bounds pointer) traps.
//
// The host never calls back into the guest: entering a //go:wasmexport while
// an import is in flight lets the Go scheduler run other goroutines on the
// same wasm stack, which corrupts it. Requests run concurrently on the host;
// the guest polls, sleeping between polls so other goroutines keep running.
//
// TLS, proxies and DNS are the host's business; ClientOpts.TLSConfig and
// client-go TLS settings have no effect in this build.
// --insecure-log-http-payload dumps whole response bodies, so it gives up
// streaming: each body is read to its end before the caller sees it.

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
		Status:     strings.TrimSpace(strconv.Itoa(code) + " " + http.StatusText(code)),
		StatusCode: code,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     decodeHeaders(readSized(func(buf *byte, n uint32) uint32 { return hostGetHeaders(id, buf, n) })),
		Body: &hostBody{
			read: func(p []byte) int32 { return hostBodyRead(id, &p[0], uint32(len(p))) },
			fail: func() error { return takeError(id) },
			drop: func() { hostDrop(id) },
			wait: func(ready func() bool) error { return waitFor(req.Context(), ready) },
		},
		Request: req,
	}
	resp.ContentLength = -1
	if n, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil {
		resp.ContentLength = n
	}
	return resp, nil
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

func takeError(id uint32) error {
	return &hostError{
		code:   hostErrorCode(id),
		detail: string(readSized(func(buf *byte, n uint32) uint32 { return hostErrorDetail(id, buf, n) })),
	}
}
