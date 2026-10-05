//go:build wasip1

package httputils

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"time"
	"unsafe"
)

// wasip1 has no outbound sockets, so requests are delegated to the host,
// which must provide the "gcx_http" import module:
//
//	start(req_ptr, req_len u32) -> id u32   begin a request (raw HTTP/1.1, absolute URL)
//	poll(id u32) -> s64                     0 pending; >0 response length; <0 -(error length)
//	take(id u32, buf_ptr u32)               copy the response or error into buf and forget id
//	cancel(id u32)                          abandon the request and forget id
//
// The host never calls back into the guest: entering a //go:wasmexport while
// an import is in flight lets the Go scheduler run other goroutines on the
// same wasm stack, which corrupts it. Requests run concurrently on the host;
// the guest polls, sleeping between polls so other goroutines keep running.
//
// TLS, proxies and DNS are the host's business; ClientOpts.TLSConfig and
// client-go TLS settings have no effect in this build.

//go:wasmimport gcx_http start
func hostStart(reqPtr unsafe.Pointer, reqLen uint32) uint32

//go:wasmimport gcx_http poll
func hostPoll(id uint32) int64

//go:wasmimport gcx_http take
func hostTake(id uint32, buf unsafe.Pointer)

//go:wasmimport gcx_http cancel
func hostCancel(id uint32)

func init() {
	// Route clients that fall back to http.DefaultTransport, or clone it, to
	// the host. It must stay an *http.Transport: libraries type-assert it.
	t := http.DefaultTransport.(*http.Transport)
	t.RegisterProtocol("http", hostTransport{})
	t.RegisterProtocol("https", hostTransport{})
}

// WireTransport replaces rt with the host transport. See the package comment above.
func WireTransport(http.RoundTripper) http.RoundTripper { return hostTransport{} }

type hostTransport struct{}

func (hostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var b bytes.Buffer
	// WriteProxy keeps the absolute URL (scheme and host) in the request line.
	if err := req.WriteProxy(&b); err != nil {
		return nil, err
	}
	raw := b.Bytes()
	id := hostStart(unsafe.Pointer(unsafe.SliceData(raw)), uint32(len(raw)))

	wait := 50 * time.Microsecond
	for {
		n := hostPoll(id)
		if n == 0 {
			select {
			case <-req.Context().Done():
				hostCancel(id)
				return nil, req.Context().Err()
			case <-time.After(wait):
			}
			wait = min(wait*2, 5*time.Millisecond)
			continue
		}
		buf := make([]byte, max(n, -n))
		hostTake(id, unsafe.Pointer(unsafe.SliceData(buf)))
		if n < 0 {
			return nil, errors.New(string(buf))
		}
		return http.ReadResponse(bufio.NewReader(bytes.NewReader(buf)), req)
	}
}
