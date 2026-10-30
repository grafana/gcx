package experiments

import (
	"errors"
	"fmt"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// --- get-facets ---

type facetsOpts struct {
	IO     cmdio.Options
	Filter FacetsFilter
}

func (o *facetsOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Filter.Source, "source", "", "Only include runs from this source (collection or external)")
	flags.StringVar(&o.Filter.ExecutionKind, "execution-kind", "", "Only include runs of this execution kind (all, simulation, agent_run or unknown)")
	flags.StringVar(&o.Filter.From, "from", "", "Only include runs after this RFC3339 timestamp")
	flags.StringVar(&o.Filter.To, "to", "", "Only include runs before this RFC3339 timestamp")
}

func (o *facetsOpts) Validate(flags *pflag.FlagSet) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	// An explicitly empty selector would be dropped from the request and
	// silently widen the query to every run, so refuse it.
	for _, selector := range []struct{ flag, value string }{
		{"source", o.Filter.Source}, {"execution-kind", o.Filter.ExecutionKind}, {"from", o.Filter.From}, {"to", o.Filter.To},
	} {
		if flags.Changed(selector.flag) && strings.TrimSpace(selector.value) == "" {
			return fmt.Errorf("--%s cannot be empty", selector.flag)
		}
	}
	var from, to time.Time
	for _, bound := range []struct {
		flag  string
		value string
		out   *time.Time
	}{{"from", o.Filter.From, &from}, {"to", o.Filter.To, &to}} {
		if bound.value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(bound.value))
		if err != nil {
			return fmt.Errorf("--%s must be an RFC3339 timestamp: %w", bound.flag, err)
		}
		*bound.out = parsed
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return errors.New("--from must not be after --to")
	}
	return nil
}

func newGetFacetsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &facetsOpts{}
	cmd := &cobra.Command{
		Use:   "get-facets",
		Short: "Get the distinct owners, suites and tags across experiments.",
		Long: `Get the distinct owners, suites and tags across experiments, as used to populate filter options.

Only --source, --execution-kind, --from and --to narrow the result: the backend ignores other experiment filters so the option lists stay complete.`,
		Example: `  # List every owner, suite and tag.
  gcx agento11y experiments get-facets -o json

  # Restrict to external runs in a time window.
  gcx agento11y experiments get-facets --source external --from 2026-10-01T00:00:00Z --to 2026-10-08T00:00:00Z`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd.Flags()); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			facets, err := client.GetFacets(cmd.Context(), opts.Filter)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), facets)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
