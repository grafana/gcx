package preferences

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PreferencesTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the current organization preferences.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			prefs, err := client.Get(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), prefs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// PreferencesTableCodec renders organization preferences as a KEY/VALUE table.
type PreferencesTableCodec struct{}

// Format reports the codec's output format identifier.
func (c *PreferencesTableCodec) Format() format.Format { return "table" }

// Encode writes the preferences as a simple KEY/VALUE table.
func (c *PreferencesTableCodec) Encode(w io.Writer, v any) error {
	prefs, ok := v.(*OrgPreferences)
	if !ok {
		return errors.New("invalid data type for table codec: expected *OrgPreferences")
	}

	t := style.NewTable("KEY", "VALUE")
	t.Row("theme", prefs.Theme)
	t.Row("timezone", prefs.Timezone)
	t.Row("weekStart", prefs.WeekStart)
	t.Row("locale", prefs.Locale)
	t.Row("homeDashboardId", strconv.Itoa(prefs.HomeDashboardID))
	return t.Render(w)
}

// Decode is not supported for the preferences table codec.
func (c *PreferencesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

type updateOpts struct {
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "file", "f", "", "Path to a JSON preferences file, or '-' for stdin")
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--file / -f is required")
	}
	return nil
}

func newUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update organization preferences from a JSON file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			data, err := readInput(cmd.InOrStdin(), opts.File)
			if err != nil {
				return err
			}

			var prefs OrgPreferences
			if err := json.Unmarshal(data, &prefs); err != nil {
				return fmt.Errorf("failed to parse preferences JSON: %w", err)
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			if err := client.Update(ctx, &prefs); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Organization preferences updated")
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// readInput reads the update payload from the given path, or from stdin when
// path is "-".
func readInput(stdin io.Reader, path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return data, nil
}
