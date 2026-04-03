package faro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NewTypedCRUD creates a TypedCRUD[FaroApp] for use in provider commands.
// It loads the REST config from the loader and constructs the Faro client.
func NewTypedCRUD(ctx context.Context, loader RESTConfigLoader) (*adapter.TypedCRUD[FaroApp], config.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, config.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for faro: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, config.NamespacedRESTConfig{}, fmt.Errorf("failed to create faro client: %w", err)
	}

	crud := &adapter.TypedCRUD[FaroApp]{
		ListFn: func(ctx context.Context) ([]FaroApp, error) {
			return client.List(ctx)
		},

		GetFn: func(ctx context.Context, name string) (*FaroApp, error) {
			id, ok := adapter.ExtractIDFromSlug(name)
			if !ok {
				id = name
			}
			return client.Get(ctx, id)
		},

		CreateFn: func(ctx context.Context, app *FaroApp) (*FaroApp, error) {
			return client.Create(ctx, app)
		},

		UpdateFn: func(ctx context.Context, name string, app *FaroApp) (*FaroApp, error) {
			id, ok := adapter.ExtractIDFromSlug(name)
			if !ok {
				id = name
			}
			return client.Update(ctx, id, app)
		},

		DeleteFn: func(ctx context.Context, name string) error {
			id, ok := adapter.ExtractIDFromSlug(name)
			if !ok {
				id = name
			}
			return client.Delete(ctx, id)
		},

		StripFields: []string{"id"},
		Namespace:   cfg.Namespace,
		Descriptor:  staticDescriptor,
	}

	return crud, cfg, nil
}

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &AppTableCodec{})
	o.IO.RegisterCustomCodec("wide", &AppTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Faro apps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), typedObjs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// AppTableCodec renders Faro apps as a tabular table.
type AppTableCodec struct {
	Wide bool
}

// Format returns the output format name.
func (c *AppTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes apps to the writer as a table.
// It accepts []adapter.TypedObject[FaroApp] (from commands) and extracts .Spec internally.
func (c *AppTableCodec) Encode(w io.Writer, v any) error {
	typedObjs, ok := v.([]adapter.TypedObject[FaroApp])
	if !ok {
		return errors.New("invalid data type for table codec: expected []TypedObject[FaroApp]")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "NAME\tAPP KEY\tCOLLECT ENDPOINT URL\tCORS ORIGINS\tEXTRA LOG LABELS\tGEOLOCATION")
	} else {
		fmt.Fprintln(tw, "NAME\tAPP KEY\tCOLLECT ENDPOINT URL")
	}

	for _, obj := range typedObjs {
		app := obj.Spec
		appKey := app.AppKey
		if appKey == "" {
			appKey = "-"
		}
		endpoint := app.CollectEndpointURL
		if endpoint == "" {
			endpoint = "-"
		}

		if c.Wide {
			cors := corsOriginsString(app.CORSOrigins)
			labels := labelsString(app.ExtraLogLabels)
			geo := geolocationString(app.Settings)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", app.GetResourceName(), appKey, endpoint, cors, labels, geo)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", app.GetResourceName(), appKey, endpoint)
		}
	}

	return tw.Flush()
}

// Decode is not supported for table format.
func (c *AppTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// resolveGetTarget resolves the lookup ID for the get command.
// If --name is provided, it does a client-side name lookup and returns the numeric ID.
// Otherwise it returns the positional argument as-is.
func resolveGetTarget(ctx context.Context, cfg config.NamespacedRESTConfig, name string, args []string) (string, error) {
	if name != "" {
		client, err := NewClient(cfg)
		if err != nil {
			return "", err
		}
		app, err := client.GetByName(ctx, name)
		if err != nil {
			return "", fmt.Errorf("faro app with name %q not found: %w", name, err)
		}
		return app.ID, nil
	}
	return args[0], nil
}

func corsOriginsString(origins []CORSOrigin) string {
	if len(origins) == 0 {
		return "-"
	}
	urls := make([]string, len(origins))
	for i, o := range origins {
		urls[i] = o.URL
	}
	return strings.Join(urls, ", ")
}

func labelsString(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(labels))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ", ")
}

