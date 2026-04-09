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
// show-sourcemaps command
// ---------------------------------------------------------------------------

type showSourcemapsOpts struct {
	Limit int
	IO    cmdio.Options
}

func (o *showSourcemapsOpts) setup(flags *pflag.FlagSet) {
	flags.IntVar(&o.Limit, "limit", 0, "Maximum number of sourcemaps to return (0 for all)")
	o.IO.RegisterCustomCodec("text", &SourcemapTableCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newShowSourcemapsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &showSourcemapsOpts{}
	cmd := &cobra.Command{
		Use:   "show-sourcemaps <app-name>",
		Short: "Show sourcemaps for a Frontend Observability app.",
		Example: `  # List all sourcemaps for an app.
  gcx frontend apps show-sourcemaps my-web-app-42

  # List the first 10 sourcemaps.
  gcx frontend apps show-sourcemaps my-web-app-42 --limit 10

  # Output as JSON.
  gcx frontend apps show-sourcemaps my-web-app-42 -o json`,
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

type applySourcemapOpts struct {
	File     string
	BundleID string
}

func (o *applySourcemapOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "Path to the sourcemap file to upload")
	flags.StringVar(&o.BundleID, "bundle-id", "", "Bundle ID (auto-generated if not set)")
}

func (o *applySourcemapOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func newApplySourcemapCommand(loader *providers.ConfigLoader) *cobra.Command {
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

			// Resolve Faro API URL.
			faroAPIURL, err := resolveFaroAPIURL(ctx, loader)
			if err != nil {
				return err
			}

			// Load cloud config for stack ID and token.
			cloudCfg, err := loader.LoadCloudConfig(ctx)
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

			cmdio.Success(cmd.OutOrStdout(), "Uploaded sourcemap for app %s (bundle %s)", appID, bundleID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// remove-sourcemap command
// ---------------------------------------------------------------------------

func newRemoveSourcemapCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-sourcemap <app-name> <bundle-id> [bundle-id...]",
		Short: "Remove sourcemap bundles from a Frontend Observability app.",
		Example: `  # Remove a single sourcemap bundle.
  gcx frontend apps remove-sourcemap my-web-app-42 1234567890-abc12

  # Remove multiple bundles at once.
  gcx frontend apps remove-sourcemap my-web-app-42 bundle-1 bundle-2 bundle-3`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			cmdio.Success(cmd.OutOrStdout(), "Removed %d sourcemap(s) from app %s", len(bundleIDs), appID)
			return nil
		},
	}
	return cmd
}

// resolveFaroAPIURL resolves the Faro API URL from provider config cache,
// falling back to auto-discovery from plugin settings.
func resolveFaroAPIURL(ctx context.Context, loader *providers.ConfigLoader) (string, error) {
	// Check provider config cache first.
	provCfg, _, err := loader.LoadProviderConfig(ctx, "faro")
	if err == nil && provCfg != nil && provCfg["faro-api-url"] != "" {
		return provCfg["faro-api-url"], nil
	}

	// Fall back to discovery from plugin settings.
	grafanaCfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("faro: grafana config required for API URL discovery: %w", err)
	}

	apiURL, err := DiscoverFaroAPIURL(ctx, grafanaCfg)
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
