package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	maxOpModelProblemBytes = 8 * 1024
	opModelNotFoundSummary = "op model entity or container not found"
)

// isContainerNotFound matches only the backend's missing-container answer; any
// other 404, such as a wrong proxy route, must not read as an empty model.
func isContainerNotFound(err error, container string) bool {
	var detailed *fail.DetailedError
	return errors.As(err, &detailed) && detailed.Summary == opModelNotFoundSummary &&
		detailed.Details == "container "+container+" not found"
}

func mapIRMTransportError(err error, endpoint string) error {
	exit1 := fail.ExitGeneralError

	if errors.Is(err, context.DeadlineExceeded) {
		return &fail.DetailedError{
			Summary:  "IRM call timed out",
			Details:  fmt.Sprintf("No response from %s within the --timeout deadline: %v", endpoint, err),
			ExitCode: &exit1,
			Suggestions: []string{
				"Inspect current state before retrying a mutation; a timeout may follow a committed write",
				"For subsequent requests, increase the deadline: --timeout 30s",
				"Confirm the backend at " + endpoint + " is responsive",
			},
		}
	}
	if errors.Is(err, context.Canceled) {
		return err
	}

	return &fail.DetailedError{
		Summary:  "IRM backend unreachable at " + endpoint,
		Details:  err.Error(),
		ExitCode: &exit1,
		Suggestions: []string{
			"Confirm the active Grafana context is reachable and authenticated: gcx config check",
			"Confirm the IRM plugin and backend are running on that stack",
		},
	}
}

func mapOpModelHTTPError(resp *http.Response, endpoint string) error {
	detail := decodeOpModelProblem(resp)

	exit1 := fail.ExitGeneralError
	exit2 := fail.ExitUsageError
	exit3 := fail.ExitAuthFailure

	switch resp.StatusCode {
	case http.StatusUnprocessableEntity, http.StatusBadRequest:
		return &fail.DetailedError{
			Summary:  "op model rejected the request",
			Details:  detail,
			ExitCode: &exit2,
			Suggestions: []string{
				"Check the flag values against the command help (--help)",
			},
		}
	case http.StatusNotFound:
		return &fail.DetailedError{
			Summary:  opModelNotFoundSummary,
			Details:  detail,
			ExitCode: &exit1,
			Suggestions: []string{
				"List the container's current entities and ids: gcx irm opmodel containers get <container>",
				"Ids are container-scoped: confirm <container> matches where the entity was created",
				"A container with no entities yet does not exist to read",
			},
		}
	case http.StatusConflict:
		return &fail.DetailedError{
			Summary:  "op model domain rule rejected the request",
			Details:  detail,
			ExitCode: &exit1,
			Suggestions: []string{
				"Review the domain rule in the error and reconsider the intended write",
				"Do not automatically retry the write or impersonate a human actor after a rejection",
				"Evidence links require an observation source and an impact or hypothesis target",
			},
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &fail.DetailedError{
			Summary:  "op model backend rejected the caller",
			Details:  detail,
			ExitCode: &exit3,
		}
	case http.StatusServiceUnavailable:
		return &fail.DetailedError{
			Summary:  "op model backend unavailable at " + endpoint,
			Details:  detail,
			ExitCode: &exit1,
			Suggestions: []string{
				"The backend is up but not serving: check that its database migration gate has converged",
			},
		}
	case http.StatusTooManyRequests:
		return &fail.DetailedError{
			Summary:  "op model backend is rate limiting",
			Details:  detail,
			ExitCode: &exit1,
		}
	default:
		return &fail.DetailedError{
			Summary:  fmt.Sprintf("op model call failed with HTTP %d", resp.StatusCode),
			Details:  detail,
			ExitCode: &exit1,
		}
	}
}

func decodeOpModelProblem(resp *http.Response) string {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxOpModelProblemBytes))
	if err != nil || len(raw) == 0 {
		return http.StatusText(resp.StatusCode)
	}

	var status metav1.Status
	if json.Unmarshal(raw, &status) != nil {
		return strings.TrimSpace(string(raw))
	}
	var parts []string
	if status.Message != "" {
		parts = append(parts, status.Message)
	}
	if status.Details != nil {
		for _, cause := range status.Details.Causes {
			if cause.Message != "" {
				parts = append(parts, strings.TrimSpace(cause.Field+" "+cause.Message))
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "; ")
	}
	return strings.TrimSpace(string(raw))
}
