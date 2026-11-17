package resources_test

import (
	"bytes"
	"testing"

	cmdresources "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
)

func TestOperationWarningsUseDiagnosticWriter(t *testing.T) {
	summary := &remote.OperationSummary{}
	summary.RecordWarnings([]string{"spec.mode: unfamiliar value custom; known values: standard"})
	var stderr bytes.Buffer
	cmdresources.ReportOperationWarnings(&stderr, summary)
	require.Contains(t, stderr.String(), "spec.mode: unfamiliar value custom; known values: standard")
	require.Zero(t, summary.SuccessCount())
	require.Zero(t, summary.FailedCount())
	require.Zero(t, summary.SkippedCount())
}
