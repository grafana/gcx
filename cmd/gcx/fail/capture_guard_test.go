package fail_test

import (
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The capture package's doc invites writes from anywhere, so the discipline of
// who actually writes each slot lives in call-site pins, not in the package.
// The batch guard (cmd/gcx/resources/batchvolume_test.go) pins SetBatch by
// name and cannot see these setters, so the error-signal and auth-method
// slots get their own: http status and k8s reason are written only by
// CaptureErrorSignals in this package; the auth method only by the config
// selector, with login holding the one forcing override. A writer anywhere
// else bypasses the semantics those sites encode — raw-error timing,
// decided-before-validation, probe-outranks-load — and this guard makes that
// a test failure instead of a review hazard. Both guards share
// testutils.CaptureWriters so the walker cannot drift between them.
func TestCaptureSignalWritersArePinned(t *testing.T) {
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)

	wantWriters := map[string][]string{
		"SetHTTPStatus":          {filepath.Join("cmd", "gcx", "fail", "capture.go")},
		"SetK8sReason":           {filepath.Join("cmd", "gcx", "fail", "capture.go")},
		"SetGrafanaAuthMethod":   {filepath.Join("internal", "config", "grafana_auth.go")},
		"ForceGrafanaAuthMethod": {filepath.Join("cmd", "gcx", "login", "command.go")},
	}
	funcNames := make([]string, 0, len(wantWriters))
	for funcName := range wantWriters {
		funcNames = append(funcNames, funcName)
	}

	gotWriters := testutils.CaptureWriters(t, root,
		"github.com/grafana/gcx/internal/telemetry/capture", funcNames...)

	for funcName, want := range wantWriters {
		assert.ElementsMatch(t, want, gotWriters[funcName],
			"capture.%s has a pinned writer set; a new writer needs the same semantics its comment documents",
			funcName)
	}
}
