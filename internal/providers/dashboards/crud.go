package dashboards

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/dashboards/descriptor"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GrafanaConfigLoader is the subset of providers.ConfigLoader used by CRUD commands.
// Defined as a local interface so commands can be tested with a stub.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// DashboardMutationClient is the subset of dynamic.NamespacedClient used by
// the mutation commands (create, update, delete). Defined as a local
// interface so the commands can be tested with a fake client and no real
// K8s connectivity (same seam as versions.DashboardVersionsClient).
type DashboardMutationClient interface {
	Create(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions) (*unstructured.Unstructured, error)
	Update(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error)
	Delete(ctx context.Context, desc resources.Descriptor, name string, opts metav1.DeleteOptions) error
}

// mutationDeps holds either a real config loader (production path) or a
// pre-built client+descriptor (test path). Tests set client+desc; production
// sets loader.
type mutationDeps struct {
	loader GrafanaConfigLoader

	// Test overrides: if client is non-nil, client+desc are used directly
	// and loader is never called.
	client DashboardMutationClient
	desc   resources.Descriptor
}

// resolve returns a client and descriptor, either from the pre-built test
// overrides or by calling descriptor.Resolve + dynamic.NewDefaultNamespacedClient.
func (d *mutationDeps) resolve(ctx context.Context, apiVersion string) (DashboardMutationClient, resources.Descriptor, error) {
	if d.client != nil {
		return d.client, d.desc, nil
	}

	cfg, err := d.loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, resources.Descriptor{}, err
	}

	desc, err := descriptor.Resolve(ctx, cfg, apiVersion)
	if err != nil {
		return nil, resources.Descriptor{}, err
	}

	client, err := dynamic.NewDefaultNamespacedClient(cfg)
	if err != nil {
		return nil, resources.Descriptor{}, err
	}

	return client, desc, nil
}

// singleMutationTextCodec is the human "text" codec for SingleMutation values
// produced by the dashboard mutation commands. It renders exactly the
// one-line receipt these commands have always printed
// ("✔ dashboard %q created|updated|deleted"), so default human stdout stays
// byte-identical to the pre-codec output. Agent mode (agents codec) and
// explicit -o json/yaml get the structured document instead.
type singleMutationTextCodec struct{}

func (c *singleMutationTextCodec) Format() format.Format { return "text" }

func (c *singleMutationTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *singleMutationTextCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(cmdio.SingleMutation)
	if !ok {
		return errors.New("invalid data type for mutation text codec: expected SingleMutation")
	}

	cmdio.Success(w, "dashboard %q %s", result.Target.Name, result.Action)
	return nil
}

// newDashboardMutation builds the shared SingleMutation result for a
// dashboard mutation. obj may be nil (delete has no returned object).
//
// Changed is set only where the command can actually tell: a successful
// create or delete is always a real state change, while an update PUT may
// have written an identical manifest — the server does not report no-ops, so
// update leaves Changed nil (omitted).
func newDashboardMutation(action string, desc resources.Descriptor, name string, obj *unstructured.Unstructured) cmdio.SingleMutation {
	target := cmdio.MutationTarget{Kind: desc.Kind, Name: name}
	if obj != nil {
		target.UID = string(obj.GetUID())
		target.Namespace = obj.GetNamespace()
	}

	result := cmdio.NewSingleMutation(action, target)
	if action == "created" || action == "deleted" {
		changed := true
		result.Changed = &changed
	}
	return result
}

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

const defaultDashboardListLimit int64 = 50

type listOpts struct {
	IO            cmdio.Options
	APIVersion    string
	Limit         int64
	ContinueToken string
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", newDashboardTableCodec(false, ""))
	o.IO.RegisterCustomCodec("wide", newDashboardTableCodec(true, ""))
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
	flags.Int64Var(&o.Limit, "limit", defaultDashboardListLimit, "Maximum number of dashboards to return in one page (0 fetches all pages)")
	flags.StringVar(&o.ContinueToken, "continue", "", "Continue token for the next page (requires --limit > 0; use the token shown by a previous limited response)")
}

func (o *listOpts) Validate() error {
	if o.Limit < 0 {
		return fmt.Errorf("--limit must be >= 0, got %d", o.Limit)
	}
	if o.ContinueToken != "" && o.Limit == 0 {
		return errors.New("--continue requires --limit > 0")
	}
	return o.IO.Validate()
}

