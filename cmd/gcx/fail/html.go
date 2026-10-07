package fail

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/queryerror"
	k8sapi "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HTML affects rendering only. Status classification remains authoritative.
func htmlResponseDetails(body, contentType string, status int) (string, bool) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	detected := http.DetectContentType([]byte(strings.TrimSpace(body)))
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" && !strings.HasPrefix(detected, "text/html") {
		return "", false
	}
	return fmt.Sprintf("invalid response from server: expected an API response, got an HTML page (HTTP %d %s)", status, http.StatusText(status)), true
}

// renderConvertedError applies the same body policy after every domain
// converter, retaining its classification, exit code, and remediation.
func renderConvertedError(err error, detailed *gcxerrors.DetailedError) *gcxerrors.DetailedError {
	if detailed == nil {
		return nil
	}
	response, ok := errorHTTPResponse(err)
	if !ok {
		return detailed
	}
	message, html := htmlResponseDetails(response.body, response.contentType, response.status)
	if !html {
		return detailed
	}
	operation := wrappedTypedErrorContext(err, response.inner)
	if operation == "" {
		operation = response.operation
	}
	if operation != "" {
		message = operation + ": " + message
	}
	detailed.Details = message
	detailed.Parent = nil
	return detailed
}

type errorResponse struct {
	inner       error
	body        string
	contentType string
	status      int
	operation   string
}

// Inspect typed response data rather than arbitrary error text. In particular,
// raw gcx api has only Message, so its body stays untouched.
func errorHTTPResponse(err error) (errorResponse, bool) {
	var transport *gcxerrors.HTTPStatusError
	var fleetErr *fleet.HTTPError
	var gcomErr *cloud.GCOMHTTPError
	var queryErr *queryerror.APIError
	var datasourceErr *datasources.APIError
	var statusErr interface {
		error
		k8sapi.APIStatus
	}
	var serviceErr serviceAPIError
	switch {
	case errors.As(err, &transport):
		return errorResponse{inner: transport, body: transport.ServerMessage, contentType: transport.ContentType, status: transport.Status}, true
	case errors.As(err, &fleetErr):
		return errorResponse{inner: fleetErr, body: fleetErr.Body, contentType: fleetErr.ContentType, status: fleetErr.Status, operation: fleetErr.Path}, true
	case errors.As(err, &gcomErr):
		return errorResponse{inner: gcomErr, body: gcomErr.Body, contentType: gcomErr.ContentType, status: gcomErr.Status}, true
	case errors.As(err, &statusErr):
		status := statusErr.Status()
		if status.Details != nil {
			for _, cause := range status.Details.Causes {
				if cause.Type == metav1.CauseTypeUnexpectedServerResponse {
					return errorResponse{inner: statusErr, body: cause.Message, status: int(status.Code)}, true
				}
			}
		}
		return errorResponse{inner: statusErr, body: status.Message, status: int(status.Code)}, true
	case errors.As(err, &queryErr):
		// Query failures embedded in HTTP 200 are not transport response failures.
		if queryErr.TransportStatus >= 200 && queryErr.TransportStatus < 300 {
			return errorResponse{}, false
		}
		return errorResponse{inner: queryErr, body: queryErr.Message, status: queryErr.StatusCode}, true
	case errors.As(err, &datasourceErr):
		return errorResponse{inner: datasourceErr, body: datasourceErr.Message, status: datasourceErr.StatusCode}, true
	case errors.As(err, &serviceErr):
		return errorResponse{inner: serviceErr, body: serviceErr.APIUserMessage(), status: serviceErr.HTTPStatusCode()}, true
	default:
		return errorResponse{}, false
	}
}
