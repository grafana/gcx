package overrides

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the overrides command group with get and update subcommands.
func Commands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "overrides",
		Short: "Manage App Observability metrics generator overrides.",
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
	o.IO.RegisterCustomCodec("table", &overridesTableCodec{})
	o.IO.RegisterCustomCodec("wide", &overridesTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand() *cobra.Command {
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

			crud, cfg, err := NewTypedCRUD(ctx)
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

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if c.Wide {
		fmt.Fprintln(tw, "NAME\tCOLLECTION\tINTERVAL\tSERVICE GRAPHS\tSPAN METRICS\tSG DIMENSIONS\tSM DIMENSIONS")
	} else {
		fmt.Fprintln(tw, "NAME\tCOLLECTION\tINTERVAL\tSERVICE GRAPHS\tSPAN METRICS")
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
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			cfg.GetResourceName(), collection, interval, serviceGraphs, spanMetrics, sgDimensions, smDimensions)
	} else {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			cfg.GetResourceName(), collection, interval, serviceGraphs, spanMetrics)
	}

	return tw.Flush()
}

func (c *overridesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

type updateOpts struct {
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "file", "f", "", "Path to the overrides file (JSON or YAML)")
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--file is required")
	}
	return nil
}

func newUpdateCommand() *cobra.Command {
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

			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			if _, err := crud.Update(ctx, "default", typedObj); err != nil {
				return fmt.Errorf("failed to update overrides: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Overrides updated successfully.")
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
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
