package fail

import (
	"errors"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/gcxerrors"
)

func convertCloudOrgsErrors(err error) (*gcxerrors.DetailedError, bool) {
	if !strings.HasPrefix(err.Error(), "failed to list cloud organisations: ") {
		return nil, false
	}
	details := "Organisation listing requires a browser Cloud OAuth login."
	if !errors.Is(err, cloud.ErrUserOAuthRequired) {
		var httpErr *cloud.GCOMHTTPError
		if !errors.As(err, &httpErr) || (httpErr.Status != http.StatusUnauthorized && httpErr.Status != http.StatusForbidden) {
			return nil, false
		}
		details = gcomErrorDetails(httpErr, err.Error())
	}
	return &gcxerrors.DetailedError{
		Summary:  "Authorization failed",
		Details:  details,
		Parent:   err,
		ExitCode: new(gcxerrors.ExitAuthFailure),
		Suggestions: []string{
			"Run gcx cloud login using the same config/context; the default scopes include profile and stack management",
			"Unset GRAFANA_CLOUD_TOKEN or cloud.<entry>.token if set: access-policy tokens take precedence over OAuth and cannot list user memberships",
		},
	}, true
}
