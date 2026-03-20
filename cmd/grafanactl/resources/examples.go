package resources

import (
	"encoding/json"
	"fmt"

	cmdconfig "github.com/grafana/grafanactl/cmd/grafanactl/config"
	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/grafana/grafanactl/internal/resources/discovery"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type examplesOpts struct {
	IO cmdio.Options
}

func (o *examplesOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (o *examplesOpts) Validate() error {
	return o.IO.Validate()
}

func examplesCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &examplesOpts{}

	cmd := &cobra.Command{
		Use:   "examples RESOURCE",
		Short: "Print an example manifest for a resource type",
		Example: `
	grafanactl resources examples incidents
	grafanactl resources examples incidents -o json
	grafanactl resources examples slo -o yaml
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			resourceName := args[0]

			cfg, err := configOpts.LoadRESTConfig(ctx)
			if err != nil {
				return err
			}

			reg, err := discovery.NewDefaultRegistry(ctx, cfg)
			if err != nil {
				return err
			}

			sels, err := resources.ParseSelectors([]string{resourceName})
			if err != nil {
				return fmt.Errorf("unknown resource %q: %w", resourceName, err)
			}

			filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{
				Selectors:            sels,
				PreferredVersionOnly: true,
			})
			if err != nil {
				return fmt.Errorf("unknown resource %q: %w", resourceName, err)
			}

			if len(filters) == 0 {
				return fmt.Errorf("unknown resource %q", resourceName)
			}

			gvk := filters[0].Descriptor.GroupVersionKind()
			example := adapter.ExampleForGVK(gvk)
			if example == nil {
				return fmt.Errorf("no example available for %q", resourceName)
			}

			var obj any
			if err := json.Unmarshal(example, &obj); err != nil {
				return fmt.Errorf("failed to parse example: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), obj)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}