// newListCommand returns the `dashboards list` subcommand.
func newListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &listOpts{}

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List dashboards",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			desc, err := descriptor.Resolve(ctx, cfg, opts.APIVersion)
			if err != nil {
				return err
			}

			client, err := dynamic.NewDefaultNamespacedClient(cfg)
			if err != nil {
				return err
			}

			list, err := client.List(ctx, desc, metav1.ListOptions{Limit: opts.Limit, Continue: opts.ContinueToken})
			if err != nil {
				return err
			}

			// Wide output needs the Grafana URL for link synthesis. Re-register
			// with runtime context after cfg is loaded.
			if opts.IO.OutputFormat == "wide" {
				opts.IO.RegisterCustomCodec("wide", newDashboardTableCodec(true, cfg.GrafanaURL))
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), list); err != nil {
				return err
			}

			emitListPaginationHint(cmd.ErrOrStderr(), os.Args, list, opts)
			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func emitListPaginationHint(w io.Writer, argv []string, list *unstructured.UnstructuredList, opts *listOpts) {
	if list.GetContinue() == "" || !shouldEmitListPaginationHint(opts.IO.OutputFormat) {
		return
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = int64(len(list.Items))
	}

	cmdio.EmitHint(
		w,
		fmt.Sprintf("showing %d dashboards; more pages are available", len(list.Items)),
		cmdio.BuildPaginationCommand(argv, limit, list.GetContinue()),
	)
}

