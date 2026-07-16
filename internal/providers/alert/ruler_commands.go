package alert

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// rulerCommands returns the ruler command group for datasource-managed
// (Mimir/Loki ruler) alerting and recording rules.
func rulerCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ruler",
		Short: "Manage datasource-managed (Mimir/Loki ruler) rule groups.",
		Long: `Manage alerting and recording rules stored in a Mimir or Loki ruler,
via Grafana's per-datasource ruler proxy.

These are datasource-managed rules, distinct from Grafana-managed alert rules.
Grafana-managed rules are read via 'gcx alert rules' and written via
'gcx resources pull/push alertrules' — the write path requires Grafana 13+,
where the rules.alerting.grafana.app API is enabled by default (on Grafana 12
it must be enabled explicitly via feature toggle). Every ruler command
requires --datasource with the UID of a Prometheus-flavored or Loki datasource.`,
	}
	cmd.AddCommand(
		rulerNamespacesCommands(loader),
		rulerGroupsCommands(loader),
	)
	return cmd
}

// rulerOpts carries the flags shared by every ruler command.
type rulerOpts struct {
	Datasource string
}

func (o *rulerOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Datasource, "datasource", "", "Datasource UID of the Mimir/Loki ruler (required)")
}

func (o *rulerOpts) Validate() error {
	if o.Datasource == "" {
		return errors.New("--datasource is required")
	}
	return nil
}

// newRulerClient resolves the datasource type to a ruler subtype and builds
// the client. The returned dsType is the datasource plugin type (single
// lookup, reused by callers that need PromQL-vs-LogQL decisions).
func (o *rulerOpts) newRulerClient(ctx context.Context, loader GrafanaConfigLoader) (*RulerClient, string, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	dsType, err := query.GetDatasourceType(ctx, cfg, o.Datasource)
	if err != nil {
		return nil, "", err
	}
	subtype, err := rulerSubtypeForDatasourceType(dsType)
	if err != nil {
		return nil, "", err
	}
	client, err := NewRulerClient(cfg, o.Datasource, subtype)
	if err != nil {
		return nil, "", err
	}
	return client, dsType, nil
}

// ---------------------------------------------------------------------------
// Namespaces
// ---------------------------------------------------------------------------

func rulerNamespacesCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "namespaces",
		Short:   "Manage ruler namespaces.",
		Aliases: []string{"namespace"},
	}
	cmd.AddCommand(
		newRulerNamespacesListCommand(loader),
		newRulerNamespacesDeleteCommand(loader),
	)
	return cmd
}

type rulerNamespacesListOpts struct {
	rulerOpts

	IO cmdio.Options
}

func (o *rulerNamespacesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &RulerNamespacesTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	o.rulerOpts.setup(flags)
}

// RulerNamespaceView is one row of the namespaces listing.
type RulerNamespaceView struct {
	Namespace string `json:"namespace"`
	Groups    int    `json:"groups"`
	Rules     int    `json:"rules"`
}

func newRulerNamespacesListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &rulerNamespacesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List ruler namespaces with group and rule counts.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := opts.newRulerClient(ctx, loader)
			if err != nil {
				return err
			}
			namespaces, err := client.ListNamespaces(ctx)
			if err != nil {
				return err
			}
			views := make([]RulerNamespaceView, 0, len(namespaces))
			for ns, groups := range namespaces {
				rules := 0
				for _, g := range groups {
					rules += len(g.Rules)
				}
				views = append(views, RulerNamespaceView{Namespace: ns, Groups: len(groups), Rules: rules})
			}
			sort.Slice(views, func(i, j int) bool { return views[i].Namespace < views[j].Namespace })
			return opts.IO.Encode(cmd.OutOrStdout(), views)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// RulerNamespacesTableCodec renders ruler namespaces as a table.
type RulerNamespacesTableCodec struct{}

func (c *RulerNamespacesTableCodec) Format() format.Format { return "table" }

func (c *RulerNamespacesTableCodec) Encode(w io.Writer, v any) error {
	views, ok := v.([]RulerNamespaceView)
	if !ok {
		return errors.New("invalid data type for table codec: expected []RulerNamespaceView")
	}
	t := style.NewTable("NAMESPACE", "GROUPS", "RULES")
	for _, n := range views {
		t.Row(n.Namespace, strconv.Itoa(n.Groups), strconv.Itoa(n.Rules))
	}
	return t.Render(w)
}

func (c *RulerNamespacesTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type rulerNamespacesDeleteOpts struct {
	rulerOpts

	Force bool
}

func (o *rulerNamespacesDeleteOpts) setup(flags *pflag.FlagSet) {
	o.rulerOpts.setup(flags)
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func newRulerNamespacesDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &rulerNamespacesDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete NAMESPACE",
		Short: "Delete a ruler namespace and all rule groups in it.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.OutOrStdout(), opts.Force,
				"Delete ruler namespace "+args[0]+" and all rule groups in it?")
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			client, _, err := opts.newRulerClient(ctx, loader)
			if err != nil {
				return err
			}
			if err := client.DeleteNamespace(ctx, args[0]); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deleted ruler namespace %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// Groups
// ---------------------------------------------------------------------------

func rulerGroupsCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "groups",
		Short:   "Manage ruler rule groups.",
		Aliases: []string{"group"},
	}
	cmd.AddCommand(
		newRulerGroupsListCommand(loader),
		newRulerGroupsGetCommand(loader),
		newRulerGroupsApplyCommand(loader),
		newRulerGroupsDeleteCommand(loader),
	)
	return cmd
}

type rulerGroupsListOpts struct {
	rulerOpts

	IO        cmdio.Options
	Namespace string
}

