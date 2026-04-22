package slo

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type setupOpts struct{}

func (o *setupOpts) setup(_ *pflag.FlagSet) {}

func (o *setupOpts) Validate() error { return nil }

func newSetupCommand(p *SLOProvider) *cobra.Command {
	opts := &setupOpts{}
	cmd := &cobra.Command{
		Use:           "setup",
		Short:         "Set up slo (not yet implemented).",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		Annotations:   map[string]string{agent.AnnotationTokenCost: "small"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			err := p.Setup(ctx, nil)
			if errors.Is(err, framework.ErrSetupNotSupported) {
				fmt.Fprintf(cmd.ErrOrStderr(), "setup not yet implemented for %s\n", p.Name())
			}
			return err
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