func shouldEmitListPaginationHint(outputFormat string) bool {
	return outputFormat == "table" || outputFormat == "wide"
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type getOpts struct {
	IO         cmdio.Options
	APIVersion string
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", newDashboardTableCodec(false, ""))
	o.IO.RegisterCustomCodec("wide", newDashboardTableCodec(true, ""))
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *getOpts) Validate() error {
	return o.IO.Validate()
}

// newGetCommand returns the `dashboards get <name>` subcommand.
func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &getOpts{}

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a dashboard by name",
		Long:  "Get a Grafana dashboard by its Kubernetes resource name.\n\nThe `name` argument equals the legacy Dashboard UID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			desc, err := descriptor.Resolve(ctx, cfg, opts.APIVersion)
			if err != nil {
				return err
			}

			client, err := dynamic.NewDefaultNamespacedClient(cfg)
			if err != nil {
				return err
			}

			item, err := client.Get(ctx, desc, args[0], metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Wide codec needs the Grafana URL for link synthesis.
			// Re-register with the real URL after cfg is loaded.
			if opts.IO.OutputFormat == "wide" {
				opts.IO.RegisterCustomCodec("wide", newDashboardTableCodec(true, cfg.GrafanaURL))
			}

			return opts.IO.Encode(cmd.OutOrStdout(), item)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// ---------------------------------------------------------------------------
// create command
// ---------------------------------------------------------------------------

type createOpts struct {
	IO         cmdio.Options
	APIVersion string
	Filename   string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	// The create result is a SingleMutation document through the codec
	// system: the default text codec prints the familiar one-line receipt;
	// agent mode and explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to JSON/YAML manifest file ('-' reads from stdin)")
	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *createOpts) Validate() error {
	if o.Filename == "" {
		return errors.New("--filename / -f is required")
	}
	return o.IO.Validate()
}

// newCreateCommand returns the `dashboards create` subcommand.
// It reads a JSON or YAML manifest from a file (-f) or stdin.
func newCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	return newCreateCommandWithDeps(&mutationDeps{loader: loader})
}

func newCreateCommandWithDeps(deps *mutationDeps) *cobra.Command {
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use:   "create -f <file>",
		Short: "Create a dashboard from a manifest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, desc, err := deps.resolve(ctx, opts.APIVersion)
			if err != nil {
				return err
			}

			obj, err := readManifest(opts.Filename)
			if err != nil {
				return err
			}

			created, err := client.Create(ctx, desc, obj, metav1.CreateOptions{})
			if err != nil {
				return err
			}

			result := newDashboardMutation("created", desc, created.GetName(), created)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("filename")

	return cmd
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

type updateOpts struct {
	IO         cmdio.Options
	APIVersion string
	Filename   string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	// The update result is a SingleMutation document through the codec
	// system: the default text codec prints the familiar one-line receipt;
	// agent mode and explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to JSON/YAML manifest file ('-' reads from stdin)")
	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *updateOpts) Validate() error {
	if o.Filename == "" {
		return errors.New("--filename / -f is required")
	}
	return o.IO.Validate()
}

// newUpdateCommand returns the `dashboards update <name>` subcommand.
func newUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	return newUpdateCommandWithDeps(&mutationDeps{loader: loader})
}

func newUpdateCommandWithDeps(deps *mutationDeps) *cobra.Command {
	opts := &updateOpts{}

	cmd := &cobra.Command{
		Use:   "update <name> -f <file>",
		Short: "Update a dashboard from a manifest",
		Long: `Update a Grafana dashboard from a JSON or YAML manifest.

The manifest must include metadata.resourceVersion captured by a recent
'gcx dashboards get'. The server uses it for optimistic concurrency: if
the dashboard has been modified by another writer since the manifest was
fetched, the update fails with a conflict error and the hint to re-fetch.

Recommended workflow:

  gcx dashboards get <name> -o yaml > dashboard.yaml
  # edit dashboard.yaml
  gcx dashboards update <name> -f dashboard.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, desc, err := deps.resolve(ctx, opts.APIVersion)
			if err != nil {
				return err
			}

			obj, err := readManifest(opts.Filename)
			if err != nil {
				return err
			}

			// Ensure the name in the manifest matches the CLI argument.
			obj.SetName(args[0])

			updated, err := client.Update(ctx, desc, obj, metav1.UpdateOptions{})
			if err != nil {
				return wrapUpdateError(args[0], err)
			}

			result := newDashboardMutation("updated", desc, updated.GetName(), updated)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("filename")

	return cmd
}

// ---------------------------------------------------------------------------
// delete command
// ---------------------------------------------------------------------------

type deleteOpts struct {
	IO         cmdio.Options
	APIVersion string
	Force      bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	// The delete result is a SingleMutation document through the codec
	// system: the default text codec prints the familiar one-line receipt;
	// agent mode and explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *deleteOpts) Validate() error {
	return o.IO.Validate()
}

// newDeleteCommand returns the `dashboards delete <name>` subcommand.
// It prompts for confirmation unless --force is passed.
func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	return newDeleteCommandWithDeps(&mutationDeps{loader: loader})
}

func newDeleteCommandWithDeps(deps *mutationDeps) *cobra.Command {
	opts := &deleteOpts{}

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Short:   "Delete a dashboard",
		Aliases: []string{"rm"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			name := args[0]

			// The prompt and the "Aborted." note are interaction
			// diagnostics, not the result — stderr keeps them out of the
			// stdout document (Constitution: stdout is the result).
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete dashboard %q?", name))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			ctx := cmd.Context()
			client, desc, err := deps.resolve(ctx, opts.APIVersion)
			if err != nil {
				return err
			}

			if err := client.Delete(ctx, desc, name, metav1.DeleteOptions{}); err != nil {
				return err
			}

			result := newDashboardMutation("deleted", desc, name, nil)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// wrapUpdateError augments an Update error with workflow guidance when the
// server reports an optimistic-concurrency conflict, so users see the next
// step (re-fetch) rather than a bare K8s status message.
func wrapUpdateError(name string, err error) error {
	if err == nil {
		return nil
	}
	if !apierrors.IsConflict(err) {
		return err
	}
	return fmt.Errorf(
		"%w\n\nthe dashboard was modified after you fetched it; re-run "+
			"'gcx dashboards get %s -o yaml' to capture the latest "+
			"metadata.resourceVersion, re-apply your edits, and update again",
		err, name,
	)
}

// readManifest reads an unstructured K8s object from the given file path
// or from stdin when filename is "-".
func readManifest(filename string) (*unstructured.Unstructured, error) {
	if filename == "" {
		return nil, errors.New("--filename / -f is required")
	}

	var reader io.Reader
	if filename == "-" {
		reader = io.LimitReader(os.Stdin, 32<<20)
	} else {
		f, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to open %q: %w", filename, err)
		}
		defer f.Close()
		reader = f
	}

	return decodeManifest(reader)
}

// decodeManifest decodes a JSON or YAML manifest into an unstructured object.
// It tries JSON first, then falls back to YAML via the format package codec.
func decodeManifest(r io.Reader) (*unstructured.Unstructured, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Detect format upfront: JSON objects/arrays start with '{' or '['.
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		// Treat as JSON and surface the parse error directly.
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(data); err != nil {
			return nil, fmt.Errorf("failed to parse JSON manifest: %w", err)
		}
		return obj, nil
	}

	// Fall back to YAML: decode into map[string]any, then re-encode as JSON.
	var rawObj map[string]any
	yamlCodec := format.NewYAMLCodec()
	if yamlErr := yamlCodec.Decode(bytes.NewReader(data), &rawObj); yamlErr != nil {
		return nil, fmt.Errorf("manifest is neither valid JSON nor YAML: %w", yamlErr)
	}

	obj2 := &unstructured.Unstructured{Object: rawObj}
	return obj2, nil
}
