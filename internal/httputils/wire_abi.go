package httputils

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

// Parts of the wasip1 host transport (wire_wasip1.go) that don't call the
// host, kept out of the wasip1-only file so the native test suite covers them.

// readSized calls get with a buffer, retrying once with the length it
// reports if that did not fit.
func readSized(get func(buf *byte, capacity uint32) uint32) []byte {
	const initial = 1024
	buf := make([]byte, initial)
	n := get(&buf[0], initial)
	if int(n) > len(buf) {
		buf = make([]byte, n)
		n = get(&buf[0], n)
	}
	return buf[:min(int(n), len(buf))] // a host whose length changed gives a short read, not a panic
}

// encodeHeaders and decodeHeaders use the ABI's "name\0value\0..." form.
func encodeHeaders(h http.Header) string {
	var b strings.Builder
	for k, vs := range h {
		switch http.CanonicalHeaderKey(k) {
		case "Host", "Content-Length", "Transfer-Encoding", "Trailer": // the authority, and framing the host owns
			continue
		}
		for _, v := range vs {
			if strings.ContainsRune(k+v, 0) { // would break the framing; HTTP forbids it anyway
				continue
			}
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

// hostBody streams a response body from the host. The host calls are
// fields so that the native suite can test it.
type hostBody struct {
	read func(p []byte) int32          // body_read; p is never empty
	fail func() error                  // error_code and error_detail, once read returns -2
	drop func()                        // drop
	wait func(ready func() bool) error // waitFor with the request's context

	// Another goroutine may Close the body while a Read waits (as
	// client-go's StreamWatcher.Stop does), and the host traps on a dropped
	// id. So Close only marks the body closed while a Read is in progress,
	// and that Read drops the id once it is done with it. The flags are
	// atomic so this holds without relying on wasip1 running one goroutine
	// at a time.
	closed, reading, dropped atomic.Bool
}

func (b *hostBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b.reading.Store(true)
	defer func() {
		b.reading.Store(false)
		if b.closed.Load() {
			b.release()
		}
	}()
	var n int32
	err := b.wait(func() bool {
		if b.closed.Load() {
			return true
		}
		n = b.read(p)
		return n != 0
	})
	switch {
	case err != nil:
		return 0, err
	case b.closed.Load():
		return 0, http.ErrBodyReadAfterClose
	case n == -1:
		return 0, io.EOF
	case n == -2:
		return 0, b.fail()
	}
	return int(n), nil
}

func (b *hostBody) Close() error {
	b.closed.Store(true)
	if !b.reading.Load() {
		b.release()
	}
	return nil
}

// release drops the id, once.
func (b *hostBody) release() {
	if b.dropped.CompareAndSwap(false, true) {
		b.drop()
	}
}

// wasi:http@0.3.1 error-code case indices that hostError classifies.
const (
	codeDNSTimeout              = 0
	codeDNSError                = 1
	codeDestinationNotFound     = 2
	codeDestinationUnavailable  = 3
	codeDestinationIPUnroutable = 5
	codeConnectionRefused       = 6
	codeConnectionTerminated    = 7
	codeConnectionTimeout       = 8
	codeConnectionReadTimeout   = 9
	codeConnectionWriteTimeout  = 10
	codeConnectionLimitReached  = 11
	codeHTTPResponseIncomplete  = 25
	codeHTTPResponseTimeout     = 33
)

// hostError is a wasi:http error-code reported by the host. It is a
// net.Error, and reports the failures that arrive natively as a
// *net.OpError (DNS, destination and connection failures, and responses
// cut short) as transient, so the retry transport retries what it would
// retry over a socket.
type hostError struct {
	code   uint32
	detail string
}

func (e *hostError) Error() string {
	if e.detail == "" {
		return "gcx_http: " + errorCodeName(e.code)
	}
	return "gcx_http: " + errorCodeName(e.code) + ": " + e.detail
}

func (e *hostError) Timeout() bool {
	switch e.code {
	case codeDNSTimeout, codeConnectionTimeout, codeConnectionReadTimeout,
		codeConnectionWriteTimeout, codeHTTPResponseTimeout:
		return true
	}
	return false
}

// Temporary is deprecated in net.Error but required to implement it.
func (e *hostError) Temporary() bool { return e.Transient() }

// Transient reports whether retrying the request may succeed.
func (e *hostError) Transient() bool {
	switch e.code {
	case codeDNSError, codeDestinationNotFound, codeDestinationUnavailable,
		codeDestinationIPUnroutable, codeConnectionRefused, codeConnectionTerminated,
		codeConnectionLimitReached, codeHTTPResponseIncomplete:
		return true
	}
	return e.Timeout()
}

// errorCodeName names a wasi:http@0.3.1 error-code case.
func errorCodeName(code uint32) string {
	names := [...]string{
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
	if int(code) < len(names) {
		return names[code]
	}
	return "error-code " + strconv.FormatUint(uint64(code), 10)
}