func (o *rulerGroupsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &RulerGroupsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	o.rulerOpts.setup(flags)
	flags.StringVar(&o.Namespace, "namespace", "", "Only list groups in this namespace")
}

// RulerGroupView is one row of the groups listing.
type RulerGroupView struct {
	Namespace string `json:"namespace"`
	Group     string `json:"group"`
	Interval  string `json:"interval,omitempty"`
	Rules     int    `json:"rules"`
}

func newRulerGroupsListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &rulerGroupsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List ruler rule groups.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := opts.newRulerClient(ctx, loader)
			if err != nil {
				return err
			}
			var namespaces map[string][]RulerRuleGroup
			if opts.Namespace != "" {
				namespaces, err = client.ListGroups(ctx, opts.Namespace)
			} else {
				namespaces, err = client.ListNamespaces(ctx)
			}
			if err != nil {
				return err
			}
			var views []RulerGroupView
			for ns, groups := range namespaces {
				for _, g := range groups {
					views = append(views, RulerGroupView{
						Namespace: ns,
						Group:     g.Name,
						Interval:  g.Interval,
						Rules:     len(g.Rules),
					})
				}
			}
			sort.Slice(views, func(i, j int) bool {
				if views[i].Namespace != views[j].Namespace {
					return views[i].Namespace < views[j].Namespace
				}
				return views[i].Group < views[j].Group
			})
			return opts.IO.Encode(cmd.OutOrStdout(), views)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// RulerGroupsTableCodec renders ruler rule groups as a table.
type RulerGroupsTableCodec struct{}

func (c *RulerGroupsTableCodec) Format() format.Format { return "table" }

func (c *RulerGroupsTableCodec) Encode(w io.Writer, v any) error {
	views, ok := v.([]RulerGroupView)
	if !ok {
		return errors.New("invalid data type for table codec: expected []RulerGroupView")
	}
	t := style.NewTable("NAMESPACE", "GROUP", "INTERVAL", "RULES")
	for _, g := range views {
		t.Row(g.Namespace, g.Group, g.Interval, strconv.Itoa(g.Rules))
	}
	return t.Render(w)
}

func (c *RulerGroupsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type rulerGroupsGetOpts struct {
	rulerOpts

	IO cmdio.Options
}

func (o *rulerGroupsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	o.rulerOpts.setup(flags)
}

func newRulerGroupsGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &rulerGroupsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get NAMESPACE GROUP",
		Short: "Get a ruler rule group (YAML by default, round-trips into apply).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := opts.newRulerClient(ctx, loader)
			if err != nil {
				return err
			}
			group, err := client.GetGroup(ctx, args[0], args[1])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), group)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type rulerGroupsApplyOpts struct {
	rulerOpts

	File      string
	Namespace string
	DryRun    bool
}

func (o *rulerGroupsApplyOpts) setup(flags *pflag.FlagSet) {
	o.rulerOpts.setup(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing rule groups (Prometheus rules file or a single group; YAML/JSON, use - for stdin)")
	flags.StringVar(&o.Namespace, "namespace", "", "Ruler namespace to apply the groups to (required)")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Parse and validate only; send nothing to the ruler")
}

func (o *rulerGroupsApplyOpts) Validate() error {
	if err := o.rulerOpts.Validate(); err != nil {
		return err
	}
	if o.Namespace == "" {
		return errors.New("--namespace is required")
	}
	return nil
}

func newRulerGroupsApplyCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &rulerGroupsApplyOpts{}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update ruler rule groups from a file.",
		Long: `Create or update rule groups in a ruler namespace. The input may be a
standard Prometheus rules file (with a top-level "groups:" list) or a single
bare rule group. Applying a group replaces the group with the same name.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			var input RulerApplyInput
			if err := providers.ReadFileOrStdin(opts.File, cmd.InOrStdin(), &input); err != nil {
				return err
			}
			groups, err := input.RuleGroups()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, dsType, err := opts.newRulerClient(ctx, loader)
			if err != nil {
				return err
			}

			promQL := query.NormalizeKind(dsType) == "prometheus"
			for _, g := range groups {
				if err := g.Validate(promQL); err != nil {
					return err
				}
			}

			if opts.DryRun {
				for _, g := range groups {
					cmdio.Info(cmd.OutOrStdout(), "would apply group %q (%d rule(s)) to namespace %q", g.Name, len(g.Rules), opts.Namespace)
				}
				return nil
			}

			var failed int
			for _, g := range groups {
				if err := client.ApplyGroup(ctx, opts.Namespace, g); err != nil {
					failed++
					cmdio.Warning(cmd.OutOrStdout(), "failed to apply group %q: %v", g.Name, err)
					continue
				}
				cmdio.Success(cmd.OutOrStdout(), "Applied group %q (%d rule(s)) to namespace %q", g.Name, len(g.Rules), opts.Namespace)
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d rule group(s) failed to apply", failed, len(groups))
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type rulerGroupsDeleteOpts struct {
	rulerOpts

	Force bool
}

func (o *rulerGroupsDeleteOpts) setup(flags *pflag.FlagSet) {
	o.rulerOpts.setup(flags)
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func newRulerGroupsDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &rulerGroupsDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete NAMESPACE GROUP",
		Short: "Delete a ruler rule group.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.OutOrStdout(), opts.Force,
				"Delete ruler rule group "+args[1]+" in namespace "+args[0]+"?")
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			client, _, err := opts.newRulerClient(ctx, loader)
			if err != nil {
				return err
			}
			if err := client.DeleteGroup(ctx, args[0], args[1]); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deleted ruler rule group %s/%s", args[0], args[1])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
