package fail_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	k8sapi "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func resetCapture(t *testing.T) {
	t.Helper()
	capture.Reset()
	t.Cleanup(capture.Reset)
}

// The generic probe reads any carrier of a transport status, however deeply
// wrapped — including inside an EmittedError, whose cause chain is where
// agent-mode in-band failures keep their transport error.
func TestCaptureErrorSignalsReadsHTTPStatusCarriers(t *testing.T) {
	for name, err := range map[string]error{
		"bare provider error":    providers.FormatError(401, []byte(`{"message":"denied"}`)),
		"wrapped provider error": fmt.Errorf("SM register/install: %w", providers.FormatError(401, nil)),
		"inside an EmittedError": gcxerrors.NewEmittedError(1, providers.FormatError(401, nil)),
	} {
		t.Run(name, func(t *testing.T) {
			resetCapture(t)

			fail.CaptureErrorSignals(err)

			assert.Equal(t, 401, capture.CurrentHTTPStatus())
			assert.Empty(t, capture.CurrentK8sReason(),
				"an HTTP failure is not a Kubernetes failure and must not invent a reason")
		})
	}
}

// A query APIError speaks only through its preserved transport status: the
// StatusCode field may hold a downstream status promoted from inside a 2xx
// body, and that must never be reported as a transport failure.
func TestCaptureErrorSignalsQueryErrors(t *testing.T) {
	t.Run("embedded error inside HTTP 200 captures the raw 2xx, never the promoted 400", func(t *testing.T) {
		resetCapture(t)

		err := queryerror.FromBody("loki", "query", 200, []byte(`{"results":{"A":{"error":"bad query","status":400}}}`))
		fail.CaptureErrorSignals(err)

		// Capture is raw; the 400–599 filter in the usage-event builder is
		// what keeps a 2xx off the wire. What must never appear here is the
		// body's 400 dressed up as a transport fact.
		assert.Equal(t, 200, capture.CurrentHTTPStatus())
	})

	t.Run("real transport failure is authoritative over the embedded status", func(t *testing.T) {
		resetCapture(t)

		err := queryerror.FromBody("loki", "query", 403, []byte(`{"results":{"A":{"error":"denied","status":500}}}`))
		fail.CaptureErrorSignals(err)

		assert.Equal(t, 403, capture.CurrentHTTPStatus())
	})

	t.Run("directly constructed APIError carries no transport status", func(t *testing.T) {
		resetCapture(t)

		fail.CaptureErrorSignals(queryerror.New("influxdb", "query", 400, "bad query", ""))

		assert.Zero(t, capture.CurrentHTTPStatus(),
			"New call sites hold body-level or synthesized statuses, not transport facts")
	})

	t.Run("query match wins without falling through to another carrier", func(t *testing.T) {
		resetCapture(t)

		// A joined chain holding both a status-less query error and a generic
		// carrier: the query error decides by matching, so the carrier must
		// not speak for it.
		joined := errors.Join(
			queryerror.New("influxdb", "query", 400, "bad query", ""),
			providers.FormatError(502, nil),
		)
		fail.CaptureErrorSignals(joined)

		assert.Zero(t, capture.CurrentHTTPStatus(),
			"match-wins is the rule: a query error with no transport status silences the chain")
	})
}

// Both Kubernetes error shapes yield their reason: the raw *StatusError, and
// the dynamic client's value-typed APIError, which has no Unwrap and never
// reaches convertAPIErrors — ReasonForError matches the APIStatus interface,
// so it sees both. Neither shape may feed http_status: a Kubernetes Status
// code is not a transport fact this field reports.
func TestCaptureErrorSignalsKubernetesReasons(t *testing.T) {
	gr := schema.GroupResource{Group: "dashboard.grafana.app", Resource: "dashboards"}

	t.Run("raw StatusError", func(t *testing.T) {
		resetCapture(t)

		fail.CaptureErrorSignals(fmt.Errorf("get: %w", k8sapi.NewUnauthorized("bad token")))

		assert.Equal(t, "Unauthorized", capture.CurrentK8sReason())
		assert.Zero(t, capture.CurrentHTTPStatus(),
			"k8s Status().Code must never feed http_status")
	})

	t.Run("dynamic client APIError", func(t *testing.T) {
		resetCapture(t)

		fail.CaptureErrorSignals(dynamic.ParseStatusError(k8sapi.NewNotFound(gr, "my-dashboard")))

		assert.Equal(t, "NotFound", capture.CurrentK8sReason())
		assert.Zero(t, capture.CurrentHTTPStatus())
	})

	t.Run("non-kubernetes error parsed by the dynamic client reports nothing", func(t *testing.T) {
		resetCapture(t)

		// ParseStatusError's fallback synthesizes reason "" and code 500;
		// neither is a finding.
		fail.CaptureErrorSignals(dynamic.ParseStatusError(errors.New("dial tcp: connection refused")))

		assert.Empty(t, capture.CurrentK8sReason())
		assert.Zero(t, capture.CurrentHTTPStatus())
	})
}

func TestCaptureErrorSignalsNilAndPlainErrors(t *testing.T) {
	resetCapture(t)

	fail.CaptureErrorSignals(nil)
	fail.CaptureErrorSignals(errors.New("plain failure"))

	assert.Zero(t, capture.CurrentHTTPStatus())
	assert.Empty(t, capture.CurrentK8sReason())
}
