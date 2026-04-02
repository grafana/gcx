package faro

import (
	"errors"
	"fmt"
	"os"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// show-sourcemaps command
// ---------------------------------------------------------------------------

type showSourcemapsOpts struct {
	IO cmdio.Options
}

func (o *showSourcemapsOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func newShowSourcemapsCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &showSourcemapsOpts{}
	cmd := &cobra.Command{
		Use:   "show-sourcemaps <app-name>",
		Short: "Show sourcemaps for a Faro app.",
		Args:  cobra.ExactArgs(1),
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

			raw, err := client.ListSourcemaps(ctx, appID)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), raw)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// apply-sourcemap command
// ---------------------------------------------------------------------------

type applySourcemapOpts struct {
	File string
}

func (o *applySourcemapOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "Path to the sourcemap file to upload")
}

func (o *applySourcemapOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func newApplySourcemapCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &applySourcemapOpts{}
	cmd := &cobra.Command{
		Use:   "apply-sourcemap <app-name>",
		Short: "Upload a sourcemap for a Faro app.",
		Example: `  # Upload a sourcemap bundle.
  gcx faro apps apply-sourcemap my-web-app-42 -f bundle.js.map`,
		Args: cobra.ExactArgs(1),
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

			f, err := os.Open(opts.File)
			if err != nil {
				return fmt.Errorf("failed to open sourcemap file %s: %w", opts.File, err)
			}
			defer f.Close()

			_, err = client.UploadSourcemap(ctx, appID, f)
			if err != nil {
				return fmt.Errorf("uploading sourcemap for app %s: %w", appID, err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Uploaded sourcemap for app %s from %s", appID, opts.File)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// remove-sourcemap command
// ---------------------------------------------------------------------------

type removeSourcemapOpts struct{}

func (o *removeSourcemapOpts) setup(_ *pflag.FlagSet) {}

func newRemoveSourcemapCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &removeSourcemapOpts{}
	cmd := &cobra.Command{
		Use:   "remove-sourcemap <app-name> <bundle-id>",
		Short: "Remove a sourcemap bundle from a Faro app.",
		Args:  cobra.ExactArgs(2),
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
			bundleID := args[1]

			if err := client.DeleteSourcemap(ctx, appID, bundleID); err != nil {
				return fmt.Errorf("removing sourcemap %s for app %s: %w", bundleID, appID, err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Removed sourcemap %s from app %s", bundleID, appID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// resolveAppID extracts the numeric ID from a slug-id composite name,
// falling back to using the argument as-is.
func resolveAppID(name string) string {
	if id, ok := adapter.ExtractIDFromSlug(name); ok {
		return id
	}
	return name
}
