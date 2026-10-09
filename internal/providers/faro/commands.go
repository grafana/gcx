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

	return newAppCRUD(client, cfg.Namespace), cfg, nil
}

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type listOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, AppTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	o.IO.BindListLimit(flags, &o.Limit, "apps", 50)
}

func newListCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Frontend Observability apps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			// The Faro API returns every app unpaginated, so the limit is a
			// display trim and the observed total is exact. Fetch everything and
			// truncate here so the truncation is reported on stderr.
			typedObjs, err := crud.List(ctx, 0)
			if err != nil {
				return err
			}
			typedObjs, meta := cmdio.TruncateCompleteList(typedObjs, opts.Limit)
			meta = cmdio.AttachListMeta(meta, os.Args)

			if err := opts.IO.Encode(cmd.OutOrStdout(), typedObjs); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// AppTable declares the Faro app table. Commands encode
// []adapter.TypedObject[FaroApp] — the same payload the JSON and YAML codecs
// receive — so the columns reach through .Spec rather than the command
// unwrapping and changing what those codecs see.
func AppTable() cmdio.Table[adapter.TypedObject[FaroApp]] {
	spec := func(fn func(FaroApp) string) func(adapter.TypedObject[FaroApp]) string {
		return func(obj adapter.TypedObject[FaroApp]) string { return fn(obj.Spec) }
	}
	return cmdio.Table[adapter.TypedObject[FaroApp]]{
		Columns: []cmdio.Column[adapter.TypedObject[FaroApp]]{
			{Header: "NAME", Content: spec(func(a FaroApp) string { return a.GetResourceName() })},
			{Header: "APP KEY", Content: spec(func(a FaroApp) string { return cmdio.OrDash(a.AppKey) })},
			{Header: "COLLECT ENDPOINT URL", Content: spec(func(a FaroApp) string { return cmdio.OrDash(a.CollectEndpointURL) })},
			{Header: "APP TYPE", Visible: cmdio.WideOnly, Content: spec(func(a FaroApp) string { return cmdio.OrDash(a.AppType) })},
			{Header: "RUNTIME", Visible: cmdio.WideOnly, Content: spec(func(a FaroApp) string {
				if a.Runtime == nil {
					return "-"
				}
				return cmdio.OrDash(*a.Runtime)
			})},
			{Header: "OTLP INGEST ENDPOINT URL", Visible: cmdio.WideOnly, Content: spec(func(a FaroApp) string { return cmdio.OrDash(a.OTLPIngestEndpointURL) })},
			{Header: "CORS ORIGINS", Visible: cmdio.WideOnly, Content: spec(func(a FaroApp) string { return corsOriginsString(a.CORSOrigins) })},
			{Header: "EXTRA LOG LABELS", Visible: cmdio.WideOnly, Content: spec(func(a FaroApp) string { return labelsString(a.ExtraLogLabels) })},
			{Header: "GEOLOCATION", Visible: cmdio.WideOnly, Content: spec(func(a FaroApp) string { return geolocationString(a.Settings) })},
		},
	}
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
	if settings == nil || settings.GeolocationEnabled == nil || !*settings.GeolocationEnabled {
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
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, AppTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <slug-id|name>",
		Short: "Get a Frontend Observability app by slug-id or name.",
		Long: `Get a Frontend Observability app by slug-id (my-web-app-42), numeric ID or
display name. A slug-id must match the app's own name, so a display name that
ends in digits, such as "checkout-2", never returns app 2. An argument that is
one app's name and another app's slug-id or ID is an error.`,
		Example: `  # Get by slug-id.
  gcx frontend apps get my-web-app-42

  # Get by name.
  gcx frontend apps get "My Web App"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj, err := resolveApp(ctx, crud, args[0])
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
	IO   cmdio.Options
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the Frontend Observability app manifest (use - for stdin)")
	o.IO.RegisterCustomCodec("text", &successLineCodec{render: singleMutationLine(func(m cmdio.SingleMutation) string {
		return fmt.Sprintf("Created Frontend Observability app %q (id=%s)", m.Target.Name, m.Target.ID)
	})})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *createOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newCreateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Frontend Observability app from a file.",
		Long: `Create a Frontend Observability app from a file.

Set spec.appType and spec.runtime at creation; the API ignores later changes
to appType. Web apps use appType web with runtime web-js. Mobile apps use
appType mobile with runtime flutter, react-native, android-native, or
swift-native. Create sends spec.extraLogLabels, including the legacy is_mobile
label.

Create and update send spec.settings. Set geolocationLevel to continent,
country, subdivision, city, or network. Set geolocationCountryDenylist to ISO
country codes, such as [DE], to skip enrichment for those sessions.`,
		Example: `  # Create an app from a YAML file.
  gcx frontend apps create -f app.yaml

  # Create a native Android app from stdin.
  cat <<EOF | gcx frontend apps create -f -
  apiVersion: faro.ext.grafana.app/v1alpha1
  kind: FaroApp
  metadata:
    name: my-mobile-app
  spec:
    name: my-mobile-app
    appType: mobile
    runtime: android-native
  EOF`,
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

			typedObj := &adapter.TypedObject[FaroApp]{Spec: *app}
			typedObj.SetName(app.GetResourceName())
			typedObj.SetNamespace(restCfg.Namespace)

			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return fmt.Errorf("creating faro app %q: %w", app.Name, err)
			}

			result := cmdio.NewSingleMutation("created", cmdio.MutationTarget{
				Kind: Kind,
				Name: created.Spec.Name,
				ID:   created.Spec.ID,
			})
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

type updateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the Frontend Observability app manifest (use - for stdin)")
	o.IO.RegisterCustomCodec("text", &successLineCodec{render: singleMutationLine(func(m cmdio.SingleMutation) string {
		return fmt.Sprintf("Updated Frontend Observability app %q (id=%s)", m.Target.Name, m.Target.ID)
	})})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newUpdateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <slug-id|name>",
		Short: "Update a Frontend Observability app from a file.",
		Long: `Update a Frontend Observability app from a file. The argument is a slug-id,
numeric ID or display name, resolved as in "gcx frontend apps get".

The API cannot rename an app, so spec.name must match the stored name. Omit
spec.runtime to keep the stored runtime; an empty runtime is invalid. The API
ignores changes to spec.appType. Omitted settings keep their stored values;
an empty geolocationCountryDenylist clears it.`,
		Example: `  # Update an app using its slug-id.
  gcx frontend apps update my-web-app-42 -f app.yaml`,
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

			target, err := resolveApp(ctx, crud, name)
			if err != nil {
				return fmt.Errorf("updating faro app %q: %w", name, err)
			}

			typedObj := &adapter.TypedObject[FaroApp]{Spec: *app}
			typedObj.SetName(target.GetName())
			typedObj.SetNamespace(restCfg.Namespace)

			updated, err := crud.Update(ctx, target.GetName(), typedObj)
			if err != nil {
				return fmt.Errorf("updating faro app %q: %w", name, err)
			}

			result := cmdio.NewSingleMutation("updated", cmdio.MutationTarget{
				Kind: Kind,
				Name: updated.Spec.Name,
				ID:   updated.Spec.ID,
			})
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete command
// ---------------------------------------------------------------------------

type deleteOpts struct {
	IO cmdio.Options
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &successLineCodec{render: singleMutationLine(func(m cmdio.SingleMutation) string {
		return fmt.Sprintf("Deleted Frontend Observability app %q (id=%s)", m.Target.Name, m.Target.ID)
	})})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *deleteOpts) Validate() error { return o.IO.Validate() }

func newDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <slug-id|name>",
		Short: "Delete a Frontend Observability app.",
		Long: `Delete a Frontend Observability app.

Deleting requires the grafana-kowalski-app.apps:delete permission (granted to
Admin and Frontend Observability Admin by default). A user with only apps:write
can create and update apps but cannot delete them.

The argument is a slug-id (my-web-app-42), numeric ID or display name. A
slug-id must match the app's own name, so deleting "checkout-2" never deletes
app 2 when app 2 has another name. An argument that is one app's name and
another app's slug-id or ID is an error. There is no confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			name := args[0]

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			target, err := resolveApp(ctx, crud, name)
			if err != nil {
				return fmt.Errorf("deleting faro app %q: %w", name, err)
			}
			if err := crud.Delete(ctx, target.GetName()); err != nil {
				return fmt.Errorf("deleting faro app %q: %w", name, err)
			}

			result := cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{
				Kind: Kind,
				Name: target.Spec.Name,
				ID:   target.Spec.ID,
			})
			return opts.IO.Encode(cmd.OutOrStdout(), result)
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
