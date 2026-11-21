package errortracking

import (
	"github.com/grafana/gcx/internal/gcxerrors"
)

func configError(details string, suggestions ...string) *gcxerrors.DetailedError {
	return &gcxerrors.DetailedError{Summary: gcxerrors.SummaryInvalidConfiguration, Details: details, Suggestions: suggestions}
}
