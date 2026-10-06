package httputils

import (
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
		{"framing is the host's job", http.Header{"Content-Length": {"9"}, "Transfer-Encoding": {"chunked"}, "Trailer": {"X"}, "A": {"b"}}, http.Header{"A": {"b"}}},
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

// fakeBody is a hostBody over a fake host that fails the test on any call
// after drop, as the real host traps.
type fakeBody struct {
	*hostBody

	reads   [][]byte // what successive body_read calls return; nil means pending
	readsN  atomic.Int32
	dropped atomic.Int32
}

func newFakeBody(t *testing.T, reads ...[]byte) *fakeBody {
	t.Helper()
	f := &fakeBody{reads: reads}
	f.hostBody = &hostBody{
		read: func(p []byte) int32 {
			if f.dropped.Load() > 0 {
				t.Error("body_read after drop")
			}
			i := int(f.readsN.Add(1)) - 1
			if i >= len(f.reads) || f.reads[i] == nil {
				return 0
			}
			switch r := f.reads[i]; string(r) {
			case "EOF":
				return -1
			case "FAIL":
				return -2
			default:
				n := copy(p, r)
				if n < 0 || n > math.MaxInt32 {
					panic("impossible copy length")
				}
				return int32(n)
			}
		},
		fail: func() error { return errors.New("host failure") },
		drop: func() { f.dropped.Add(1) },
		wait: func(ready func() bool) error {
			for !ready() {
				time.Sleep(time.Millisecond)
			}
			return nil
		},
	}
	return f
}

func TestHostBody(t *testing.T) {
	t.Run("streams then ends", func(t *testing.T) {
		f := newFakeBody(t, nil, []byte("abc"), []byte("de"), []byte("EOF"))
		got, err := io.ReadAll(f)
		if err != nil || string(got) != "abcde" {
			t.Fatalf("got %q, %v", got, err)
		}
		_ = f.Close()
		_ = f.Close()
		if n := f.dropped.Load(); n != 1 {
			t.Errorf("dropped %d times, want 1", n)
		}
	})

	t.Run("reports host failure", func(t *testing.T) {
		f := newFakeBody(t, []byte("FAIL"))
		if _, err := f.Read(make([]byte, 8)); err == nil || err.Error() != "host failure" {
			t.Errorf("err %v, want the host failure", err)
		}
	})

	t.Run("close while a read waits", func(t *testing.T) {
		f := newFakeBody(t) // body_read stays pending
		done := make(chan error)
		go func() {
			_, err := f.Read(make([]byte, 8))
			done <- err
		}()
		for f.readsN.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		_ = f.Close()
		if err := <-done; !errors.Is(err, http.ErrBodyReadAfterClose) {
			t.Errorf("err %v, want ErrBodyReadAfterClose", err)
		}
		if n := f.dropped.Load(); n != 1 {
			t.Errorf("dropped %d times, want 1", n)
		}
	})

	t.Run("read after close", func(t *testing.T) {
		f := newFakeBody(t, []byte("abc"))
		_ = f.Close()
		if _, err := f.Read(make([]byte, 8)); !errors.Is(err, http.ErrBodyReadAfterClose) {
			t.Errorf("err %v, want ErrBodyReadAfterClose", err)
		}
		if f.readsN.Load() != 0 || f.dropped.Load() != 1 {
			t.Errorf("reads %d, drops %d; want 0 and 1", f.readsN.Load(), f.dropped.Load())
		}
	})
}
