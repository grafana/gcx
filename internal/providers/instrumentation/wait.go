package instrumentation

import (
	"errors"

	"github.com/grafana/gcx/internal/gcxerrors"
)

// ErrWaitTimeoutEmitted is a sentinel returned by wait commands after they have
// already emitted a fused WaitResult (with Error populated) to stdout.
// It is an EmittedError, so the top-level reporter honors the exit code and
// SUPPRESSES any secondary output — the WaitResult is the only payload.
// errors.Is against this variable still works through %w wrapping (pointer
// identity), which the wait call sites rely on.
var ErrWaitTimeoutEmitted error = gcxerrors.NewEmittedError(
	gcxerrors.ExitGeneralError, errors.New("wait: timeout emitted"))

// WaitOutcome classifies a proto InstrumentationStatus into one of three
// terminal states for the wait polling loop.
type WaitOutcome int

const (
	// WaitPending means continue polling.
	WaitPending WaitOutcome = iota
	// WaitSuccess means the command should exit 0.
	WaitSuccess
	// WaitError means the command should exit non-zero.
	WaitError
)

// ClassifyK8sMonitoringStatus classifies a K8sMonitoringStatus wire value
// (from RunK8sMonitoring) for use by clusters wait.
//
// The wire returns full proto enum names like K8S_MONITORING_STATUS_INSTRUMENTED.
// The classifier matches these full names, and also accepts shorthand constants
// for backward compatibility with test fixtures.
func ClassifyK8sMonitoringStatus(s InstrumentationStatus) WaitOutcome {
	switch s {
	// Full proto enum names from wire (terminal success states)
	case "K8S_MONITORING_STATUS_INSTRUMENTED", "K8S_MONITORING_STATUS_EXCLUDED":
		return WaitSuccess
	case "K8S_MONITORING_STATUS_ERROR":
		return WaitError
	// Shorthand constants (for test compatibility)
	case StatusInstrumented, StatusExcluded:
		return WaitSuccess
	case StatusError:
		return WaitError
	default:
		// Covers UNSPECIFIED, NOT_INSTRUMENTED, PENDING_INSTRUMENTATION,
		// PENDING_UNINSTRUMENTATION, and any future unknown values.
		return WaitPending
	}
}

// ClassifyInstrumentationStatus classifies a DiscoveryItem InstrumentationStatus
// wire value (from RunK8sDiscovery) for use by apps wait.
//
// The two proto families differ only in the error variant name:
//
//	K8sMonitoringStatus uses ERROR; DiscoveryItem uses INSTRUMENTATION_ERROR.
func ClassifyInstrumentationStatus(s InstrumentationStatus) WaitOutcome {
	switch s {
	case "INSTRUMENTATION_STATUS_INSTRUMENTED", "INSTRUMENTATION_STATUS_EXCLUDED":
		return WaitSuccess
	case "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR":
		return WaitError
	default:
		// Covers UNSPECIFIED, NOT_INSTRUMENTED, PENDING_INSTRUMENTATION,
		// PENDING_UNINSTRUMENTATION, and any future unknown values.
		return WaitPending
	}
}
