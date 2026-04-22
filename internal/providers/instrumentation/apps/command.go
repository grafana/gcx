package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/shared"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the apps parent cobra command with list/get/create/update/delete subcommands.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apps",
		Short:   "Manage instrumentation app configurations.",
		Aliases: []string{"app"},
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

type listOpts struct {
	IO      cmdio.Options
	Limit   int64
	Cluster string
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for unlimited)")
	flags.StringVar(&o.Cluster, "cluster", "", "Filter by cluster name (direct call, skips fan-out)")
}

// Validate checks that IO options are valid.
func (o *listOpts) Validate() error {
	return o.IO.Validate()
}

func newListCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List instrumentation app configurations.",
		Long: `List instrumentation app configurations.

Without --cluster, fans out one API call per connected cluster (bounded concurrency, default 10). For large deployments with many clusters, this may take several seconds.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			ctx := cmd.Context()

			if opts.Cluster != "" {
				r, err := fleet.LoadClientWithStack(ctx, loader)
				if err != nil {
					return fmt.Errorf("instrumentation: %w", err)
				}
				client := instrumentation.NewClient(r.Client)
				resp, err := client.GetAppInstrumentation(ctx, opts.Cluster)
				if err != nil {
					return fmt.Errorf("instrumentation: %w", err)
				}
				desc := instrumentation.AppDescriptor()
				objs := make([]adapter.TypedObject[instrumentation.App], 0, len(resp.Namespaces))
				for _, ns := range resp.Namespaces {
					app := nsToApp(opts.Cluster, ns)
					obj := adapter.TypedObject[instrumentation.App]{}
					obj.APIVersion = desc.GroupVersion.String()
					obj.Kind = desc.Kind
					obj.SetName(app.GetResourceName())
					obj.Spec = app
					objs = append(objs, obj)
				}
				// F14: honor --limit in the --cluster fan-out path.
				objs = adapter.TruncateSlice(objs, opts.Limit)
				return encodeListWithFooter(cmd, opts.IO, objs, opts.Cluster != "")
			}

			crud, err := instrumentation.NewAppTypedCRUD(ctx, loader)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}
			objs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}
			return encodeListWithFooter(cmd, opts.IO, objs, false)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// Validate checks that IO options are valid.
func (o *getOpts) Validate() error {
	return o.IO.Validate()
}

func newGetCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get an instrumentation app configuration by name.",
		Example: `  # Name is in cluster-namespace form.
  gcx instrumentation apps get prod-east-payments`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			ctx := cmd.Context()
			name := args[0]

			crud, err := instrumentation.NewAppTypedCRUD(ctx, loader)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			obj, err := crud.Get(ctx, name)
			if err != nil {
				if errors.Is(err, adapter.ErrNotFound) {
					var footer instrumentation.Footer
					footer.Hint("to verify the app name", "gcx instrumentation apps list")
					footer.Hint("to check if the collector has reported", "gcx instrumentation status")
					footer.Print(cmd.ErrOrStderr())
				}
				return fmt.Errorf("instrumentation: %w", err)
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), []adapter.TypedObject[instrumentation.App]{*obj}); err != nil {
				return err
			}
			var footer instrumentation.Footer
			footer.Notef("data reflects Alloy collector-reported state (refresh ~30s).")
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

type createOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the App manifest (use - for stdin)")
}

func (o *createOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newCreateCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an instrumentation app configuration from a file.",
		Example: `  gcx instrumentation apps create -f app.yaml
  cat app.yaml | gcx instrumentation apps create -f -`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			metadataName, app, err := readAppManifest(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			if err := instrumentation.ValidateAppIdentityName(metadataName, app.Cluster, app.Namespace); err != nil {
				return err
			}

			crud, err := instrumentation.NewAppTypedCRUD(ctx, loader)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			typedObj := &adapter.TypedObject[instrumentation.App]{Spec: *app}
			typedObj.SetName(app.GetResourceName())

			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), created); err != nil {
				return err
			}
			var footer instrumentation.Footer
			footer.Notef("App %q is persisted; list/get shows Alloy collector-reported state and may take ~30s to refresh.", app.GetResourceName())
			footer.Hint("to verify", "gcx instrumentation status --cluster "+app.Cluster)
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

type updateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the App manifest (use - for stdin)")
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newUpdateCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:     "update <name>",
		Short:   "Update an instrumentation app configuration from a file.",
		Example: `  gcx instrumentation apps update prod-east-payments -f app.yaml`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			name := args[0]

			metadataName, app, err := readAppManifest(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			if err := instrumentation.ValidateAppIdentityName(metadataName, app.Cluster, app.Namespace); err != nil {
				return err
			}

			crud, err := instrumentation.NewAppTypedCRUD(ctx, loader)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			typedObj := &adapter.TypedObject[instrumentation.App]{Spec: *app}
			typedObj.SetName(name)

			updated, err := crud.Update(ctx, name, typedObj)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), updated); err != nil {
				return err
			}
			var footer instrumentation.Footer
			footer.Notef("App %q is persisted; list/get shows Alloy collector-reported state and may take ~30s to refresh.", name)
			footer.Hint("to verify", "gcx instrumentation status --cluster "+app.Cluster)
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

// deleteOpts holds options for the delete subcommand. Empty today, but the
// options pattern (setup/Validate) is mandatory per CONSTITUTION taste rules
// and gives future flags a clear home.
type deleteOpts struct{}

func (o *deleteOpts) setup(_ *pflag.FlagSet) {}

func (o *deleteOpts) Validate() error { return nil }

// newDeleteCommand creates the delete subcommand.
// No --output flag: delete is destructive and matches the clusters delete pattern
// (clusters delete also has no --output). The adapter already prefixes errors with
// "instrumentation: " so no additional wrapping is needed on the delete path.
func newDeleteCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an instrumentation app configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			name := args[0]

			crud, err := instrumentation.NewAppTypedCRUD(ctx, loader)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			if err := crud.Delete(ctx, name); err != nil {
				return err
			}
			var footer instrumentation.Footer
			footer.Notef("App %q deletion is persisted; list/get shows Alloy collector-reported state and may take ~30s to refresh.", name)
			footer.Hint("to verify", "gcx instrumentation status")
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// table codec
// ---------------------------------------------------------------------------

// TableCodec renders []adapter.TypedObject[instrumentation.App] as a table.
type TableCodec struct {
	Wide bool
}

// Format returns the codec's format identifier.
func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes the app list as a table.
func (c *TableCodec) Encode(w io.Writer, v any) error {
	objs, ok := v.([]adapter.TypedObject[instrumentation.App])
	if !ok {
		return errors.New("invalid data type for table codec: expected []TypedObject[App]")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("CLUSTER", "NAMESPACE", "SELECTION", "TRACING", "LOGGING", "PROCESS METRICS", "EXTENDED METRICS", "PROFILING")
		for _, obj := range objs {
			a := obj.Spec
			t.Row(a.Cluster, a.Namespace, shared.ValOrDash(a.Selection),
				shared.BoolStr(a.Tracing), shared.BoolStr(a.Logging),
				shared.BoolStr(a.ProcessMetrics), shared.BoolStr(a.ExtendedMetrics),
				shared.BoolStr(a.Profiling))
		}
	} else {
		t = style.NewTable("CLUSTER", "NAMESPACE", "TRACING", "LOGGING", "PROFILING")
		for _, obj := range objs {
			a := obj.Spec
			t.Row(a.Cluster, a.Namespace, shared.BoolStr(a.Tracing), shared.BoolStr(a.Logging), shared.BoolStr(a.Profiling))
		}
	}
	return t.Render(w)
}

// Decode is not supported for table format.
func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// readAppManifest decodes a YAML/JSON App manifest from file (or stdin if "-").
// Returns the raw metadata.name (may be empty), the decoded App spec, and any error.
func readAppManifest(file string, stdin io.Reader) (string, *instrumentation.App, error) {
	var r io.Reader
	if file == "-" {
		r = stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return "", nil, fmt.Errorf("failed to open file %s: %w", file, err)
		}
		defer f.Close()
		r = f
	}

	yamlCodec := format.NewYAMLCodec()
	var obj unstructured.Unstructured
	if err := yamlCodec.Decode(r, &obj); err != nil {
		return "", nil, fmt.Errorf("failed to parse input: %w", err)
	}

	specRaw, ok := obj.Object["spec"]
	if !ok {
		return "", nil, errors.New("manifest is missing spec field")
	}

	specJSON, err := json.Marshal(specRaw)
	if err != nil {
		return "", nil, fmt.Errorf("failed to encode spec: %w", err)
	}

	var app instrumentation.App
	if err := json.Unmarshal(specJSON, &app); err != nil {
		return "", nil, fmt.Errorf("failed to parse spec: %w", err)
	}

	if app.Cluster == "" {
		return "", nil, errors.New("manifest spec.cluster is required")
	}
	if app.Namespace == "" {
		return "", nil, errors.New("manifest spec.namespace is required")
	}

	return obj.GetName(), &app, nil
}

// encodeListWithFooter emits the primary result, then a Footer with the
// eventual-consistency note and an empty-state hint when no apps match.
// clusterScoped is true when the caller filtered to a specific cluster.
func encodeListWithFooter(cmd *cobra.Command, io cmdio.Options, objs []adapter.TypedObject[instrumentation.App], clusterScoped bool) error {
	if err := io.Encode(cmd.OutOrStdout(), objs); err != nil {
		return err
	}
	var footer instrumentation.Footer
	if len(objs) == 0 {
		footer.Notef("no apps found.")
	}
	footer.Notef("data reflects Alloy collector-reported state (refresh ~30s).")
	if len(objs) == 0 {
		if clusterScoped {
			footer.Hint("to list registered clusters", "gcx instrumentation status")
		} else {
			footer.Hint("to define an app", "gcx instrumentation apps create --help")
		}
	}
	footer.Print(cmd.ErrOrStderr())
	return nil
}

// nsToApp converts a NamespaceConfig to an App for the given cluster.
func nsToApp(clusterName string, ns instrumentation.NamespaceConfig) instrumentation.App {
	apps := make([]instrumentation.AppConfig, len(ns.Apps))
	for i, a := range ns.Apps {
		apps[i] = instrumentation.AppConfig{Name: a.Name, Selection: a.Selection, Type: a.Type}
	}
	return instrumentation.App{
		Cluster:         clusterName,
		Namespace:       ns.Name,
		Selection:       ns.Selection,
		Tracing:         ns.Tracing,
		Logging:         ns.Logging,
		ProcessMetrics:  ns.ProcessMetrics,
		ExtendedMetrics: ns.ExtendedMetrics,
		Profiling:       ns.Profiling,
		Apps:            apps,
	}
}
