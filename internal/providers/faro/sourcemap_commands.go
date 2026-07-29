package faro

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SourcemapTableCodec renders sourcemap bundles as a table.
type SourcemapTableCodec struct{}

func (c *SourcemapTableCodec) Format() format.Format { return "text" }

func (c *SourcemapTableCodec) Encode(w io.Writer, v any) error {
	bundles, ok := v.([]SourcemapBundle)
	if !ok {
		return fmt.Errorf("invalid data type for sourcemap table codec: expected []SourcemapBundle, got %T", v)
	}

	if len(bundles) == 0 {
		_, err := fmt.Fprintln(w, "No sourcemap bundles found.")
		return err
	}

	t := style.NewTable("BUNDLE ID", "CREATED", "UPDATED")
	for _, b := range bundles {
		t.Row(b.ID, b.Created, b.Updated)
	}
	return t.Render(w)
}

func (c *SourcemapTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// ---------------------------------------------------------------------------
// list-sourcemaps command
// ---------------------------------------------------------------------------

type listSourcemapsOpts struct {
	Limit int
	IO    cmdio.Options
}

func (o *listSourcemapsOpts) setup(flags *pflag.FlagSet) {
	flags.IntVar(&o.Limit, "limit", 0, "Maximum number of sourcemaps to return (0 for all)")
	o.IO.RegisterCustomCodec("text", &SourcemapTableCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newListSourcemapsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listSourcemapsOpts{}
	cmd := &cobra.Command{
		Use:   "list-sourcemaps <app-name>",
		Short: "List sourcemaps for a Frontend Observability app.",
		Example: `  # List all sourcemaps for an app.
  gcx frontend apps list-sourcemaps my-web-app-42

  # List the first 10 sourcemaps.
  gcx frontend apps list-sourcemaps my-web-app-42 --limit 10

  # Output as JSON.
  gcx frontend apps list-sourcemaps my-web-app-42 -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(cfg)
			if err != nil {
				return err
			}

			appID := resolveAppID(args[0])

			bundles, err := client.ListSourcemaps(ctx, appID, opts.Limit)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), bundles)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// apply-sourcemap command
// ---------------------------------------------------------------------------

// Discriminators for the bespoke sourcemap result shapes. The mutation family
// in internal/output has no slot for the app/bundle pair these commands act
// on, so they carry their own collision-resistant type/schema_version fields.
const (
	sourcemapUploadResultType    = "gcx.faro.sourcemap_upload"
	sourcemapDeleteResultType    = "gcx.faro.sourcemap_delete"
	sourcemapResultSchemaVersion = "1"
)

// sourcemapUploadResult is the finite result of apply-sourcemap. It makes the
// (possibly auto-generated) bundle ID machine-readable — previously it was
// only recoverable from the prose success line.
type sourcemapUploadResult struct {
	Type          string `json:"type" yaml:"type"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Action        string `json:"action" yaml:"action"`
	AppID         string `json:"app_id" yaml:"app_id"`
	BundleID      string `json:"bundle_id" yaml:"bundle_id"`
}

// sourcemapUploadConfigLoader is the subset of *providers.ConfigLoader the
// apply-sourcemap command needs: the trust-checked provider snapshot (config +
// cloud credentials) and discovery-result caching.
type sourcemapUploadConfigLoader interface {
	RESTConfigLoader
	LoadDirectProviderSnapshot(ctx context.Context, policy providers.DirectProviderPolicy) (providers.DirectProviderSnapshot, error)
	SaveProviderConfig(ctx context.Context, providerName, key, value string) error
}

type applySourcemapOpts struct {
	IO       cmdio.Options
	File     string
	BundleID string
}

func (o *applySourcemapOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "Path to the sourcemap file to upload")
	flags.StringVar(&o.BundleID, "bundle-id", "", "Bundle ID (auto-generated if not set)")
	o.IO.RegisterCustomCodec("text", &successLineCodec{render: func(v any) (string, error) {
		r, ok := v.(sourcemapUploadResult)
		if !ok {
			return "", fmt.Errorf("invalid data type for text codec: expected sourcemapUploadResult, got %T", v)
		}
		return fmt.Sprintf("Uploaded sourcemap for app %s (bundle %s)", r.AppID, r.BundleID), nil
	}})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *applySourcemapOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newApplySourcemapCommand(loader sourcemapUploadConfigLoader) *cobra.Command {
	opts := &applySourcemapOpts{}
	cmd := &cobra.Command{
		Use:   "apply-sourcemap <app-name>",
		Short: "Upload a sourcemap for a Frontend Observability app.",
		Example: `  # Upload a sourcemap bundle.
  gcx frontend apps apply-sourcemap my-web-app-42 -f bundle.js.map`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			snapshot, err := loader.LoadDirectProviderSnapshot(ctx, providers.DirectProviderPolicy{
				ProviderName:    "faro",
				EndpointKeys:    []string{"faro-api-url"},
				CredentialEnv:   "GRAFANA_CLOUD_TOKEN",
				RejectAutoLocal: true,
			})
			if err != nil {
				return err
			}

			// Resolve Faro API URL.
			faroAPIURL, err := resolveFaroAPIURL(ctx, loader, snapshot)
			if err != nil {
				return err
			}

			// Load cloud config for stack ID and token.
			cloudCfg, err := snapshot.ResolveCloudConfig(ctx)
			if err != nil {
				return fmt.Errorf("cloud config required for sourcemap upload: %w", err)
			}

			// Generate bundle ID if not provided.
			bundleID := opts.BundleID
			if bundleID == "" {
				bundleID = GenerateBundleID()
			}

			// Open and read the sourcemap file.
			f, err := os.Open(opts.File)
			if err != nil {
				return fmt.Errorf("opening sourcemap file: %w", err)
			}
			defer f.Close()

			// Detect content type based on file extension.
			contentType := "application/json"
			if strings.HasSuffix(opts.File, ".tar.gz") || strings.HasSuffix(opts.File, ".tgz") {
				contentType = "application/gzip"
			}

			// Upload the sourcemap.
			appID := resolveAppID(args[0])
			if err := UploadSourcemap(ctx, faroAPIURL, cloudCfg.Stack.ID, cloudCfg.Token, appID, bundleID, f, contentType); err != nil {
				return err
			}

			result := sourcemapUploadResult{
				Type:          sourcemapUploadResultType,
				SchemaVersion: sourcemapResultSchemaVersion,
				Action:        "uploaded",
				AppID:         appID,
				BundleID:      bundleID,
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete-sourcemap command
// ---------------------------------------------------------------------------

// sourcemapDeleteResult is the finite result of delete-sourcemap. The batch
// delete is a single all-or-nothing HTTP call, so a per-bundle outcome list
// would be dishonest — the result carries the whole batch and one count.
type sourcemapDeleteResult struct {
	Type          string   `json:"type" yaml:"type"`
	SchemaVersion string   `json:"schema_version" yaml:"schema_version"`
	Action        string   `json:"action" yaml:"action"`
	AppID         string   `json:"app_id" yaml:"app_id"`
	BundleIDs     []string `json:"bundle_ids" yaml:"bundle_ids"`
	Deleted       int      `json:"deleted" yaml:"deleted"`
}

type deleteSourcemapOpts struct {
	IO cmdio.Options
}

func (o *deleteSourcemapOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &successLineCodec{render: func(v any) (string, error) {
		r, ok := v.(sourcemapDeleteResult)
		if !ok {
			return "", fmt.Errorf("invalid data type for text codec: expected sourcemapDeleteResult, got %T", v)
		}
		return fmt.Sprintf("Deleted %d sourcemap(s) from app %s", r.Deleted, r.AppID), nil
	}})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *deleteSourcemapOpts) Validate() error { return o.IO.Validate() }

func newDeleteSourcemapCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &deleteSourcemapOpts{}
	cmd := &cobra.Command{
		Use:   "delete-sourcemap <app-name> <bundle-id> [bundle-id...]",
		Short: "Delete sourcemap bundles from a Frontend Observability app.",
		Example: `  # Delete a single sourcemap bundle.
  gcx frontend apps delete-sourcemap my-web-app-42 1234567890-abc12

  # Delete multiple bundles at once.
  gcx frontend apps delete-sourcemap my-web-app-42 bundle-1 bundle-2 bundle-3`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(cfg)
			if err != nil {
				return err
			}

			appID := resolveAppID(args[0])
			bundleIDs := args[1:]

			if err := client.DeleteSourcemaps(ctx, appID, bundleIDs); err != nil {
				return err
			}

			result := sourcemapDeleteResult{
				Type:          sourcemapDeleteResultType,
				SchemaVersion: sourcemapResultSchemaVersion,
				Action:        "deleted",
				AppID:         appID,
				BundleIDs:     bundleIDs,
				Deleted:       len(bundleIDs),
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// resolveFaroAPIURL resolves the Faro API URL from provider config cache,
// falling back to auto-discovery from plugin settings.
func resolveFaroAPIURL(ctx context.Context, loader sourcemapUploadConfigLoader, snapshot providers.DirectProviderSnapshot) (string, error) {
	// Check the trust-checked provider config cache first.
	if snapshot.ProviderConfig != nil && snapshot.ProviderConfig["faro-api-url"] != "" {
		return snapshot.ProviderConfig["faro-api-url"], nil
	}

	// Fall back to discovery from plugin settings.
	if snapshot.GrafanaConfig == nil {
		return "", errors.New("faro: grafana config required for API URL discovery")
	}

	apiURL, err := DiscoverFaroAPIURL(ctx, *snapshot.GrafanaConfig)
	if err != nil {
		return "", fmt.Errorf("faro API URL not configured and discovery failed: %w\n\nSet providers.faro.faro-api-url in config or GRAFANA_PROVIDER_FARO_FARO_API_URL env var", err)
	}

	// Cache for subsequent calls.
	_ = loader.SaveProviderConfig(ctx, "faro", "faro-api-url", apiURL)

	return apiURL, nil
}

// resolveAppID extracts the numeric ID from a slug-id composite name,
// falling back to using the argument as-is.
func resolveAppID(name string) string {
	if id, ok := adapter.ExtractIDFromSlug(name); ok {
		return id
	}
	return name
}
