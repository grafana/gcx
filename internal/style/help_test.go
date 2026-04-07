package style_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestHelpFunc_JSONTipShownWhenFlagExists(t *testing.T) {
	// Disable styling so we exercise the non-glamour path (agent mode).
	style.SetEnabled(false)
	t.Cleanup(func() { style.SetEnabled(true) })

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test command.",
	}
	cmd.Flags().String("json", "", "JSON fields")

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(style.HelpFunc(defaultHelp))
	cmd.Help() //nolint:errcheck

	assert.Contains(t, buf.String(), "Tip:")
	assert.Contains(t, buf.String(), "--json ?")
	assert.Contains(t, buf.String(), "--json field1,field2")
}

func TestHelpFunc_JSONTipHiddenWhenNoFlag(t *testing.T) {
	style.SetEnabled(false)
	t.Cleanup(func() { style.SetEnabled(true) })

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test command.",
	}
	// No --json flag registered.

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(style.HelpFunc(defaultHelp))
	cmd.Help() //nolint:errcheck

	assert.NotContains(t, buf.String(), "Tip:", "should not show tip without --json flag")
}
