package fleet

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// experimentalPreamble is the wording required on every experimental command.
// cmd/gcx/root/experimental_test.go matches it with collapsed whitespace.
const experimentalPreamble = `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.`

func experimentalLong(body string) string {
	return experimentalPreamble + "\n\n" + body
}

func markExperimental(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[agent.AnnotationStability] = agent.StabilityExperimental
}

func requireNonEmpty(value, name string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

// ---------------------------------------------------------------------------
// Clusters
// ---------------------------------------------------------------------------

func (h *fleetHelper) clustersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "[experimental] Manage clusters registered with Fleet Management.",
		Long: experimentalLong(`Manage clusters stored by the Fleet Management remote manager.

A cluster is the record an in-cluster operator belongs to. It has an id, a
name, and the namespace the operator runs in. Use this to list those records
or to create one before adding Collector custom resources.

Not for Kubernetes Monitoring feature flags (cost metrics, Beyla, node logs).
Those are gcx instrumentation clusters.

Not for collectors that have registered with Fleet Management. Those are
gcx fleet collectors.

The operator connection (ClusterConnection) is not a command. The operator
opens that channel itself.`),
		Example: `  # List clusters
  gcx fleet clusters list

  # Create a cluster, then add a Collector CR for it
  gcx fleet clusters create --id cluster-a --name prod --namespace alloy
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --name metrics --namespace alloy`,
		Aliases: []string{"cluster"},
	}
	markExperimental(cmd)
	cmd.AddCommand(
		h.newClusterListCommand(),
		h.newClusterCreateCommand(),
		h.newClusterUpdateCommand(),
		h.newClusterDeleteCommand(),
	)
	return cmd
}

func (h *fleetHelper) newClusterListCommand() *cobra.Command {
	opts := &clusterListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "[experimental] List clusters registered with Fleet Management.",
		Long: experimentalLong(`List clusters stored by the Fleet Management remote manager.

The API returns the full set. --limit only truncates what is printed.
--limit 0 prints every cluster.

Not for gcx instrumentation clusters, and not for gcx fleet collectors.`),
		Example: `  # List a bounded summary
  gcx fleet clusters list

  # Print every cluster
  gcx fleet clusters list --limit 0

  # Select the identity fields
  gcx fleet clusters list --json id,name,namespace`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.IO.JSONDiscovery {
				return opts.IO.Encode(cmd.OutOrStdout(), []Cluster{})
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			clusters, err := client.ListClusters(ctx)
			if err != nil {
				return err
			}

			clusters, meta := cmdio.TruncateCompleteList(clusters, opts.Limit)
			if clusters == nil {
				clusters = []Cluster{}
			}
			meta = cmdio.AttachListMeta(meta, os.Args)
			if err := opts.IO.Encode(cmd.OutOrStdout(), clusters); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

type clusterListOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *clusterListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ClusterTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	o.IO.BindListLimit(flags, &o.Limit, "clusters", 50)
}

func (h *fleetHelper) newClusterCreateCommand() *cobra.Command {
	opts := &clusterWriteOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "[experimental] Create a Fleet Management cluster.",
		Long: experimentalLong(`Create a cluster in the Fleet Management remote manager.

--id is required and is chosen by the caller. The call fails when that id
already exists. --name and --namespace are stored empty when omitted.

The in-cluster operator can also introduce a cluster by connecting. This
command is the direct way to create the record first.`),
		Example: `  # Create a cluster with a name and operator namespace
  gcx fleet clusters create --id cluster-a --name prod --namespace alloy

  # Create a cluster with only an id
  gcx fleet clusters create --id cluster-b

  # Print the stored cluster as JSON
  gcx fleet clusters create --id cluster-a --name prod -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			cluster, err := opts.build(opts.ID, "--id")
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}
			created, err := client.CreateCluster(ctx, cluster)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *created)
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags(), clusterSuccessLine("Created"), true)
	return cmd
}

func (h *fleetHelper) newClusterUpdateCommand() *cobra.Command {
	opts := &clusterWriteOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "[experimental] Replace a Fleet Management cluster.",
		Long: experimentalLong(`Replace a cluster in the Fleet Management remote manager.

The call fails when the id does not exist. This is a full replacement, not a
patch: --name and --namespace that are omitted are stored empty, and the
previous values are not kept. Pass every field that should remain.

There is no get command. Use gcx fleet clusters list to read the current
name and namespace before replacing them.`),
		Example: `  # Replace the name and keep the operator namespace
  gcx fleet clusters update cluster-a --name staging --namespace alloy

  # Clear the name and namespace
  gcx fleet clusters update cluster-a

  # Print the stored cluster as JSON
  gcx fleet clusters update cluster-a --name staging --namespace alloy -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			cluster, err := opts.build(args[0], "id")
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}
			updated, err := client.UpdateCluster(ctx, cluster)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *updated)
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags(), clusterSuccessLine("Updated"), false)
	return cmd
}

