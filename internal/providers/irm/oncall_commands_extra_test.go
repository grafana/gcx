package irm

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/pflag"
)

// Regression: alert-group action commands and escalate set a DefaultFormat
// that must correspond to a registered codec, otherwise IO.Validate()
// rejects the default value with "unknown output format".
// See https://github.com/grafana/gcx/issues/681.
//
// Force agent mode off so BindFlags doesn't override the per-command default
// with the agents codec — that override would mask the bug.
func TestActionOptsDefaultFormatIsValid(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	t.Run("alertGroupActionOpts", func(t *testing.T) {
		o := &alertGroupActionOpts{}
		o.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
		if err := o.IO.Validate(); err != nil {
			t.Fatalf("default IO.Validate() returned error: %v", err)
		}
	})

	t.Run("escalateOpts", func(t *testing.T) {
		o := &escalateOpts{}
		o.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
		if err := o.IO.Validate(); err != nil {
			t.Fatalf("default IO.Validate() returned error: %v", err)
		}
	})
}
