package setup

import (
	"errors"
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/grafana/gcx/cmd/gcx/setup/instrumentation"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Command returns the setup command area for onboarding and configuring
// Grafana Cloud products.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Onboard and configure Grafana Cloud products.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Chain the root's PersistentPreRun (root command sets up logging/context).
			if root := cmd.Root(); root != nil && root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader := &providers.ConfigLoader{}
	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(instrumentation.Command(loader))
	cmd.AddCommand(NewStatusCommand())
	cmd.AddCommand(NewRunCommand(loader))

	return cmd
}

type setupStatusOpts struct {
	IO cmdio.Options
}

func (o *setupStatusOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &statusTextCodec{})
	o.IO.RegisterCustomCodec("wide", &statusTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *setupStatusOpts) Validate() error {
	return o.IO.Validate()
}

// NewStatusCommand returns the `setup status` subcommand. Exported for testing.
func NewStatusCommand() *cobra.Command {
	opts := &setupStatusOpts{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show aggregated setup status across all products.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			statuses := framework.AggregateStatus(cmd.Context(), 0)
			return opts.IO.Encode(cmd.OutOrStdout(), statuses)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type statusTextCodec struct{}

func (c *statusTextCodec) Format() format.Format {
	return "text"
}

func (c *statusTextCodec) Encode(w io.Writer, data any) error {
	statuses, ok := data.([]framework.ProductStatus)
	if !ok {
		return errors.New("statusTextCodec: expected []framework.ProductStatus")
	}
	t := style.NewTable("PRODUCT", "STATE", "DETAILS", "HINT")
	for _, s := range statuses {
		stateStr := string(s.State)
		if style.IsStylingEnabled() {
			stateStr = colorState(s.State)
		}
		t.Row(s.Product, stateStr, s.Details, s.SetupHint)
	}
	return t.Render(w)
}

func (c *statusTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text codec does not support decoding")
}

func colorState(state framework.ProductState) string {
	var color lipgloss.Color
	switch state {
	case framework.StateActive:
		color = lipgloss.Color("#7EB26D")
	case framework.StateConfigured:
		color = lipgloss.Color("#EAB839")
	case framework.StateError:
		color = lipgloss.Color("#F2495C")
	default:
		color = style.ColorMuted
	}
	return lipgloss.NewStyle().Foreground(color).Render(string(state))
}
