package overrides

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the overrides command group with get and update subcommands.
// The loader carries the --config flag bound on the appo11y command; every
// subcommand loads config through it so an explicit --config is honored.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "overrides",
		Short: "Manage App Observability metrics generator overrides.",
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
	o.IO.RegisterCustomCodec("table", &overridesTableCodec{})
	o.IO.RegisterCustomCodec("wide", &overridesTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the App Observability metrics generator overrides.",
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

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), typedObj.Spec)
			}

			res, err := ToResource(typedObj.Spec, cfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert overrides to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// overridesTableCodec renders MetricsGeneratorConfig as a tabular table.
type overridesTableCodec struct {
	Wide bool
}

func (c *overridesTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *overridesTableCodec) Encode(w io.Writer, v any) error {
	cfg, ok := v.(MetricsGeneratorConfig)
	if !ok {
		return errors.New("invalid data type for table codec: expected MetricsGeneratorConfig")
	}

	collection := "enabled"
	if cfg.MetricsGenerator != nil && cfg.MetricsGenerator.DisableCollection {
		collection = "disabled"
	}

	interval := "-"
	if cfg.MetricsGenerator != nil && cfg.MetricsGenerator.CollectionInterval != "" {
		interval = cfg.MetricsGenerator.CollectionInterval
	}

	serviceGraphs := "disabled"
	spanMetrics := "disabled"
	var sgDimensions, smDimensions string

	if cfg.MetricsGenerator != nil && cfg.MetricsGenerator.Processor != nil {
		if cfg.MetricsGenerator.Processor.ServiceGraphs != nil {
			serviceGraphs = "enabled"
			sgDimensions = strings.Join(cfg.MetricsGenerator.Processor.ServiceGraphs.Dimensions, ", ")
		}
		if cfg.MetricsGenerator.Processor.SpanMetrics != nil {
			spanMetrics = "enabled"
			smDimensions = strings.Join(cfg.MetricsGenerator.Processor.SpanMetrics.Dimensions, ", ")
		}
	}

	if c.Wide {
		t := style.NewTable("NAME", "COLLECTION", "INTERVAL", "SERVICE GRAPHS", "SPAN METRICS", "SG DIMENSIONS", "SM DIMENSIONS")
		t.Row(cfg.GetResourceName(), collection, interval, serviceGraphs, spanMetrics, sgDimensions, smDimensions)
		return t.Render(w)
	}

	t := style.NewTable("NAME", "COLLECTION", "INTERVAL", "SERVICE GRAPHS", "SPAN METRICS")
	t.Row(cfg.GetResourceName(), collection, interval, serviceGraphs, spanMetrics)
	return t.Render(w)
}

func (c *overridesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

type updateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "file", "f", "", "Path to the overrides file (JSON or YAML)")
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
		return errors.New("--file is required")
	}
	return o.IO.Validate()
}

func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update App Observability metrics generator overrides from a file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			typedObj, err := parseOverridesFile(opts.File)
			if err != nil {
				return fmt.Errorf("failed to parse overrides file: %w", err)
			}

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			if _, err := crud.Update(ctx, "default", typedObj); err != nil {
				return fmt.Errorf("failed to update overrides: %w", err)
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
	result := cmdio.NewSingleMutation("updated", cmdio.MutationTarget{Kind: "Overrides", Name: "default"})
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
	cmdio.Success(w, "Overrides updated successfully.")
	return nil
}

func (c *updateReceiptCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

// parseOverridesFile reads a JSON or YAML file and returns a TypedObject[MetricsGeneratorConfig].
// The ETag annotation (if present) is restored onto the spec via SetETag.
func parseOverridesFile(filePath string) (*adapter.TypedObject[MetricsGeneratorConfig], error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var codec interface {
		Decode(src io.Reader, value any) error
	}

	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".json":
		codec = format.NewJSONCodec()
	default:
		codec = format.NewYAMLCodec()
	}

	var obj unstructured.Unstructured
	if err := codec.Decode(strings.NewReader(string(data)), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	specRaw, ok := obj.Object["spec"]
	if !ok {
		return nil, errors.New("file has no spec field")
	}

	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return nil, errors.New("spec is not a map")
	}

	specBytes, err := json.Marshal(specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	var cfg MetricsGeneratorConfig
	if err := json.Unmarshal(specBytes, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec: %w", err)
	}

	// Restore ETag from annotations so UpdateFn can use it for the If-Match header.
	if etag := obj.GetAnnotations()[ETagAnnotation]; etag != "" {
		cfg.SetETag(etag)
	}

	typedObj := &adapter.TypedObject[MetricsGeneratorConfig]{
		Spec: cfg,
	}
	typedObj.SetName(cfg.GetResourceName())

	return typedObj, nil
}
