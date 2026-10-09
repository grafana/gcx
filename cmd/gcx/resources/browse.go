//go:build !wasip1

package resources

import (
	"context"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/browse"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func addBrowseCommand(parent *cobra.Command, configOpts *cmdconfig.Options) {
	parent.AddCommand(browseCmd(configOpts))
}

type browseOpts struct{}

func (opts *browseOpts) setup(*pflag.FlagSet) {}

func (opts *browseOpts) Validate() error { return nil }

func browseCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &browseOpts{}

	cmd := &cobra.Command{
		Use:   "browse [RESOURCE_SELECTOR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "Interactively explore resources (read-only)",
		Long: `Interactively explore resource types and their objects. Read-only.

The left pane lists resource types; the right pane shows the highlighted
type's spec schema. Press enter to list that type's objects, and the right
pane renders the highlighted object as YAML.

Keys: / filter (fuzzy) · enter select · esc back or clear filter ·
tab scroll the preview · o open the object in Grafana · q quit.

Selectors narrow the type list, using the same syntax as 'gcx resources get'.
Requires an interactive terminal; in agent mode or with piped I/O, use
'gcx resources list-types' and 'gcx resources get' instead.`,
		Example: `
	# Explore every resource type
	gcx resources browse

	# Only dashboards and Synthetic Monitoring checks
	gcx resources browse dashboards checks.syntheticmonitoring`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := checkBrowseInteractive(
				agent.IsAgentMode(),
				term.IsTerminal(int(os.Stdin.Fd())),
				term.IsTerminal(int(os.Stdout.Fd())),
			); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			src, err := newBrowseSource(ctx, cfg, args)
			if err != nil {
				return err
			}

			ui := quietContext(ctx)
			_, err = tea.NewProgram(browse.New(ui, src, deeplink.Open), tea.WithContext(ui)).Run()
			return err
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// checkBrowseInteractive enforces the interactive-class contract: browse is a
// full-screen terminal program, so it fails fast instead of blocking in agent
// mode or drawing escape codes into a pipe.
func checkBrowseInteractive(agentMode, stdinTTY, stdoutTTY bool) error {
	suggestions := []string{
		"List resource types with 'gcx resources list-types'",
		"Fetch objects with 'gcx resources get <selector> -o yaml'",
	}
	if agentMode {
		return gcxerrors.DetailedError{
			Summary:     "browse is interactive and cannot run in agent mode",
			Details:     "it draws a full-screen terminal UI and waits for key presses",
			Suggestions: suggestions,
		}
	}
	if !stdinTTY || !stdoutTTY {
		return gcxerrors.DetailedError{
			Summary:     "browse needs an interactive terminal",
			Details:     "stdin and stdout must both be a terminal",
			Suggestions: suggestions,
		}
	}
	return nil
}

// quietContext silences the diagnostics that background fetches write to
// stderr (puller warnings, lazy provider config warnings). Stderr is the same
// terminal the full-screen UI draws on, so any line written there corrupts
// the layout; failures reach the user through the UI instead.
func quietContext(ctx context.Context) context.Context {
	ctx = logging.Context(ctx, &logging.NoOpLogger{})
	return config.ContextWithWarningWriter(ctx, io.Discard)
}

// browseSource serves the browser from the same discovery registry, pull
// pipeline, and schema fetcher as list-types and get, so provider-backed
// types (SLO, Synthetic Monitoring, ...) work alongside native ones.
type browseSource struct {
	cfg     config.NamespacedRESTConfig
	reg     *discovery.Registry
	types   resources.Descriptors
	schemas *discovery.SchemaFetcher
}

func newBrowseSource(ctx context.Context, cfg config.NamespacedRESTConfig, args []string) (*browseSource, error) {
	reg, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Resolve selectors now so a bad one fails before the terminal switches
	// to full screen.
	types, err := filterDescriptors(reg, reg.SupportedResources().Sorted(), args)
	if err != nil {
		return nil, err
	}
	fetcher, err := discovery.NewSchemaFetcher(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("initializing schema fetcher: %w", err)
	}
	return &browseSource{cfg: cfg, reg: reg, types: types, schemas: fetcher}, nil
}

func (s *browseSource) Types(context.Context) (resources.Descriptors, error) {
	return s.types, nil
}

func (s *browseSource) List(ctx context.Context, d resources.Descriptor) ([]unstructured.Unstructured, error) {
	sels, err := resources.ParseSelectors([]string{browse.Selector(d)})
	if err != nil {
		return nil, err
	}
	res, err := fetchWithRegistry(ctx, FetchRequest{Config: s.cfg}, s.reg, sels)
	if err != nil {
		return nil, err
	}
	items := res.Resources.ToUnstructuredList().Items
	resources.SortUnstructured(items)
	deeplink.InjectURLs(items, s.cfg.GrafanaURL)
	return items, pullFailures(res.PullSummary)
}

// pullFailures turns failures recorded during a pull into a
// *browse.PartialError, or nil when there were none.
func pullFailures(summary *remote.OperationSummary) error {
	if summary == nil || summary.FailedCount() == 0 {
		return nil
	}
	return &browse.PartialError{Failed: summary.FailedCount(), Err: summary.Failures()[0].Error}
}

func (s *browseSource) Schemas(ctx context.Context, descs resources.Descriptors) (map[string]map[string]any, error) {
	server, err := s.schemas.FetchSpecSchemas(ctx, descs)
	// On error, server is nil and provider-registered schemas still resolve.
	return schemasBySelector(descs, server), err
}

// schemasBySelector resolves each type's schema (server first, then the
// provider registry) and keys the result by browse.Selector.
func schemasBySelector(descs resources.Descriptors, server map[string]map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(descs))
	for _, d := range descs {
		gvk := d.GroupVersion.Group + "/" + d.GroupVersion.Version + "/" + d.Kind
		if found, ok := resolveSchema(server, gvk, d); ok {
			if m, isMap := found.(map[string]any); isMap {
				out[browse.Selector(d)] = m
			}
		}
	}
	return out
}
