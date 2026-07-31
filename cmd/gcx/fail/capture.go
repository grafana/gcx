package fail

import (
	"errors"

	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/telemetry/capture"
	k8sapi "k8s.io/apimachinery/pkg/api/errors"
)

// CaptureErrorSignals records the shape-only transport facts the surfaced
// error carries — an HTTP status, a Kubernetes reason — for the usage event.
// It must run on the raw error, as the first thing reportError does: the
// AlreadyReported and EmittedError short-circuits return before any
// conversion, and an EmittedError's cause chain is exactly where an
// agent-mode in-band failure keeps its transport error.
//
// It captures raw values only. The wire filters — the 400–599 range, the
// reason allowlist, the exit-4/5 suppression — live in the usage-event
// builder, which also means nothing captured here can reach the wire
// unfiltered.
func CaptureErrorSignals(err error) {
	if err == nil {
		return
	}

	// Match wins, by type rather than by value: a query APIError's StatusCode
	// may be a downstream status promoted from inside a 2xx body, so only its
	// preserved TransportStatus may speak for it — and when that is zero (the
	// error was built by New from a body-level status) nothing may, so there
	// is deliberately no fall-through to another carrier in the same chain.
	var queryErr *queryerror.APIError
	var carrier interface{ HTTPStatusCode() int }
	switch {
	case errors.As(err, &queryErr):
		capture.SetHTTPStatus(queryErr.TransportStatus)
	case errors.As(err, &carrier):
		capture.SetHTTPStatus(carrier.HTTPStatusCode())
	}

	// Independent of the HTTP probe: ReasonForError matches the APIStatus
	// interface, so it covers both a raw *k8sapi.StatusError and the dynamic
	// client's value-typed APIError, which never reaches convertAPIErrors.
	// An empty reason (StatusReasonUnknown, including ParseStatusError's
	// non-Kubernetes fallback) is not a finding; the setter ignores it.
	capture.SetK8sReason(string(k8sapi.ReasonForError(err)))
}