type clusterWriteOpts struct {
	IO        cmdio.Options
	ID        string
	Name      string
	Namespace string
}

func (o *clusterWriteOpts) setup(flags *pflag.FlagSet, render func(any) (string, error), includeID bool) {
	if includeID {
		flags.StringVar(&o.ID, "id", "", "Cluster id. Required. Chosen by the caller; fails when the id already exists")
	}
	flags.StringVar(&o.Name, "name", "", "Cluster name. Omitted or empty is stored empty. Update replaces the cluster and does not keep the previous name")
	flags.StringVar(&o.Namespace, "namespace", "", "Namespace the operator runs in. Omitted or empty is stored empty. Update replaces the cluster and does not keep the previous namespace")
	o.IO.RegisterCustomCodec("text", &successLineCodec{render: render})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *clusterWriteOpts) build(id, idName string) (Cluster, error) {
	id, err := requireNonEmpty(id, idName)
	if err != nil {
		return Cluster{}, err
	}
	return Cluster{
		ID:        id,
		Name:      strings.TrimSpace(o.Name),
		Namespace: strings.TrimSpace(o.Namespace),
	}, nil
}

func clusterSuccessLine(verb string) func(any) (string, error) {
	return func(v any) (string, error) {
		cluster, ok := v.(Cluster)
		if !ok {
			return "", fmt.Errorf("invalid data type for text codec: expected Cluster, got %T", v)
		}
		if cluster.Name != "" {
			return fmt.Sprintf("%s cluster %s (id=%s)", verb, cluster.Name, cluster.ID), nil
		}
		return fmt.Sprintf("%s cluster %s", verb, cluster.ID), nil
	}
}

func (h *fleetHelper) newClusterDeleteCommand() *cobra.Command {
	return h.newDeleteCommand(
		"[experimental] Delete a Fleet Management cluster.",
		experimentalLong(`Delete a cluster from the Fleet Management remote manager.

The call fails when the id does not exist, and when the cluster still has
Collector custom resources. Delete those first:

  gcx fleet collector-crs list --cluster <id>
  gcx fleet collector-crs delete <collector-cr-id>`),
		`  # Delete a cluster that has no Collector CRs
  gcx fleet clusters delete cluster-a

  # Skip the confirmation prompt
  gcx fleet clusters delete cluster-a --force

  # Print the deletion receipt as JSON
  gcx fleet clusters delete cluster-a --force -o json`,
		"cluster",
		"Cluster",
		func(cmd *cobra.Command, client *Client, id string) error {
			return client.DeleteCluster(cmd.Context(), id)
		},
		func(m cmdio.SingleMutation) string {
			return "Deleted cluster " + m.Target.ID
		},
	)
}

// ---------------------------------------------------------------------------
// Collector CRs
// ---------------------------------------------------------------------------

func (h *fleetHelper) collectorCRsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collector-crs",
		Short: "[experimental] Manage desired Collector custom resources.",
		Long: experimentalLong(`Manage desired Collector custom resources stored by the Fleet Management remote manager.

A Collector CR is the custom resource an in-cluster operator should apply.
It belongs to one cluster. The next time that cluster's operator connects,
the server includes the current desired set.

Not for collectors that have registered with Fleet Management. Those are
agents that have phoned home: gcx fleet collectors.

Not for pipeline configuration assigned to registered collectors. That is
gcx fleet pipelines.

List shows the revision the server assigned and the revision the operator
last reported as applied. Create and update ignore revision, appliedRevision,
and applyError. The operator connection (ClusterConnection) is not a command.`),
		Example: `  # List every desired Collector CR
  gcx fleet collector-crs list

  # List Collector CRs for one cluster
  gcx fleet collector-crs list --cluster cluster-a

  # Create one, with the spec read from a file
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --namespace alloy --name metrics --release k8smon --spec-file spec.yaml`,
		Aliases: []string{"collector-cr"},
	}
	markExperimental(cmd)
	cmd.AddCommand(
		h.newCollectorCRListCommand(),
		h.newCollectorCRCreateCommand(),
		h.newCollectorCRUpdateCommand(),
		h.newCollectorCRDeleteCommand(),
	)
	return cmd
}

