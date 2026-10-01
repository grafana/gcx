package host

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Access is the most a sandboxed invocation may do over HTTP. The zero value
// is [AccessRead], so a sandbox that does not say otherwise is read-only.
type Access int

const (
	// AccessRead permits GET, HEAD and OPTIONS, plus POSTs to read-shaped
	// endpoints such as datasource queries.
	AccessRead Access = iota
	// AccessWrite additionally permits POST, PUT and PATCH.
	AccessWrite
	// AccessDelete additionally permits DELETE.
	AccessDelete
)

// ErrAccessDenied is returned (wrapped) for a request the sandbox's [Access]
// does not permit. The request is never sent.
var ErrAccessDenied = errors.New("request not permitted")

// GuardTransport enforces the [Access] of the sandbox carried by each
// request's context. Requests outside a sandbox pass through untouched.
//
// The rule mirrors the Grafana Assistant gcx proxy: the HTTP method decides,
// except for POSTs to endpoints that only read (see readShapedPOST).
func GuardTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if _, ok := base.(*GuardedTransport); ok {
		return base
	}
	return &GuardedTransport{Base: base}
}

// GuardedTransport is the transport returned by [GuardTransport].
type GuardedTransport struct {
	Base http.RoundTripper
}

func (t *GuardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if sb := sandbox(req.Context()); sb != nil {
		if need := requiredAccess(req); need > sb.Access {
			if req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, fmt.Errorf("%w: %s %s needs %s access, this invocation is %s", ErrAccessDenied, req.Method, req.URL.Path, need, sb.Access)
		}
	}
	return t.Base.RoundTrip(req)
}

// DefaultTransport returns [http.DefaultTransport] behind [GuardTransport].
// Use it wherever a nil transport would otherwise fall back to the default.
func DefaultTransport() http.RoundTripper {
	return GuardTransport(http.DefaultTransport)
}

// DefaultClient returns a client using [DefaultTransport], in place of
// [http.DefaultClient].
func DefaultClient() *http.Client {
	return &http.Client{Transport: DefaultTransport()}
}

// requiredAccess classifies a request. Unknown methods need full access.
func requiredAccess(req *http.Request) Access {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "":
		return AccessRead
	case http.MethodPost:
		if readShapedPOST(req.URL.Path) {
			return AccessRead
		}
		return AccessWrite
	case http.MethodPut, http.MethodPatch:
		return AccessWrite
	default:
		return AccessDelete
	}
}

// readShapedPOST reports whether path is an endpoint that uses POST only to
// carry a query body.
func readShapedPOST(path string) bool {
	switch {
	// Unified datasource query API and its legacy equivalent.
	case strings.Contains(path, "/apis/query.grafana.app/") && strings.HasSuffix(path, "/query"),
		strings.HasSuffix(path, "/api/ds/query"):
		return true
	// Agent Observability conversation search.
	case strings.HasSuffix(path, "/api/plugins/grafana-agento11y-app/resources/query/conversations/search"):
		return true
	}
	return false
}

func (a Access) String() string {
	switch a {
	case AccessRead:
		return "read-only"
	case AccessWrite:
		return "read-write"
	case AccessDelete:
		return "full"
	default:
		return fmt.Sprintf("Access(%d)", int(a))
	}
}
