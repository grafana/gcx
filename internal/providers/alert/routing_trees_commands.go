package alert

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	routingTreesGroup = "notifications.alerting.grafana.app"
	routingTreeKind   = "RoutingTree"

	// provenanceAnnotation records who manages a tree (e.g. "api", "file").
	provenanceAnnotation = "grafana.com/provenance"
)

// defaultRoutingTreeNames are the names Grafana accepts for the default tree.
// Deleting the default tree resets it rather than removing it.
//
//nolint:gochecknoglobals
var defaultRoutingTreeNames = []string{"user-defined", "default"}

const apiVersionFlagUsage = "API version to use (e.g. " + routingTreesGroup + "/v1beta1); defaults to the server's preferred version"

// routingTreesCommands returns the routing-trees command group.
func routingTreesCommands(loader GrafanaConfigLoader) *cobra.Command {
	return newRoutingTreesCommand(native.Bind(loader, native.Config{
		Group:    routingTreesGroup,
		Resource: "routingtrees",
	}))
}

func newRoutingTreesCommand(binding native.Binding) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "routing-trees",
		Short:   "Manage notification routing trees (default and named).",
		Aliases: []string{"routing-tree"},
		Long: `Manage notification routing trees through Grafana's native
notifications.alerting.grafana.app API.

The default tree is named "user-defined". Named trees require Grafana 13.1+,
or 12.4-13.0 with the alertingMultiplePolicies feature toggle. Server errors
are reported unchanged.`,
	}
	cmd.AddCommand(
		newRoutingTreesListCommand(binding),
		newRoutingTreesGetCommand(binding),
		newRoutingTreesCreateCommand(binding),
		newRoutingTreesUpdateCommand(binding),
		newRoutingTreesDeleteCommand(binding),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

type routingTreesListOpts struct {
	IO         cmdio.Options
	APIVersion string
}

func (o *routingTreesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &routingTreeTableCodec{})
	o.IO.RegisterCustomCodec("wide", &routingTreeTableCodec{wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.APIVersion, "api-version", "", apiVersionFlagUsage)
}

func (o *routingTreesListOpts) Validate(cmd *cobra.Command) error {
	if err := validateAPIVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}

func newRoutingTreesListCommand(binding native.Binding) *cobra.Command {
	opts := &routingTreesListOpts{}
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List routing trees, including the default tree.",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			access, err := binding.Load(ctx, native.LoadOptions{APIVersion: opts.APIVersion})
			if err != nil {
				return err
			}
			list, err := access.Client.List(ctx, access.Descriptor, metav1.ListOptions{})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), list)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

type routingTreesGetOpts struct {
	IO         cmdio.Options
	APIVersion string
}

func (o *routingTreesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.APIVersion, "api-version", "", apiVersionFlagUsage)
}

func (o *routingTreesGetOpts) Validate(cmd *cobra.Command) error {
	if err := validateAPIVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}

