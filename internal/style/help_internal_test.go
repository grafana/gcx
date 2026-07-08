package style

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderLong must not hard-break long tokens such as documentation URLs, or
// terminals stop detecting them as clickable links.
func TestRenderLong_KeepsLongURLOnOneLine(t *testing.T) {
	url := "https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies.md"

	out, err := renderLong("See:\n" + url)
	require.NoError(t, err)

	// Strip ANSI escapes before inspecting line contents.
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(out, "")

	var found bool
	for line := range strings.SplitSeq(plain, "\n") {
		if strings.Contains(line, url) {
			found = true
			break
		}
	}
	assert.True(t, found, "the full URL must appear on a single line, got:\n%s", plain)
}
