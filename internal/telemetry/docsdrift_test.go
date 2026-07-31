package telemetry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The published usage-statistics page states both wire vocabularies in full,
// and disclosure accuracy is a contract of this telemetry program: the
// notice-revision mechanism exists precisely to re-disclose changed
// collection. A vocabulary entry added in Go and forgotten on the page would
// make the disclosure under-state what is collected with no failing gate, so
// this test ties the page to the source of truth the way
// skillsdrift_test.go ties skill docs to the command tree.
func TestUsageStatsPageListsTheFullWireVocabularies(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "sources", "anonymous-usage-statistics.md"))
	require.NoError(t, err)
	text := string(page)

	for _, reason := range telemetry.K8sReasonLabels() {
		assert.Contains(t, text, "`"+reason+"`",
			"k8s_reason value %q is collectable but missing from the published vocabulary", reason)
	}
	for _, method := range telemetry.GrafanaAuthMethodLabels() {
		assert.Contains(t, text, "`"+method+"`",
			"grafana_auth_method value %q is collectable but missing from the published vocabulary", method)
	}
	for _, bucket := range telemetry.Buckets() {
		assert.Contains(t, text, "`"+bucket+"`",
			"batch bucket %q is collectable but missing from the published vocabulary", bucket)
	}
}
