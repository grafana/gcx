package settings

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the settings command group.
func Commands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage App Observability plugin settings.",
	}
	cmd.AddCommand(
		newGetCommand(),
		newUpdateCommand(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &settingsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &settingsTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand() *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get App Observability plugin settings.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, cfg, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, "default")
			if err != nil {
				return err
			}

			s := typedObj.Spec

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), &s)
			}

			res, err := ToResource(s, cfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert settings to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// settingsTableCodec renders PluginSettings as a tabular table.
type settingsTableCodec struct {
	Wide bool
}

func (c *settingsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *settingsTableCodec) Encode(w io.Writer, v any) error {
	s, ok := v.(*PluginSettings)
	if !ok {
		return errors.New("invalid data type for table codec: expected *PluginSettings")
	}

	logQueryMode := s.JSONData.DefaultLogQueryMode
	if logQueryMode == "" {
		logQueryMode = "-"
	}

	metricsMode := s.JSONData.MetricsMode
	if metricsMode == "" {
		metricsMode = "-"
	}

	if c.Wide {
		logsQueryNS := s.JSONData.LogsQueryWithNamespace
		logsQueryNoNS := s.JSONData.LogsQueryWithoutNamespace
		t := style.NewTable("NAME", "LOG QUERY MODE", "METRICS MODE", "LOGS QUERY (NS)", "LOGS QUERY (NO NS)")
		t.Row(s.GetResourceName(), logQueryMode, metricsMode, logsQueryNS, logsQueryNoNS)
		return t.Render(w)
	}

	t := style.NewTable("NAME", "LOG QUERY MODE", "METRICS MODE")
	t.Row(s.GetResourceName(), logQueryMode, metricsMode)
	return t.Render(w)
}

func (c *settingsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

type updateOpts struct {
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "file", "f", "", "Path to the settings file (JSON or YAML)")
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--file / -f is required")
	}
	return nil
}

func newUpdateCommand() *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update App Observability plugin settings.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			data, err := os.ReadFile(opts.File)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", opts.File, err)
			}

			var codec interface {
				Decode(src io.Reader, value any) error
			}
			switch strings.ToLower(filepath.Ext(opts.File)) {
			case ".json":
				codec = format.NewJSONCodec()
			default:
				codec = format.NewYAMLCodec()
			}

			var obj unstructured.Unstructured
			if err := codec.Decode(strings.NewReader(string(data)), &obj); err != nil {
				return fmt.Errorf("failed to parse %s: %w", opts.File, err)
			}

			res, err := resources.FromUnstructured(&obj)
			if err != nil {
				return fmt.Errorf("failed to build resource from %s: %w", opts.File, err)
			}

			s, err := FromResource(res)
			if err != nil {
				return fmt.Errorf("failed to extract settings from %s: %w", opts.File, err)
			}

			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[PluginSettings]{
				Spec: *s,
			}
			typedObj.SetName("default")

			if _, err := crud.Update(ctx, "default", typedObj); err != nil {
				return fmt.Errorf("failed to update settings: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated App Observability plugin settings")
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
