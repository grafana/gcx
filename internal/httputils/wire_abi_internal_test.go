package httputils

import (
	"net/http"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func TestHeaderFraming(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   http.Header
		want http.Header
	}{
		{"empty", http.Header{}, http.Header{}},
		{"multiple values", http.Header{"Accept": {"a", "b"}, "X-Y": {"z"}}, http.Header{"Accept": {"a", "b"}, "X-Y": {"z"}}},
		{"empty value", http.Header{"X-Empty": {""}}, http.Header{"X-Empty": {""}}},
		{"host is the authority's job", http.Header{"Host": {"evil.example"}, "A": {"b"}}, http.Header{"A": {"b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, decodeHeaders([]byte(encodeHeaders(tc.in))))
		})
	}
	assert.Equal(t, "A\x00b\x00", encodeHeaders(http.Header{"A": {"b"}}))
}

// hostWrite fakes a (buf, cap) -> len host call that returns n bytes.
func hostWrite(n uint32, calls *int) func(buf *byte, capacity uint32) uint32 {
	return func(buf *byte, capacity uint32) uint32 {
		*calls++
		if n <= capacity {
			copy(unsafe.Slice(buf, capacity), strings.Repeat("x", int(n)))
		}
		return n
	}
}

func TestReadSized(t *testing.T) {
	for _, n := range []uint32{0, 5, 1024, 1025, 70000} {
		calls := 0
		assert.Equal(t, strings.Repeat("x", int(n)), string(readSized(hostWrite(n, &calls))), "length %d", n)
		if want := 1 + btoi(n > 1024); calls != want {
			t.Errorf("length %d: %d calls, want %d", n, calls, want)
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestHostError(t *testing.T) {
	for _, tc := range []struct {
		code               uint32
		detail, msg        string
		timeout, transient bool
	}{
		{codeDNSTimeout, "", "gcx_http: DNS-timeout", true, true},
		{codeDNSError, "no such host", "gcx_http: DNS-error: no such host", false, true},
		{codeConnectionRefused, "", "gcx_http: connection-refused", false, true},
		{codeConnectionReadTimeout, "", "gcx_http: connection-read-timeout", true, true},
		{codeHTTPResponseIncomplete, "", "gcx_http: HTTP-response-incomplete", false, true},
		{13, "bad cert", "gcx_http: TLS-certificate-error: bad cert", false, false},
		{15, "egress denied: x", "gcx_http: HTTP-request-denied: egress denied: x", false, false},
		{38, "", "gcx_http: internal-error", false, false},
		{99, "", "gcx_http: error-code 99", false, false},
	} {
		e := &hostError{code: tc.code, detail: tc.detail}
		assert.Equal(t, tc.msg, e.Error())
		assert.Equal(t, tc.timeout, e.Timeout(), "Timeout for %s", tc.msg)
		assert.Equal(t, tc.transient, e.Transient(), "Transient for %s", tc.msg)
	}
	// The last case index matches wasi:http@0.3.1's last error-code.
	assert.Equal(t, "internal-error", errorCodeName(38))
}
