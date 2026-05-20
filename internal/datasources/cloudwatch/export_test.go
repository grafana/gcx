package cloudwatch

import (
	"io"

	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
)

// AllFramesEmpty exposes allFramesEmpty for testing.
func AllFramesEmpty(resp *cwclient.QueryResponse) bool {
	return allFramesEmpty(resp)
}

// MaybeEmitCrossAccountHint exposes maybeEmitCrossAccountHint for testing.
func MaybeEmitCrossAccountHint(w io.Writer, dimensions map[string]string, accountID string, resp *cwclient.QueryResponse) {
	maybeEmitCrossAccountHint(w, dimensions, accountID, resp)
}
