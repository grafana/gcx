package dashboards

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/dashboards/descriptor"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GrafanaConfigLoader is the subset of providers.ConfigLoader used by CRUD commands.
// Defined as a local interface so commands can be tested with a stub.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type listOpts struct {
	IO         cmdio.Options
	APIVersion string
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", newDashboardTableCodec(false, ""))
	o.IO.RegisterCustomCodec("wide", newDashboardTableCodec(true, ""))
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *listOpts) Validate() error {
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

			list, err := client.List(ctx, desc, metav1.ListOptions{})
			if err != nil {
				return err
			}

			// Wide codec needs the Grafana URL for link synthesis.
			// Re-register with the real URL after cfg is loaded.
			if opts.IO.OutputFormat == "wide" {
				opts.IO.RegisterCustomCodec("wide", newDashboardTableCodec(true, cfg.GrafanaURL))
			}

			return opts.IO.Encode(cmd.OutOrStdout(), list)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
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
	APIVersion string
	Filename   string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to JSON/YAML manifest file ('-' reads from stdin)")
	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *createOpts) Validate() error {
	if o.Filename == "" {
		return errors.New("--filename / -f is required")
	}
	return nil
}

// newCreateCommand returns the `dashboards create` subcommand.
// It reads a JSON or YAML manifest from a file (-f) or stdin.
func newCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use:   "create -f <file>",
		Short: "Create a dashboard from a manifest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			obj, err := readManifest(opts.Filename)
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

			created, err := client.Create(ctx, desc, obj, metav1.CreateOptions{})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "dashboard %q created", created.GetName())
			return nil
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
	APIVersion string
	Filename   string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to JSON/YAML manifest file ('-' reads from stdin)")
	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *updateOpts) Validate() error {
	if o.Filename == "" {
		return errors.New("--filename / -f is required")
	}
	return nil
}

// newUpdateCommand returns the `dashboards update <name>` subcommand.
func newUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &updateOpts{}

	cmd := &cobra.Command{
		Use:   "update <name> -f <file>",
		Short: "Update a dashboard from a manifest",
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

			obj, err := readManifest(opts.Filename)
			if err != nil {
				return err
			}

			// Ensure the name in the manifest matches the CLI argument.
			obj.SetName(args[0])

			desc, err := descriptor.Resolve(ctx, cfg, opts.APIVersion)
			if err != nil {
				return err
			}

			client, err := dynamic.NewDefaultNamespacedClient(cfg)
			if err != nil {
				return err
			}

			updated, err := client.Update(ctx, desc, obj, metav1.UpdateOptions{})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "dashboard %q updated", updated.GetName())
			return nil
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
	APIVersion string
	Yes        bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Yes, "yes", "y", false, "Skip confirmation prompt")
	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *deleteOpts) Validate() error {
	return nil
}

// newDeleteCommand returns the `dashboards delete <name>` subcommand.
// It prompts for confirmation unless --yes / -y is passed.
func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
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

			if !opts.Yes {
				if !confirmDelete(cmd.OutOrStdout(), cmd.InOrStdin(), name) {
					return nil
				}
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

			if err := client.Delete(ctx, desc, name, metav1.DeleteOptions{}); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "dashboard %q deleted", name)
			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// confirmDelete asks the user to confirm a destructive operation.
// Returns true if confirmed, false if the user aborted.
func confirmDelete(w io.Writer, r io.Reader, name string) bool {
	fmt.Fprintf(w, "Delete dashboard %q? [y/N] ", name)
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		// EOF or scanner error is treated as "no" — correct behavior for non-interactive pipelines.
		return false
	}
	answer := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
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
