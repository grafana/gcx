package dynamic

import (
	"errors"
	"fmt"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ apierrors.APIStatus = (*APIError)(nil)

// APIError is an error that wraps a Kubernetes API status.
// It formats the error message in a more readable format
// (since Kubernetes natively will only log the message, which could sometimes be simply "unknown").
type APIError struct {
	status metav1.Status

	// cause is the original error when it carried no API status (a refused
	// connection, a TLS failure, a timeout). The status is then synthesized.
	cause error
}

// Error implements the error interface.
// It logs the code & reason in addition to the message,
// which could be simply "unknown" in some cases.
func (e APIError) Error() string {
	if e.cause != nil {
		// The status was synthesized; a made-up "500" would mislead.
		return e.cause.Error()
	}
	return fmt.Sprintf("%d %s: %s", e.status.Code, string(e.status.Reason), e.status.Message)
}

// Status implements the apierrors.APIStatus interface.
func (e APIError) Status() metav1.Status {
	return e.status
}

// Unwrap returns the original error when the status was synthesized, so
// callers can still match network and context errors.
func (e APIError) Unwrap() error {
	return e.cause
}

// Synthesized reports whether the status was made up locally because the
// original error carried none. The server never sent it.
func (e APIError) Synthesized() bool {
	return e.cause != nil
}

// ParseStatusError parses a Kubernetes API status from an error.
func ParseStatusError(err error) error {
	if err == nil {
		return nil
	}

	if status, ok := err.(apierrors.APIStatus); ok || errors.As(err, &status) {
		return APIError{status: status.Status()}
	}

	return APIError{
		status: metav1.Status{
			Status:  metav1.StatusFailure,
			Reason:  metav1.StatusReasonUnknown,
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		},
		cause: err,
	}
}
