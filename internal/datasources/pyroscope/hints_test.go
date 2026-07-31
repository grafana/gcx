package pyroscope //nolint:testpackage // white-box test of unexported emitEmptyWindowHint

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestEmitEmptyWindowHint(t *testing.T) {
	start := time.Date(2026, 7, 16, 14, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	t.Run("explicit range names the window", func(t *testing.T) {
		var buf strings.Builder
		emitEmptyWindowHint(&buf, "labels", start, end, true)

		assert.Contains(t, buf.String(), "no labels found in window 2026-07-16T14:00:00Z to 2026-07-16T15:00:00Z")
		assert.Contains(t, buf.String(), "--since")
	})

	t.Run("default window is called out as such", func(t *testing.T) {
		var buf strings.Builder
		emitEmptyWindowHint(&buf, "profile types", time.Time{}, time.Time{}, false)

		out := buf.String()
		assert.Contains(t, out, "no profile types found in the default window (last 1h")
		assert.Contains(t, out, "--since")
	})
}

func TestMetadataCommandsExposeTimeFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{"labels", LabelsCmd(nil)},
		{"list-profile-types", ListProfileTypesCmd(nil)},
	} {
		for _, flag := range []string{"since", "from", "to"} {
			assert.NotNil(t, tc.cmd.Flags().Lookup(flag), "%s should expose --%s", tc.name, flag)
		}
	}
}