func newRoutingTreesGetCommand(binding native.Binding) *cobra.Command {
	opts := &routingTreesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a routing tree manifest by name.",
		Long: `Get a routing tree's native manifest by name. The default tree is
"user-defined". The name is sent to the server as given.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			access, err := binding.Load(ctx, native.LoadOptions{APIVersion: opts.APIVersion})
			if err != nil {
				return err
			}
			item, err := access.Client.Get(ctx, access.Descriptor, args[0], metav1.GetOptions{})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), item)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

type routingTreesWriteOpts struct {
	IO         cmdio.Options
	APIVersion string
	Filename   string
}

func (o *routingTreesWriteOpts) setup(flags *pflag.FlagSet) {
	// The default text codec prints a one-line receipt; agent mode and
	// explicit -o json/yaml get the structured SingleMutation document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{line: routingTreeMutationLine})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to a JSON/YAML RoutingTree manifest ('-' reads from stdin)")
	flags.StringVar(&o.APIVersion, "api-version", "", "Must match the manifest's apiVersion when set; the manifest decides the version")
}

func (o *routingTreesWriteOpts) Validate(cmd *cobra.Command) error {
	if o.Filename == "" {
		return fail.NewCommandUsageError(cmd, "--filename / -f is required", nil)
	}
	if err := validateAPIVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}

func newRoutingTreesCreateCommand(binding native.Binding) *cobra.Command {
	opts := &routingTreesWriteOpts{}
	cmd := &cobra.Command{
		Use:   "create -f <file>",
		Short: "Create a named routing tree from a manifest.",
		Long: `Create a routing tree from a native RoutingTree manifest. Create never
updates an existing tree; an existing or reserved name ("user-defined",
"default") fails with the server's conflict error.`,
		Example: `  gcx alert routing-trees create -f team-a.yaml`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			obj, err := native.ReadManifest(opts.Filename, cmd.InOrStdin())
			if err != nil {
				return err
			}
			version, err := manifestVersion(cmd, obj, opts.APIVersion)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			access, err := binding.Load(ctx, native.LoadOptions{APIVersion: version})
			if err != nil {
				return err
			}
			created, err := access.Client.Create(ctx, access.Descriptor, obj, metav1.CreateOptions{})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), newRoutingTreeMutation("created", created.GetName(), created))
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("filename")
	return cmd
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func newRoutingTreesUpdateCommand(binding native.Binding) *cobra.Command {
	opts := &routingTreesWriteOpts{}
	cmd := &cobra.Command{
		Use:   "update <name> -f <file>",
		Short: "Replace one routing tree from a manifest.",
		Long: `Replace one routing tree from a native RoutingTree manifest. Other trees
are unchanged.

The manifest must carry the metadata.resourceVersion from a recent get. If the
tree changed on the server since then, the update fails with a conflict.`,
		Example: `  gcx alert routing-trees get team-a -o yaml > team-a.yaml
  # edit team-a.yaml
  gcx alert routing-trees update team-a -f team-a.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			name := args[0]
			obj, err := native.ReadManifest(opts.Filename, cmd.InOrStdin())
			if err != nil {
				return err
			}
			version, err := manifestVersion(cmd, obj, opts.APIVersion)
			if err != nil {
				return err
			}
			if obj.GetName() != name {
				return fail.NewCommandUsageError(cmd,
					fmt.Sprintf("name argument %q does not match the manifest's metadata.name %q", name, obj.GetName()), nil)
			}
			if obj.GetResourceVersion() == "" {
				usageErr := fail.NewCommandUsageError(cmd, "the manifest has no metadata.resourceVersion", nil)
				usageErr.Suggestions = []string{
					fmt.Sprintf("Fetch the current tree first: gcx alert routing-trees get %s -o yaml > tree.yaml", name),
				}
				return usageErr
			}

			ctx := cmd.Context()
			access, err := binding.Load(ctx, native.LoadOptions{APIVersion: version})
			if err != nil {
				return err
			}
			updated, err := access.Client.Update(ctx, access.Descriptor, obj, metav1.UpdateOptions{})
			if err != nil {
				return wrapRoutingTreeConflict(name, err)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), newRoutingTreeMutation("updated", updated.GetName(), updated))
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("filename")
	return cmd
}

// wrapRoutingTreeConflict adds the re-fetch step to a stale-version conflict.
// The server's error stays in the chain.
func wrapRoutingTreeConflict(name string, err error) error {
	if !apierrors.IsConflict(err) {
		return err
	}
	return fmt.Errorf("%w\n\nthe routing tree changed after you fetched it; re-run "+
		"'gcx alert routing-trees get %s -o yaml', re-apply your edits, and update again", err, name)
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

type routingTreesDeleteOpts struct {
	IO         cmdio.Options
	APIVersion string
	Force      bool
}

func (o *routingTreesDeleteOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{line: routingTreeMutationLine})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	flags.StringVar(&o.APIVersion, "api-version", "", apiVersionFlagUsage)
}

func (o *routingTreesDeleteOpts) Validate(cmd *cobra.Command) error {
	if err := validateAPIVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}

func newRoutingTreesDeleteCommand(binding native.Binding) *cobra.Command {
	opts := &routingTreesDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a named routing tree, or reset the default tree.",
		Long: `Delete a named routing tree. Deleting the default tree ("user-defined")
resets it to Grafana's built-in configuration instead of removing it.`,
		Aliases: []string{"rm"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			name := args[0]
			reset := isDefaultRoutingTree(name)

			prompt := fmt.Sprintf("Delete routing tree %q?", name)
			if reset {
				prompt = fmt.Sprintf("Reset the default routing tree %q to Grafana's built-in configuration?", name)
			}
			// The prompt and the "Aborted." note are diagnostics, so they go to stderr.
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, prompt)
			if err != nil {
				return err
			}
			if !proceed {
				return cancelledRoutingTreeDelete(name)
			}

			ctx := cmd.Context()
			access, err := binding.Load(ctx, native.LoadOptions{APIVersion: opts.APIVersion})
			if err != nil {
				return err
			}
			if err := access.Client.Delete(ctx, access.Descriptor, name, metav1.DeleteOptions{}); err != nil {
				return err
			}

			action := "deleted"
			if reset {
				action = "reset"
			}
			return opts.IO.Encode(cmd.OutOrStdout(), newRoutingTreeMutation(action, name, nil))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func isDefaultRoutingTree(name string) bool {
	return slices.Contains(defaultRoutingTreeNames, name)
}

func cancelledRoutingTreeDelete(name string) error {
	return &gcxerrors.DetailedError{
		Summary:  "delete cancelled",
		Details:  fmt.Sprintf("Confirmation prompt was declined; routing tree %q was not changed.", name),
		ExitCode: new(gcxerrors.ExitCancelled),
	}
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// validateAPIVersionFlag rejects an --api-version outside the routing-trees
// group before any request.
func validateAPIVersionFlag(cmd *cobra.Command, apiVersion string) error {
	if _, err := native.ParseAPIVersion(routingTreesGroup, apiVersion); err != nil {
		return fail.NewCommandUsageError(cmd, "--api-version: "+err.Error(), nil)
	}
	return nil
}

// manifestVersion checks the manifest's group and kind and returns the
// version it selects. A set --api-version must name the same version.
func manifestVersion(cmd *cobra.Command, obj *unstructured.Unstructured, flagAPIVersion string) (string, error) {
	gv, err := schema.ParseGroupVersion(obj.GetAPIVersion())
	if err != nil || gv.Group != routingTreesGroup || gv.Version == "" {
		return "", fail.NewCommandUsageError(cmd,
			fmt.Sprintf("manifest apiVersion %q must be %s/<version>", obj.GetAPIVersion(), routingTreesGroup), nil)
	}
	if obj.GetKind() != routingTreeKind {
		return "", fail.NewCommandUsageError(cmd,
			fmt.Sprintf("manifest kind %q must be %s", obj.GetKind(), routingTreeKind), nil)
	}
	if flagAPIVersion != "" {
		// validateAPIVersionFlag already rejected a bad group.
		flagVersion, _ := native.ParseAPIVersion(routingTreesGroup, flagAPIVersion)
		if flagVersion != gv.Version {
			return "", fail.NewCommandUsageError(cmd,
				fmt.Sprintf("--api-version %q conflicts with the manifest's apiVersion %q; the manifest decides the version", flagAPIVersion, obj.GetAPIVersion()), nil)
		}
	}
	return gv.Version, nil
}

func newRoutingTreeMutation(action, name string, obj *unstructured.Unstructured) cmdio.SingleMutation {
	target := cmdio.MutationTarget{Kind: routingTreeKind, Name: name}
	if obj != nil {
		target.UID = string(obj.GetUID())
		target.Namespace = obj.GetNamespace()
	}
	result := cmdio.NewSingleMutation(action, target)
	// Create and delete always change state. Update and reset may write
	// what was already there, and the server does not say.
	if action == "created" || action == "deleted" {
		changed := true
		result.Changed = &changed
	}
	return result
}

func routingTreeMutationLine(m cmdio.SingleMutation) string {
	if m.Action == "reset" {
		return fmt.Sprintf("default routing tree %q reset", m.Target.Name)
	}
	return fmt.Sprintf("routing tree %q %s", m.Target.Name, m.Action)
}

// routingTreeTableCodec renders routing trees as a table.
//
// Default columns: NAME  RECEIVER  ROUTES
// Wide columns:    NAME  RECEIVER  ROUTES  PROVENANCE
//
// Missing fields render empty, which tolerates v0alpha1 schema differences.
type routingTreeTableCodec struct {
	wide bool
}

func (c *routingTreeTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *routingTreeTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

func (c *routingTreeTableCodec) Encode(w io.Writer, v any) error {
	var items []unstructured.Unstructured
	switch val := v.(type) {
	case *unstructured.UnstructuredList:
		items = val.Items
	case *unstructured.Unstructured:
		items = []unstructured.Unstructured{*val}
	default:
		return fmt.Errorf("routing tree table: unsupported type %T", v)
	}

	headers := []string{"NAME", "RECEIVER", "ROUTES"}
	if c.wide {
		headers = append(headers, "PROVENANCE")
	}
	t := style.NewTable(headers...)

	for _, item := range items {
		receiver, _, _ := unstructured.NestedString(item.Object, "spec", "defaults", "receiver")
		routes := ""
		if r, found, _ := unstructured.NestedSlice(item.Object, "spec", "routes"); found {
			routes = strconv.Itoa(len(r))
		}
		row := []string{item.GetName(), receiver, routes}
		if c.wide {
			row = append(row, item.GetAnnotations()[provenanceAnnotation])
		}
		t.Row(row...)
	}

	return t.Render(w)
}
