package preferences

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

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

			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, "default")
			if err != nil {
				return err
			}

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), &typedObj.Spec)
			}

			res, err := ToResource(typedObj.Spec, cfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert preferences to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// PreferencesTableCodec renders organization preferences as a KEY/VALUE table.
type PreferencesTableCodec struct{}

func (c *PreferencesTableCodec) Format() format.Format { return "table" }

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

func (c *PreferencesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

type updateOpts struct {
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "file", "f", "", "Path to a preferences manifest file (JSON or YAML), or '-' for stdin")
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
		Short: "Update organization preferences from a manifest file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			data, err := readInput(cmd.InOrStdin(), opts.File)
			if err != nil {
				return err
			}

			var codec interface {
				Decode(src io.Reader, value any) error
			}
			switch strings.ToLower(filepath.Ext(opts.File)) {
			case ".yaml", ".yml":
				codec = format.NewYAMLCodec()
			default:
				codec = format.NewJSONCodec()
			}

			var obj unstructured.Unstructured
			if err := codec.Decode(strings.NewReader(string(data)), &obj); err != nil {
				return fmt.Errorf("failed to parse %s: %w", opts.File, err)
			}

			res, err := resources.FromUnstructured(&obj)
			if err != nil {
				return fmt.Errorf("failed to build resource from %s: %w", opts.File, err)
			}

			p, err := FromResource(res)
			if err != nil {
				return fmt.Errorf("failed to extract preferences from %s: %w", opts.File, err)
			}

			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[OrgPreferences]{
				Spec: *p,
			}
			typedObj.SetName("default")

			if _, err := crud.Update(ctx, "default", typedObj); err != nil {
				return fmt.Errorf("failed to update preferences: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Organization preferences updated")
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

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