func geolocationString(settings *FaroAppSettings) string {
	if settings == nil || !settings.GeolocationEnabled {
		return "-"
	}
	level := settings.GeolocationLevel
	if level == "" {
		level = "enabled"
	}
	return level
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type getOpts struct {
	IO   cmdio.Options
	Name string
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &AppTableCodec{})
	o.IO.RegisterCustomCodec("wide", &AppTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "Get Faro app by name instead of slug-id")
}

func newGetCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get [slug-id]",
		Short: "Get a Faro app by slug-id or name.",
		Example: `  # Get by slug-id.
  gcx faro apps get my-web-app-42

  # Get by name.
  gcx faro apps get --name "My Web App"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			if opts.Name == "" && len(args) == 0 {
				return errors.New("provide a slug-id argument or --name flag")
			}

			ctx := cmd.Context()

			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			lookupID, lookupErr := resolveGetTarget(ctx, cfg, opts.Name, args)
			if lookupErr != nil {
				return lookupErr
			}

			typedObj, err := crud.Get(ctx, lookupID)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), []adapter.TypedObject[FaroApp]{*typedObj})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// create command
// ---------------------------------------------------------------------------

type createOpts struct {
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the Faro app manifest (use - for stdin)")
}

func (o *createOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func newCreateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Faro app from a file.",
		Example: `  # Create a Faro app from a YAML file.
  gcx faro apps create -f app.yaml

  # Create from stdin.
  cat app.yaml | gcx faro apps create -f -`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, restCfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			app, err := readAppFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			if len(app.ExtraLogLabels) > 0 || app.Settings != nil {
				cmdio.Warning(cmd.ErrOrStderr(),
					"extraLogLabels and settings are ignored during creation (Faro API limitation); use update to apply them")
			}

			typedObj := &adapter.TypedObject[FaroApp]{Spec: *app}
			typedObj.SetName(app.GetResourceName())
			typedObj.SetNamespace(restCfg.Namespace)

			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return fmt.Errorf("creating faro app %q: %w", app.Name, err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Created Faro app %q (id=%s)", created.Spec.Name, created.Spec.ID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

type updateOpts struct {
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the Faro app manifest (use - for stdin)")
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func newUpdateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a Faro app from a file.",
		Example: `  # Update a Faro app using its slug-id.
  gcx faro apps update my-web-app-42 -f app.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			name := args[0]

			crud, restCfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			app, err := readAppFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[FaroApp]{Spec: *app}
			typedObj.SetName(name)
			typedObj.SetNamespace(restCfg.Namespace)

			updated, err := crud.Update(ctx, name, typedObj)
			if err != nil {
				return fmt.Errorf("updating faro app %q: %w", name, err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated Faro app %q (id=%s)", updated.Spec.Name, updated.Spec.ID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete command
// ---------------------------------------------------------------------------

type deleteOpts struct{}

func (o *deleteOpts) setup(_ *pflag.FlagSet) {}

func newDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a Faro app.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, name); err != nil {
				return fmt.Errorf("deleting faro app %q: %w", name, err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Deleted Faro app %q", name)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// readAppFromFile reads a FaroApp spec from a file path or stdin.
// It decodes a Kubernetes-style manifest and JSON-round-trips the spec
// into a FaroApp, so new fields are handled automatically.
func readAppFromFile(file string, stdin io.Reader) (*FaroApp, error) {
	var reader io.Reader
	if file == "-" {
		reader = stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", file, err)
		}
		defer f.Close()
		reader = f
	}

	yamlCodec := format.NewYAMLCodec()
	var obj unstructured.Unstructured
	if err := yamlCodec.Decode(reader, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	specRaw, ok := obj.Object["spec"]
	if !ok {
		return nil, errors.New("manifest is missing spec field")
	}

	// JSON round-trip: map[string]any → JSON → FaroApp.
	specJSON, err := json.Marshal(specRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to encode spec: %w", err)
	}
	var app FaroApp
	if err := json.Unmarshal(specJSON, &app); err != nil {
		return nil, fmt.Errorf("failed to parse spec: %w", err)
	}

	// Extract ID from metadata.name slug.
	if metaName := obj.GetName(); metaName != "" {
		if id, ok := adapter.ExtractIDFromSlug(metaName); ok {
			app.ID = id
		}
	}

	if app.Name == "" {
		return nil, errors.New("manifest spec.name is required")
	}

	return &app, nil
}