func (h *fleetHelper) newCollectorCRListCommand() *cobra.Command {
	opts := &collectorCRListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "[experimental] List desired Collector custom resources.",
		Long: experimentalLong(`List desired Collector custom resources.

Omit --cluster to list every Collector CR for the tenant. Set --cluster to
ask the server for that cluster only. An empty --cluster is the same as
omitting it.

The API returns the full matching set. --limit only truncates what is
printed. --limit 0 prints every match. Specs can be large; select fields
when you do not need spec.

appliedRevision is empty until the operator reports a revision. applyError
is empty when that revision was applied.`),
		Example: `  # List a bounded summary of every Collector CR
  gcx fleet collector-crs list

  # List Collector CRs for one cluster
  gcx fleet collector-crs list --cluster cluster-a

  # Drop spec from the payload
  gcx fleet collector-crs list --cluster cluster-a --json id,clusterId,namespace,name,release,revision,appliedRevision,applyError

  # Print every Collector CR for one cluster
  gcx fleet collector-crs list --cluster cluster-a --limit 0`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.IO.JSONDiscovery {
				return opts.IO.Encode(cmd.OutOrStdout(), []CollectorCR{})
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			crs, err := client.ListCollectorCRs(ctx, strings.TrimSpace(opts.Cluster))
			if err != nil {
				return err
			}

			crs, meta := cmdio.TruncateCompleteList(crs, opts.Limit)
			if crs == nil {
				crs = []CollectorCR{}
			}
			meta = cmdio.AttachListMeta(meta, os.Args)
			if err := opts.IO.Encode(cmd.OutOrStdout(), crs); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

type collectorCRListOpts struct {
	IO      cmdio.Options
	Limit   int
	Cluster string
}

func (o *collectorCRListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &CollectorCRTableCodec{})
	o.IO.RegisterCustomCodec("wide", &CollectorCRTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	o.IO.BindListLimit(flags, &o.Limit, "collector CRs", 50)
	flags.StringVar(&o.Cluster, "cluster", "", "Cluster id. Empty lists every Collector CR. Sent to the server as clusterId")
}

func (h *fleetHelper) newCollectorCRCreateCommand() *cobra.Command {
	opts := &collectorCRWriteOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "[experimental] Create a desired Collector custom resource.",
		Long: experimentalLong(`Create a desired Collector custom resource.

--id and --cluster are required. The call fails when the id already exists
or the cluster does not. --namespace, --name, --release, and --spec are
stored empty when omitted.

The server assigns revision from namespace, name, release, and spec.
revision, appliedRevision, and applyError cannot be set here.

--spec and --spec-file are mutually exclusive. --spec-file - reads stdin.
The spec is stored as written, including a trailing newline.`),
		Example: `  # Create a Collector CR with an inline spec
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --namespace alloy --name metrics --release k8smon --spec "spec: {}"

  # Read the spec from a file
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --spec-file spec.yaml

  # Print the stored Collector CR, including the server revision
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --spec "spec: {}" -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			cr, err := opts.build(opts.ID, "--id", cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}
			created, err := client.CreateCollectorCR(ctx, cr)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *created)
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags(), collectorCRSuccessLine("Created"), true)
	return cmd
}

func (h *fleetHelper) newCollectorCRUpdateCommand() *cobra.Command {
	opts := &collectorCRWriteOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "[experimental] Replace a desired Collector custom resource.",
		Long: experimentalLong(`Replace a desired Collector custom resource.

The call fails when the id does not exist or the cluster does not. This is
a full replacement, not a patch: --namespace, --name, --release, and --spec
that are omitted are stored empty, and the previous values are not kept.
--cluster is required because the stored Collector CR must name a cluster.

The server assigns a new revision when namespace, name, release, or spec
changes. revision, appliedRevision, and applyError cannot be set here.

There is no get command. Use gcx fleet collector-crs list to read the
current fields before replacing them.`),
		Example: `  # Replace the spec and keep the other fields
  gcx fleet collector-crs update cr-a --cluster cluster-a --namespace alloy --name metrics --release k8smon --spec-file spec.yaml

  # Clear namespace, name, release, and spec
  gcx fleet collector-crs update cr-a --cluster cluster-a

  # Print the stored Collector CR, including the new revision
  gcx fleet collector-crs update cr-a --cluster cluster-a --name metrics -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			cr, err := opts.build(args[0], "id", cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}
			updated, err := client.UpdateCollectorCR(ctx, cr)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *updated)
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags(), collectorCRSuccessLine("Updated"), false)
	return cmd
}

