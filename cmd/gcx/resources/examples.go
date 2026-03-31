package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type examplesOpts struct {
	IO cmdio.Options
}

func (o *examplesOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &tabCodec{wide: false})
	o.IO.RegisterCustomCodec("wide", &tabCodec{wide: true})
	o.IO.DefaultFormat("text")

	o.IO.BindFlags(flags)
}

func (o *examplesOpts) Validate() error {
	return o.IO.Validate()
}

func examplesCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &examplesOpts{}

	cmd := &cobra.Command{
		Use:   "examples [RESOURCE_SELECTOR]",
		Short: "List example manifests for resource types",
		Long:  "List example manifests for provider-backed resource types. Without arguments, lists all resources that have examples. With a selector, shows examples for matching resources.",
		Example: `
	gcx resources examples
	gcx resources examples -o wide
	gcx resources examples -o json
	gcx resources examples -o yaml
	gcx resources examples incidents
	gcx resources examples incidents -o json
	gcx resources examples slo -o yaml
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			reg, err := discovery.NewDefaultRegistry(ctx, cfg)
			if err != nil {
				return err
			}

			// Collect descriptors that have examples.
			descs, examples := collectExamples(ctx, reg)

			// If a resource selector was provided, filter to matching descriptors.
			if len(args) > 0 {
				resourceName := args[0]
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

				matched := make(map[string]bool, len(filters))
				for _, f := range filters {
					matched[f.Descriptor.GroupVersionKind().String()] = true
				}
				var filtered resources.Descriptors
				for _, d := range descs {
					if matched[d.GroupVersionKind().String()] {
						filtered = append(filtered, d)
					}
				}
				descs = filtered

				if len(descs) == 0 {
					return fmt.Errorf("no example available for %q", resourceName)
				}
			}

			switch opts.IO.OutputFormat {
			case "json", "yaml":
				return opts.IO.Encode(cmd.OutOrStdout(), examplesToNested(descs, examples))
			default:
				// text/wide: tabular output listing resources that have examples.
				return opts.IO.Encode(cmd.OutOrStdout(), descs)
			}
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// collectExamples returns sorted descriptors and a GVK→example map for all
// provider-registered resource types that have examples. Resources without
// examples are skipped.
func collectExamples(ctx context.Context, reg *discovery.Registry) (resources.Descriptors, map[schema.GroupVersionKind]json.RawMessage) {
	examples := make(map[schema.GroupVersionKind]json.RawMessage)
	var descs resources.Descriptors

	for _, r := range adapter.AllRegistrations() {
		ex := resolveExample(ctx, reg, r.GVK)
		if ex == nil {
			continue
		}
		examples[r.GVK] = ex
		descs = append(descs, r.Descriptor)
	}

	return descs.Sorted(), examples
}

// examplesToNested builds a nested group → version → []resource map for
// JSON/YAML output, mirroring the structure used by the schemas command.
// Resources without examples are skipped.
func examplesToNested(descs resources.Descriptors, examples map[schema.GroupVersionKind]json.RawMessage) map[string]any {
	type versionMap = map[string][]map[string]any
	groups := make(map[string]versionMap)

	for _, d := range descs {
		group := d.GroupVersion.Group
		version := d.GroupVersion.Version
		gvk := d.GroupVersionKind()

		ex, ok := examples[gvk]
		if !ok {
			continue
		}

		var parsed any
		if err := json.Unmarshal(ex, &parsed); err != nil {
			slog.Warn("skipping example with invalid JSON", "gvk", gvk.String(), "error", err)
			continue
		}

		entry := map[string]any{
			"kind":     d.Kind,
			"plural":   d.Plural,
			"singular": d.Singular,
			"example":  parsed,
		}

		if groups[group] == nil {
			groups[group] = make(versionMap)
		}
		groups[group][version] = append(groups[group][version], entry)
	}

	result := make(map[string]any, len(groups))
	for group, versions := range groups {
		vm := make(map[string]any, len(versions))
		for version, entries := range versions {
			vm[version] = entries
		}
		result[group] = vm
	}

	return result
}

// resolveExample returns the example for a GVK. Checks the cheap global
// registry first (all current providers register examples there), then falls
// back to instantiating the adapter factory (for future providers that inject
// examples via TypedRegistration). Returns nil if no example is registered.
func resolveExample(_ context.Context, _ *discovery.Registry, gvk schema.GroupVersionKind) json.RawMessage {
	return adapter.ExampleForGVK(gvk)
}
