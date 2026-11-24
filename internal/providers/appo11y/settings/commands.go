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
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the settings command group.
// The loader carries the --config flag bound on the appo11y command; every
// subcommand loads config through it so an explicit --config is honored.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage App Observability plugin settings.",
	}
	cmd.AddCommand(
		newGetCommand(loader),
		newUpdateCommand(loader),
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
	table := settingsTable()
	o.IO.RegisterCustomCodec(cmdio.FormatTable, table.Codec(cmdio.FormatTable))
	o.IO.RegisterCustomCodec(cmdio.FormatWide, table.Codec(cmdio.FormatWide))
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
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

			crud, cfg, err := NewTypedCRUD(ctx, loader)
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

func settingsTable() cmdio.TableAdapter[*PluginSettings, PluginSettings] {
	return cmdio.TableAdapter[*PluginSettings, PluginSettings]{
		Table: cmdio.Table[PluginSettings]{Columns: []cmdio.Column[PluginSettings]{
			{Header: "NAME", Content: func(s PluginSettings) string { return s.GetResourceName() }},
			{Header: "LOG QUERY MODE", Content: func(s PluginSettings) string {
				if s.JSONData.DefaultLogQueryMode == "" {
					return "-"
				}
				return s.JSONData.DefaultLogQueryMode
			}},
			{Header: "METRICS MODE", Content: func(s PluginSettings) string {
				if s.JSONData.MetricsMode == "" {
					return "-"
				}
				return s.JSONData.MetricsMode
			}},
			{Header: "LOGS QUERY (NS)", Visible: cmdio.WideOnly, Content: func(s PluginSettings) string { return s.JSONData.LogsQueryWithNamespace }},
			{Header: "LOGS QUERY (NO NS)", Visible: cmdio.WideOnly, Content: func(s PluginSettings) string { return s.JSONData.LogsQueryWithoutNamespace }},
		}},
		Rows: func(s *PluginSettings) ([]PluginSettings, error) { return []PluginSettings{*s}, nil },
	}
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

type updateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "file", "f", "", "Path to the settings file (JSON or YAML)")
	// The update receipt is a SingleMutation document through the codec
	// system: the human text default renders the exact success line this
	// command has always printed; agent mode and explicit -o json/yaml get
	// the structured document.
	o.IO.RegisterCustomCodec("text", &updateReceiptCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--file / -f is required")
	}
	return o.IO.Validate()
}

func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update App Observability plugin settings.",
		Args:  cobra.NoArgs,
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

			crud, _, err := NewTypedCRUD(ctx, loader)
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

			return writeUpdateReceipt(cmd.OutOrStdout(), opts)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// writeUpdateReceipt writes the update result document through the codec
// system. Split from RunE so the output contract is testable without a live
// plugin API.
func writeUpdateReceipt(stdout io.Writer, opts *updateOpts) error {
	result := cmdio.NewSingleMutation("updated", cmdio.MutationTarget{Kind: Kind, Name: "default"})
	return opts.IO.Encode(stdout, result)
}

// updateReceiptCodec is the human "text" codec for the update receipt: it
// renders exactly the one-line confirmation this command has always printed,
// keeping default human stdout byte-identical to the pre-codec output.
type updateReceiptCodec struct{}

func (c *updateReceiptCodec) Format() format.Format { return "text" }

func (c *updateReceiptCodec) Encode(w io.Writer, v any) error {
	if _, ok := v.(cmdio.SingleMutation); !ok {
		return errors.New("invalid data type for update receipt codec: expected SingleMutation")
	}
	cmdio.Success(w, "Updated App Observability plugin settings")
	return nil
}

func (c *updateReceiptCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}