type collectorCRWriteOpts struct {
	IO        cmdio.Options
	ID        string
	Cluster   string
	Namespace string
	Name      string
	Release   string
	Spec      string
	SpecFile  string
}

func (o *collectorCRWriteOpts) setup(flags *pflag.FlagSet, render func(any) (string, error), includeID bool) {
	if includeID {
		flags.StringVar(&o.ID, "id", "", "Collector CR id. Required. Chosen by the caller; fails when the id already exists")
	}
	flags.StringVar(&o.Cluster, "cluster", "", "Cluster id. Required. The cluster must already exist")
	flags.StringVar(&o.Namespace, "namespace", "", "Namespace of the custom resource. Omitted or empty is stored empty. Update replaces the Collector CR and does not keep the previous namespace")
	flags.StringVar(&o.Name, "name", "", "Name of the custom resource. Omitted or empty is stored empty. Update replaces the Collector CR and does not keep the previous name")
	flags.StringVar(&o.Release, "release", "", "Helm release from the Kubernetes Monitoring chart. Omitted or empty is stored empty. Update replaces the Collector CR and does not keep the previous release")
	flags.StringVar(&o.Spec, "spec", "", "YAML spec, stored as written. Mutually exclusive with --spec-file. Omitted or empty is stored empty. Update replaces the Collector CR and does not keep the previous spec")
	flags.StringVar(&o.SpecFile, "spec-file", "", "File containing the YAML spec, or - for stdin. Mutually exclusive with --spec. The file contents are stored as written")
	o.IO.RegisterCustomCodec("text", &successLineCodec{render: render})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *collectorCRWriteOpts) Validate() error {
	if o.Spec != "" && o.SpecFile != "" {
		return errors.New("--spec and --spec-file are mutually exclusive")
	}
	return o.IO.Validate()
}

func (o *collectorCRWriteOpts) build(id, idName string, in io.Reader) (CollectorCR, error) {
	id, err := requireNonEmpty(id, idName)
	if err != nil {
		return CollectorCR{}, err
	}
	clusterID, err := requireNonEmpty(o.Cluster, "--cluster")
	if err != nil {
		return CollectorCR{}, err
	}
	spec, err := o.readSpec(in)
	if err != nil {
		return CollectorCR{}, err
	}
	return CollectorCR{
		ID:        id,
		ClusterID: clusterID,
		Namespace: strings.TrimSpace(o.Namespace),
		Name:      strings.TrimSpace(o.Name),
		Release:   strings.TrimSpace(o.Release),
		Spec:      spec,
	}, nil
}

func (o *collectorCRWriteOpts) readSpec(in io.Reader) (string, error) {
	if o.SpecFile == "" {
		return o.Spec, nil
	}
	if o.SpecFile == "-" {
		body, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("read spec from stdin: %w", err)
		}
		return string(body), nil
	}
	body, err := os.ReadFile(o.SpecFile)
	if err != nil {
		return "", fmt.Errorf("read spec file %q: %w", o.SpecFile, err)
	}
	return string(body), nil
}

func collectorCRSuccessLine(verb string) func(any) (string, error) {
	return func(v any) (string, error) {
		cr, ok := v.(CollectorCR)
		if !ok {
			return "", fmt.Errorf("invalid data type for text codec: expected CollectorCR, got %T", v)
		}
		if cr.Name != "" {
			return fmt.Sprintf("%s collector CR %s (id=%s, revision=%s)", verb, cr.Name, cr.ID, cr.Revision), nil
		}
		return fmt.Sprintf("%s collector CR %s (revision=%s)", verb, cr.ID, cr.Revision), nil
	}
}

func (h *fleetHelper) newCollectorCRDeleteCommand() *cobra.Command {
	return h.newDeleteCommand(
		"[experimental] Delete a desired Collector custom resource.",
		experimentalLong(`Delete a desired Collector custom resource by id.

The call fails when the id does not exist. The next operator connection for
that cluster omits it. The id alone identifies the Collector CR; a cluster
id is not required.`),
		`  # Delete a Collector CR
  gcx fleet collector-crs delete cr-a

  # Skip the confirmation prompt
  gcx fleet collector-crs delete cr-a --force

  # Print the deletion receipt as JSON
  gcx fleet collector-crs delete cr-a --force -o json`,
		"collector CR",
		"CollectorCR",
		func(cmd *cobra.Command, client *Client, id string) error {
			return client.DeleteCollectorCR(cmd.Context(), id)
		},
		func(m cmdio.SingleMutation) string {
			return "Deleted collector CR " + m.Target.ID
		},
	)
}

type confirmedDeleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *confirmedDeleteOpts) setup(flags *pflag.FlagSet, render func(any) (string, error)) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	o.IO.RegisterCustomCodec("text", &successLineCodec{render: render})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (h *fleetHelper) newDeleteCommand(short, longText, example, noun, kind string, deleteFn func(*cobra.Command, *Client, string) error, success func(cmdio.SingleMutation) string) *cobra.Command {
	opts := &confirmedDeleteOpts{}
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Short:   short,
		Long:    longText,
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return h.confirmDelete(cmd, opts.Force, args[0], noun, kind, opts.IO.Encode, func(client *Client, id string) error {
				return deleteFn(cmd, client, id)
			})
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags(), singleMutationLine(success))
	return cmd
}

func (h *fleetHelper) confirmDelete(cmd *cobra.Command, force bool, rawID, noun, kind string, encode func(io.Writer, any) error, deleteFn func(*Client, string) error) error {
	id, err := requireNonEmpty(rawID, "id")
	if err != nil {
		return err
	}

	proceed, err := providers.ConfirmDestructive(
		cmd.InOrStdin(), cmd.ErrOrStderr(), force,
		fmt.Sprintf("Delete %s %s?", noun, id),
	)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	client, _, err := h.loadClient(cmd.Context())
	if err != nil {
		return err
	}
	if err := deleteFn(client, id); err != nil {
		return err
	}
	return encode(cmd.OutOrStdout(), cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{
		Kind: kind,
		ID:   id,
	}))
}

// ---------------------------------------------------------------------------
// Table codecs
// ---------------------------------------------------------------------------

// ClusterTableCodec renders clusters as a table.
type ClusterTableCodec struct{}

// Format returns the codec's format identifier.
func (c *ClusterTableCodec) Format() format.Format { return "table" }

// Encode writes the cluster list as a table.
func (c *ClusterTableCodec) Encode(w io.Writer, v any) error {
	clusters, ok := v.([]Cluster)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Cluster")
	}
	t := style.NewTable("ID", "NAME", "NAMESPACE")
	for _, cluster := range clusters {
		t.Row(cluster.ID, displayOrDash(cluster.Name), displayOrDash(cluster.Namespace))
	}
	return t.Render(w)
}

// Decode is not supported for table format.
func (c *ClusterTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

// CollectorCRTableCodec renders Collector CRs as a table.
// Wide includes the spec. The default table omits it.
type CollectorCRTableCodec struct {
	Wide bool
}

// Format returns the codec's format identifier.
func (c *CollectorCRTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes the Collector CR list as a table.
func (c *CollectorCRTableCodec) Encode(w io.Writer, v any) error {
	crs, ok := v.([]CollectorCR)
	if !ok {
		return errors.New("invalid data type for table codec: expected []CollectorCR")
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable(
			"ID", "CLUSTER", "NAMESPACE", "NAME", "RELEASE", "REVISION", "APPLIED", "ERROR", "SPEC",
		).MultilineCells(true)
	} else {
		t = style.NewTable("ID", "CLUSTER", "NAMESPACE", "NAME", "RELEASE", "REVISION", "APPLIED", "ERROR")
	}
	for _, cr := range crs {
		if c.Wide {
			t.Row(
				cr.ID, cr.ClusterID, displayOrDash(cr.Namespace), displayOrDash(cr.Name),
				displayOrDash(cr.Release), displayOrDash(cr.Revision), displayOrDash(cr.AppliedRevision),
				displayOrDash(cr.ApplyError), displayOrDash(cr.Spec),
			)
			continue
		}
		t.Row(
			cr.ID, cr.ClusterID, displayOrDash(cr.Namespace), displayOrDash(cr.Name),
			displayOrDash(cr.Release), displayOrDash(cr.Revision), displayOrDash(cr.AppliedRevision),
			displayOrDash(cr.ApplyError),
		)
	}
	return t.Render(w)
}

// Decode is not supported for table format.
func (c *CollectorCRTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

func displayOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
