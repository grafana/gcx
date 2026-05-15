package irm

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
)

// Regression: alert-group action commands and escalate previously set
// a DefaultFormat ("text") that had no registered codec, so the commands
// failed with "unknown output format". The idiomatic gcx pattern for
// action-only commands (sibling delete/close commands across providers)
// is to skip cmdio.Options entirely — no -o flag, no Validate(), just
// cmdio.Success() on completion. This test asserts those commands do
// not expose an -o/--output flag.
// See https://github.com/grafana/gcx/issues/681.
func TestActionCommandsHaveNoOutputFlag(t *testing.T) {
	t.Parallel()

	loader := stubLoader{}

	cases := []*cobra.Command{
		newAlertGroupActionCommand(loader, "resolve", "Resolve.", nil),
		newAlertGroupActionCommand(loader, "acknowledge", "Acknowledge.", nil),
		newAlertGroupActionCommand(loader, "unsilence", "Unsilence.", nil),
		newAlertGroupSilenceCommand(loader),
		newEscalateCommand(loader),
	}

	for _, cmd := range cases {
		t.Run(cmd.Name(), func(t *testing.T) {
			t.Parallel()
			if f := cmd.Flags().Lookup("output"); f != nil {
				t.Errorf("%s exposes --output flag; action-only commands should skip cmdio.Options", cmd.Name())
			}
			if f := cmd.Flags().Lookup("json"); f != nil {
				t.Errorf("%s exposes --json flag; action-only commands should skip cmdio.Options", cmd.Name())
			}
		})
	}
}

type stubLoader struct{}

func (stubLoader) LoadOnCallClient(_ context.Context) (OnCallAPI, string, error) {
	return nil, "", nil
}
