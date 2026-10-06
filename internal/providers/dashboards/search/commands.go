// Package search implements the `gcx dashboards search` command.
// Both search endpoints use v0alpha1 of the dashboard.grafana.app API group.
package search

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	Hybrid  bool
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

	flags.StringArrayVar(&o.Folders, "folder", nil, "Filter server-side by folder UID (repeatable, matches any; hybrid: empty matches root)")
	flags.StringArrayVar(&o.Tags, "tag", nil, "Filter by tag (repeatable)")
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of results (hybrid: 1-200; lexical: 0 for no limit)")
	flags.BoolVar(&o.Hybrid, "hybrid", false, "Combine keyword and semantic search, including dashboard panel content")
	flags.StringVar(&o.Sort, "sort", "", "Sort key (e.g. name_sort)")
	flags.BoolVar(&o.Deleted, "deleted", false, "Include recently deleted dashboards")
	// --api-version is defined so cobra parses it without an "unknown flag" error,
	// but RunE rejects it with a clear message.
	flags.StringVar(&o.APIVersion, "api-version", "", "Not supported on search (search is pinned to v0alpha1)")
	_ = flags.MarkHidden("api-version")
}

func (o *searchOpts) Validate(query string, flags *pflag.FlagSet) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if !o.Hybrid {
		if query == "" && len(o.Folders) == 0 && len(o.Tags) == 0 {
			return errors.New("provide a search query or at least one --folder or --tag filter")
		}
		return nil
	}
	if strings.TrimSpace(query) == "" {
		return errors.New("--hybrid requires a non-empty search query")
	}
	if len(query) > 1000 {
		return errors.New("--hybrid query must not exceed 1000 bytes")
	}
	for _, flag := range []string{"tag", "sort", "deleted"} {
		if flags.Changed(flag) {
			return fmt.Errorf("--%s is not supported with --hybrid; omit --hybrid to use lexical search", flag)
		}
	}
	if o.Limit < 1 || o.Limit > 200 {
		return fmt.Errorf("--limit must be between 1 and 200 with --hybrid, got %d", o.Limit)
	}
	if len(o.Folders) > 1000 {
		return errors.New("--hybrid supports at most 1000 folder filter values")
	}
	for _, folder := range o.Folders {
		if strings.Contains(folder, "*") {
			return fmt.Errorf("--folder %q must be an exact UID without '*' with --hybrid", folder)
		}
	}
	return nil
}

// Commands returns the `gcx dashboards search` command.
func Commands(loader GrafanaConfigLoader) *cobra.Command {
	opts := &searchOpts{}

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search dashboards by text or meaning.",
		Long: `Search dashboards using lexical search, or combine keyword and semantic
search with --hybrid to find dashboards by their content, including panels.

Hybrid search requires query text and supports --folder and --limit (1-200).
It returns the top matches in relevance order, not an exhaustive inventory;
there is no pagination. JSON/YAML results include spec.score and spec.chunks.
Scores are comparable only within one response. Use -o wide to see the best
matching chunk in the table. --tag, --sort and --deleted require lexical search.
If hybrid search is unavailable on your instance, retry without --hybrid.

Both endpoints use v0alpha1 and do not support --api-version overrides.
Lexical search accepts an empty query with at least one --folder or --tag.`,
		Example: `  # Search by title.
  gcx dashboards search "my dashboard"

  # Search within a folder.
  gcx dashboards search --folder my-folder-name

  # Search by tag with multiple folders.
  gcx dashboards search --tag prod --folder folder-a --folder folder-b

  # Find dashboards by meaning and panel content.
  gcx dashboards search "Kubernetes memory issues" --hybrid --limit 10 -o json

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

			query := ""
			if len(args) > 0 {
				query = args[0]
			}

			if err := opts.Validate(query, cmd.Flags()); err != nil {
				return err
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

			if opts.Hybrid {
				result, err := client.HybridSearch(ctx, params)
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), result)
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
