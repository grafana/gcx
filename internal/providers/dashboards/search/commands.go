// Package search implements the `gcx dashboards search` command.
// The search endpoint is pinned to v0alpha1 of the dashboard.grafana.app API
// group. type=dashboard is sent as a server-side filter to exclude folders;
// the legacy type=dash-db value is ignored by the server but the modern
// type=dashboard value is honored.
package search

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	// searchResultAPIVersion is the pinned API version string used in the K8s envelope output.
	searchResultAPIVersion = "dashboard.grafana.app/v0alpha1"

	searchResultKind = "DashboardSearchResultList"
	searchHitKind    = "DashboardHit"
)

// GrafanaConfigLoader is the subset of providers.ConfigLoader used by the
// search command. Defined as a local interface so the command can be tested
// with a stub (narrow coupling; the concrete type satisfies the interface).
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// searchOpts holds all flags for the search command.
type searchOpts struct {
	IO      cmdio.Options
	Folders []string
	Tags    []string
	Limit   int
	Sort    string
	Deleted bool
	// --api-version is intentionally blocked at runtime;
	// bound only to avoid "unknown flag" errors from cobra.
	APIVersion string
}

func (o *searchOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &searchTableCodec{})
	o.IO.RegisterCustomCodec("wide", &searchTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringArrayVar(&o.Folders, "folder", nil, "Filter by folder name (repeatable)")
	flags.StringArrayVar(&o.Tags, "tag", nil, "Filter by tag (repeatable)")
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of results (0 for no limit)")
	flags.StringVar(&o.Sort, "sort", "", "Sort key (e.g. name_sort)")
	flags.BoolVar(&o.Deleted, "deleted", false, "Include recently deleted dashboards")
	// --api-version is defined so cobra parses it without an "unknown flag" error,
	// but RunE rejects it with a clear message.
	flags.StringVar(&o.APIVersion, "api-version", "", "Not supported on search (search is pinned to v0alpha1)")
	_ = flags.MarkHidden("api-version")
}

func (o *searchOpts) Validate() error {
	return o.IO.Validate()
}

// Commands returns the `gcx dashboards search` command.
func Commands(loader GrafanaConfigLoader) *cobra.Command {
	opts := &searchOpts{}

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search dashboards by title, tag, or folder",
		Long: `Search dashboards using the Grafana full-text search API.

The search endpoint is pinned to the v0alpha1 API version and does not support
--api-version overrides. Use 'gcx dashboards list' to list dashboards with the
server-preferred API version.

An empty positional query is accepted when at least one --folder or --tag
filter is supplied.`,
		Example: `  # Search by title.
  gcx dashboards search "my dashboard"

  # Search within a folder.
  gcx dashboards search --folder my-folder-name

  # Search by tag with multiple folders.
  gcx dashboards search --tag prod --folder folder-a --folder folder-b

  # Output as YAML.
  gcx dashboards search "metrics" -o yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Reject --api-version: the search endpoint is pinned to v0alpha1.
			// The flag is defined so cobra parses it correctly (no "unknown flag"
			// error), but it is always rejected here.
			if cmd.Flags().Changed("api-version") {
				return fmt.Errorf(
					"--api-version is not supported on 'search': the search endpoint is pinned to %s",
					searchResultAPIVersion,
				)
			}

			if err := opts.Validate(); err != nil {
				return err
			}

			query := ""
			if len(args) > 0 {
				query = args[0]
			}

			// Require at least one signal to prevent unbounded searches.
			if query == "" && len(opts.Folders) == 0 && len(opts.Tags) == 0 {
				return errors.New("provide a search query or at least one --folder or --tag filter")
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := newSearchClient(cfg)
			if err != nil {
				return err
			}

			params := SearchParams{
				Query:   query,
				Folders: opts.Folders,
				Tags:    opts.Tags,
				Limit:   opts.Limit,
				Sort:    opts.Sort,
				Deleted: opts.Deleted,
			}

			wire, err := client.Search(ctx, params)
			if err != nil {
				return err
			}

			// Build the K8s-style envelope.
			// type=dashboard is sent to the server, so all hits are dashboards.
			result := &DashboardSearchResultList{
				Kind:       searchResultKind,
				APIVersion: searchResultAPIVersion,
				Items:      make([]DashboardHit, 0, len(wire.Hits)),
			}
			for _, hit := range wire.Hits {
				result.Items = append(result.Items, DashboardHit{
					Kind:       searchHitKind,
					APIVersion: searchResultAPIVersion,
					Metadata: DashboardHitMeta{
						Name: hit.Name,
					},
					Spec: DashboardHitSpec{
						Title:  hit.Title,
						Folder: hit.Folder,
						Tags:   hit.Tags,
					},
				})
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
