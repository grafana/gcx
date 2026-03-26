package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/grafanactl/internal/format"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// PipelineAPIVersion is the API version for fleet pipeline resources.
	PipelineAPIVersion = "fleet.ext.grafana.app/v1alpha1"
	// PipelineKind is the kind for pipeline resources.
	PipelineKind = "Pipeline"

	// CollectorAPIVersion is the API version for fleet collector resources.
	CollectorAPIVersion = "fleet.ext.grafana.app/v1alpha1"
	// CollectorKind is the kind for collector resources.
	CollectorKind = "Collector"
)

// ---------------------------------------------------------------------------
// Static descriptors
// ---------------------------------------------------------------------------

//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var pipelineDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "fleet.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     PipelineKind,
	Singular: "pipeline",
	Plural:   "pipelines",
}

//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var collectorDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "fleet.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     CollectorKind,
	Singular: "collector",
	Plural:   "collectors",
}

// PipelineDescriptor returns the static descriptor for pipeline resources.
func PipelineDescriptor() resources.Descriptor { return pipelineDescriptorVar }

// CollectorDescriptor returns the static descriptor for collector resources.
func CollectorDescriptor() resources.Descriptor { return collectorDescriptorVar }

// ---------------------------------------------------------------------------
// init — self-registration
// ---------------------------------------------------------------------------

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&FleetProvider{})
}

// ---------------------------------------------------------------------------
// FleetProvider — implements providers.Provider
// ---------------------------------------------------------------------------

var _ providers.Provider = &FleetProvider{}

// FleetProvider manages Grafana Fleet Management resources.
type FleetProvider struct{}

// Name returns the unique identifier for this provider.
func (p *FleetProvider) Name() string { return "fleet" }

// ShortDesc returns a one-line description of the provider.
func (p *FleetProvider) ShortDesc() string {
	return "Manage Grafana Fleet Management resources."
}

// Commands returns the Cobra commands contributed by this provider.
func (p *FleetProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	fleetCmd := &cobra.Command{
		Use:   "fleet",
		Short: p.ShortDesc(),
	}

	loader.BindFlags(fleetCmd.PersistentFlags())

	helper := &fleetHelper{loader: loader}

	fleetCmd.AddCommand(
		helper.pipelinesCommand(),
		helper.collectorsCommand(),
		helper.tenantCommand(),
	)

	return []*cobra.Command{fleetCmd}
}

// Validate checks that the given provider configuration is valid.
func (p *FleetProvider) Validate(_ map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *FleetProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for Fleet resource types.
func (p *FleetProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewPipelineAdapterFactory(loader),
			Descriptor: PipelineDescriptor(),
			GVK:        PipelineDescriptor().GroupVersionKind(),
			Schema:     pipelineSchema(),
			Example:    pipelineExample(),
		},
		{
			Factory:    NewCollectorAdapterFactory(loader),
			Descriptor: CollectorDescriptor(),
			GVK:        CollectorDescriptor().GroupVersionKind(),
			Schema:     collectorSchema(),
			Example:    collectorExample(),
		},
	}
}

// ---------------------------------------------------------------------------
// fleetHelper — shared helper for building commands
// ---------------------------------------------------------------------------

type fleetHelper struct {
	loader *providers.ConfigLoader
}

func (h *fleetHelper) loadClient(ctx context.Context) (*Client, string, error) {
	cloudCfg, err := h.loader.LoadCloudConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	url := cloudCfg.Stack.AgentManagementInstanceURL
	if url == "" {
		return nil, "", errors.New("fleet management endpoint is not available for this stack")
	}
	if cloudCfg.Stack.AgentManagementInstanceID == 0 {
		return nil, "", errors.New("fleet management instance ID is not available for this stack")
	}
	instanceID := strconv.Itoa(cloudCfg.Stack.AgentManagementInstanceID)
	httpClient, err := cloudCfg.HTTPClient()
	if err != nil {
		return nil, "", fmt.Errorf("fleet: failed to create HTTP client: %w", err)
	}
	return NewClient(url, instanceID, cloudCfg.Token, true, httpClient), cloudCfg.Namespace, nil
}

// ---------------------------------------------------------------------------
// Pipeline commands
// ---------------------------------------------------------------------------

func (h *fleetHelper) pipelinesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pipelines",
		Short:   "Manage Fleet Management pipelines.",
		Aliases: []string{"pipeline"},
	}

	cmd.AddCommand(
		h.newPipelineListCommand(),
		h.newPipelineGetCommand(),
		h.newPipelineCreateCommand(),
		h.newPipelineUpdateCommand(),
		h.newPipelineDeleteCommand(),
	)

	return cmd
}

func (h *fleetHelper) newPipelineListCommand() *cobra.Command {
	opts := &pipelineListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pipelines.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, namespace, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			pipelines, err := client.ListPipelines(ctx)
			if err != nil {
				return err
			}

			// Table codec operates on raw []Pipeline for direct field access.
			// Other formats (yaml/json) convert to K8s envelope Resources
			// for consistency with get/pull and round-trip support.
			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), pipelines)
			}

			var objs []unstructured.Unstructured
			for _, p := range pipelines {
				res, err := PipelineToResource(p, namespace)
				if err != nil {
					return fmt.Errorf("failed to convert pipeline %s to resource: %w", p.ID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type pipelineListOpts struct {
	IO cmdio.Options
}

func (o *pipelineListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PipelineTableCodec{})
	o.IO.RegisterCustomCodec("wide", &PipelineTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (h *fleetHelper) newPipelineGetCommand() *cobra.Command { //nolint:dupl // Intentionally similar to collector get — distinct resource types.
	opts := &pipelineGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <id|name>",
		Short: "Get a pipeline by ID or name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, namespace, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			pipeline, err := resolvePipeline(ctx, client, args[0])
			if err != nil {
				return err
			}

			res, err := PipelineToResource(*pipeline, namespace)
			if err != nil {
				return fmt.Errorf("failed to convert pipeline to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// resolvePipeline looks up a pipeline by slug-id, plain ID, or name.
func resolvePipeline(ctx context.Context, client *Client, ref string) (*Pipeline, error) {
	// Try extracting a numeric ID from the reference (handles "name-123" and "123").
	if id, ok := extractIDFromSlug(ref); ok {
		p, err := client.GetPipeline(ctx, id)
		if err == nil {
			return p, nil
		}
	}
	// Fall back to name-based lookup.
	pipelines, err := client.ListPipelines(ctx)
	if err != nil {
		return nil, fmt.Errorf("fleet: resolve pipeline %q: %w", ref, err)
	}
	for i := range pipelines {
		if pipelines[i].Name == ref {
			return &pipelines[i], nil
		}
	}
	return nil, fmt.Errorf("pipeline %q not found", ref)
}

// resolveCollector looks up a collector by slug-id, plain ID, or name.
func resolveCollector(ctx context.Context, client *Client, ref string) (*Collector, error) {
	// Try extracting a numeric ID from the reference (handles "name-123" and "123").
	if id, ok := extractIDFromSlug(ref); ok {
		c, err := client.GetCollector(ctx, id)
		if err == nil {
			return c, nil
		}
	}
	// Fall back to name-based lookup.
	collectors, err := client.ListCollectors(ctx)
	if err != nil {
		return nil, fmt.Errorf("fleet: resolve collector %q: %w", ref, err)
	}
	for i := range collectors {
		if collectors[i].Name == ref {
			return &collectors[i], nil
		}
	}
	return nil, fmt.Errorf("collector %q not found", ref)
}

type pipelineGetOpts struct {
	IO cmdio.Options
}

func (o *pipelineGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (h *fleetHelper) newPipelineCreateCommand() *cobra.Command { //nolint:dupl // Intentionally similar to collector create — distinct resource types.
	opts := &pipelineWriteOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a pipeline from a file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			pipeline, err := readPipelineFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			created, err := client.CreatePipeline(ctx, *pipeline)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Created pipeline %s (id=%s)", created.Name, created.ID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func (h *fleetHelper) newPipelineUpdateCommand() *cobra.Command {
	opts := &pipelineWriteOpts{}
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a pipeline from a file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			pipeline, err := readPipelineFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			if err := client.UpdatePipeline(ctx, args[0], *pipeline); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated pipeline %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func (h *fleetHelper) newPipelineDeleteCommand() *cobra.Command {
	opts := &pipelineDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a pipeline.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			if err := client.DeletePipeline(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Deleted pipeline %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type pipelineDeleteOpts struct{}

func (o *pipelineDeleteOpts) setup(_ *pflag.FlagSet) {}

func (o *pipelineDeleteOpts) Validate() error { return nil }

type pipelineWriteOpts struct {
	File string
}

func (o *pipelineWriteOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the pipeline manifest (use - for stdin)")
}

func (o *pipelineWriteOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Collector commands
// ---------------------------------------------------------------------------

func (h *fleetHelper) collectorsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "collectors",
		Short:   "Manage Fleet Management collectors.",
		Aliases: []string{"collector"},
	}

	cmd.AddCommand(
		h.newCollectorListCommand(),
		h.newCollectorGetCommand(),
		h.newCollectorCreateCommand(),
		h.newCollectorUpdateCommand(),
		h.newCollectorDeleteCommand(),
	)

	return cmd
}

func (h *fleetHelper) newCollectorListCommand() *cobra.Command {
	opts := &collectorListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List collectors.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, namespace, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			collectors, err := client.ListCollectors(ctx)
			if err != nil {
				return err
			}

			// Table codec operates on raw []Collector for direct field access.
			// Other formats (yaml/json) convert to K8s envelope Resources
			// for consistency with get/pull and round-trip support.
			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), collectors)
			}

			var objs []unstructured.Unstructured
			for _, col := range collectors {
				res, err := CollectorToResource(col, namespace)
				if err != nil {
					return fmt.Errorf("failed to convert collector %s to resource: %w", col.ID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type collectorListOpts struct {
	IO cmdio.Options
}

func (o *collectorListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &CollectorTableCodec{})
	o.IO.RegisterCustomCodec("wide", &CollectorTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (h *fleetHelper) newCollectorGetCommand() *cobra.Command { //nolint:dupl // Intentionally similar to pipeline get — distinct resource types.
	opts := &collectorGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <id|name>",
		Short: "Get a collector by ID or name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, namespace, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			collector, err := resolveCollector(ctx, client, args[0])
			if err != nil {
				return err
			}

			res, err := CollectorToResource(*collector, namespace)
			if err != nil {
				return fmt.Errorf("failed to convert collector to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type collectorGetOpts struct {
	IO cmdio.Options
}

func (o *collectorGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (h *fleetHelper) newCollectorCreateCommand() *cobra.Command { //nolint:dupl // Intentionally similar to pipeline create — distinct resource types.
	opts := &collectorWriteOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a collector from a file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			collector, err := readCollectorFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			created, err := client.CreateCollector(ctx, *collector)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Created collector %s (id=%s)", created.Name, created.ID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func (h *fleetHelper) newCollectorUpdateCommand() *cobra.Command {
	opts := &collectorWriteOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a collector from a file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			collector, err := readCollectorFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			collector.ID = args[0]

			if err := client.UpdateCollector(ctx, *collector); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated collector %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func (h *fleetHelper) newCollectorDeleteCommand() *cobra.Command {
	opts := &collectorDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a collector.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			if err := client.DeleteCollector(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Deleted collector %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type collectorDeleteOpts struct{}

func (o *collectorDeleteOpts) setup(_ *pflag.FlagSet) {}

func (o *collectorDeleteOpts) Validate() error { return nil }

type collectorWriteOpts struct {
	File string
}

func (o *collectorWriteOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the collector manifest (use - for stdin)")
}

func (o *collectorWriteOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tenant commands
// ---------------------------------------------------------------------------

func (h *fleetHelper) tenantCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Manage Fleet Management tenant settings.",
	}

	cmd.AddCommand(h.newTenantLimitsCommand())

	return cmd
}

func (h *fleetHelper) newTenantLimitsCommand() *cobra.Command {
	opts := &tenantLimitsOpts{}
	cmd := &cobra.Command{
		Use:   "limits",
		Short: "Show tenant limits.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := h.loadClient(ctx)
			if err != nil {
				return err
			}

			limits, err := client.GetLimits(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), limits)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type tenantLimitsOpts struct {
	IO cmdio.Options
}

func (o *tenantLimitsOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Table codecs
// ---------------------------------------------------------------------------

// PipelineTableCodec renders pipelines as a tabular table.
type PipelineTableCodec struct {
	Wide bool
}

// Format returns the codec's format identifier.
func (c *PipelineTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes the pipeline list as a table.
func (c *PipelineTableCodec) Encode(w io.Writer, v any) error {
	pipelines, ok := v.([]Pipeline)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Pipeline")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tNAME\tENABLED\tMATCHERS")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tENABLED")
	}

	for _, p := range pipelines {
		enabled := "-"
		if p.Enabled != nil {
			enabled = strconv.FormatBool(*p.Enabled)
		}
		if c.Wide {
			matchers := strings.Join(p.Matchers, ", ")
			if matchers == "" {
				matchers = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.ID, p.Name, enabled, matchers)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", p.ID, p.Name, enabled)
		}
	}

	return tw.Flush()
}

// Decode is not supported for table format.
func (c *PipelineTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// CollectorTableCodec renders collectors as a tabular table.
type CollectorTableCodec struct {
	Wide bool
}

// Format returns the codec's format identifier.
func (c *CollectorTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes the collector list as a table.
func (c *CollectorTableCodec) Encode(w io.Writer, v any) error {
	collectors, ok := v.([]Collector)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Collector")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tNAME\tTYPE\tENABLED\tCREATED_AT")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tTYPE\tENABLED")
	}

	for _, col := range collectors {
		enabled := "-"
		if col.Enabled != nil {
			enabled = strconv.FormatBool(*col.Enabled)
		}

		colType := col.CollectorType
		if colType == "" {
			colType = "-"
		}

		if c.Wide {
			createdAt := "-"
			if col.CreatedAt != nil {
				createdAt = col.CreatedAt.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", col.ID, col.Name, colType, enabled, createdAt)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", col.ID, col.Name, colType, enabled)
		}
	}

	return tw.Flush()
}

// Decode is not supported for table format.
func (c *CollectorTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// Slug helpers — name-ID naming for metadata.name
// ---------------------------------------------------------------------------

// nonAlphanumHyphen matches characters that are not lowercase alphanumeric or hyphens.
//
// Compiled regex for reuse across resource name conversions.
var nonAlphanumHyphen = regexp.MustCompile(`[^a-z0-9-]+`)

// multiHyphen matches consecutive hyphens.
//
// Compiled regex for reuse across resource name conversions.
var multiHyphen = regexp.MustCompile(`-+`)

// slugifyName converts a name to a K8s-safe slug (RFC 1123 subdomain).
func slugifyName(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumHyphen.ReplaceAllString(s, "-")
	s = multiHyphen.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "resource"
	}
	return s
}

// extractIDFromSlug recovers the fleet resource ID from a metadata.name.
// Handles three formats:
//   - "slug-<id>" (e.g. "windowsconfig-18155") — current format
//   - "<id>"      (e.g. "18155")               — plain numeric ID
//   - "<name>"    (e.g. "windowsconfig")        — name-only, returns ("", false)
func extractIDFromSlug(name string) (string, bool) {
	// Pure numeric — treat as ID directly.
	if _, err := strconv.Atoi(name); err == nil {
		return name, true
	}
	// "slug-<id>" — extract numeric suffix after the last hyphen.
	if idx := strings.LastIndex(name, "-"); idx >= 0 {
		suffix := name[idx+1:]
		if _, err := strconv.Atoi(suffix); err == nil {
			return suffix, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Resource conversion helpers
// ---------------------------------------------------------------------------

// PipelineToResource converts a Pipeline to a grafanactl Resource.
// metadata.name is set to "slug-id" (e.g., "windowsconfig-18155") for unique identification.
func PipelineToResource(p Pipeline, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pipeline: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pipeline to map: %w", err)
	}

	// Strip the ID from spec — it lives in metadata.name.
	delete(specMap, "id")

	// Build slug-id name for unique identification.
	name := slugifyName(p.Name)
	if p.ID != "" {
		name = name + "-" + p.ID
	}

	obj := map[string]any{
		"apiVersion": PipelineAPIVersion,
		"kind":       PipelineKind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// PipelineFromResource converts a grafanactl Resource back to a Pipeline.
// The ID is recovered from the slug-id in metadata.name.
func PipelineFromResource(res *resources.Resource) (*Pipeline, error) {
	obj := res.Object.Object

	specRaw, ok := obj["spec"]
	if !ok {
		return nil, errors.New("resource has no spec field")
	}

	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return nil, errors.New("resource spec is not a map")
	}

	data, err := json.Marshal(specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	var p Pipeline
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to pipeline: %w", err)
	}

	// Restore ID from metadata.name slug.
	if id, ok := extractIDFromSlug(res.Raw.GetName()); ok {
		p.ID = id
	}

	return &p, nil
}

// CollectorToResource converts a Collector to a grafanactl Resource.
// metadata.name is set to "slug-id" for unique identification.
func CollectorToResource(col Collector, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(col)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal collector: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal collector to map: %w", err)
	}

	// Strip the ID from spec — it lives in metadata.name.
	delete(specMap, "id")

	// Build slug-id name for unique identification.
	name := slugifyName(col.Name)
	if col.ID != "" {
		name = name + "-" + col.ID
	}

	obj := map[string]any{
		"apiVersion": CollectorAPIVersion,
		"kind":       CollectorKind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// CollectorFromResource converts a grafanactl Resource back to a Collector.
func CollectorFromResource(res *resources.Resource) (*Collector, error) {
	obj := res.Object.Object

	specRaw, ok := obj["spec"]
	if !ok {
		return nil, errors.New("resource has no spec field")
	}

	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return nil, errors.New("resource spec is not a map")
	}

	data, err := json.Marshal(specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	var col Collector
	if err := json.Unmarshal(data, &col); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to collector: %w", err)
	}

	// Restore ID from metadata.name slug.
	if id, ok := extractIDFromSlug(res.Raw.GetName()); ok {
		col.ID = id
	}

	return &col, nil
}

// ---------------------------------------------------------------------------
// File reading helpers
// ---------------------------------------------------------------------------

// readPipelineFromFile reads a K8s-envelope manifest and extracts a Pipeline from its spec.
func readPipelineFromFile(filename string, stdin io.Reader) (*Pipeline, error) {
	var reader io.Reader
	if filename == "-" {
		reader = stdin
	} else {
		f, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", filename, err)
		}
		defer f.Close()
		reader = f
	}

	yamlCodec := format.NewYAMLCodec()
	var obj unstructured.Unstructured
	if err := yamlCodec.Decode(reader, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	res, err := resources.FromUnstructured(&obj)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource from input: %w", err)
	}

	return PipelineFromResource(res)
}

// readCollectorFromFile reads a K8s-envelope manifest and extracts a Collector from its spec.
func readCollectorFromFile(filename string, stdin io.Reader) (*Collector, error) {
	var reader io.Reader
	if filename == "-" {
		reader = stdin
	} else {
		f, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", filename, err)
		}
		defer f.Close()
		reader = f
	}

	yamlCodec := format.NewYAMLCodec()
	var obj unstructured.Unstructured
	if err := yamlCodec.Decode(reader, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	res, err := resources.FromUnstructured(&obj)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource from input: %w", err)
	}

	return CollectorFromResource(res)
}

// ---------------------------------------------------------------------------
// Resource adapter factories
// ---------------------------------------------------------------------------

// CloudConfigLoader can load Grafana Cloud configuration from the active context.
type CloudConfigLoader interface {
	LoadCloudConfig(ctx context.Context) (providers.CloudRESTConfig, error)
}

// NewPipelineTypedCRUD creates a TypedCRUD for Fleet pipelines.
func NewPipelineTypedCRUD(ctx context.Context, loader CloudConfigLoader) (*adapter.TypedCRUD[Pipeline], string, error) {
	cloudCfg, err := loader.LoadCloudConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load Fleet config for pipelines: %w", err)
	}

	url := cloudCfg.Stack.AgentManagementInstanceURL
	if url == "" {
		return nil, "", errors.New("fleet management endpoint is not available for this stack")
	}
	if cloudCfg.Stack.AgentManagementInstanceID == 0 {
		return nil, "", errors.New("fleet management instance ID is not available for this stack")
	}
	instanceID := strconv.Itoa(cloudCfg.Stack.AgentManagementInstanceID)
	httpClient, err := cloudCfg.HTTPClient()
	if err != nil {
		return nil, "", fmt.Errorf("fleet: failed to create HTTP client for pipelines: %w", err)
	}
	client := NewClient(url, instanceID, cloudCfg.Token, true, httpClient)

	crud := &adapter.TypedCRUD[Pipeline]{
		ListFn: client.ListPipelines,
		GetFn: func(ctx context.Context, name string) (*Pipeline, error) {
			return resolvePipeline(ctx, client, name)
		},
		CreateFn: func(ctx context.Context, p *Pipeline) (*Pipeline, error) {
			return client.CreatePipeline(ctx, *p)
		},
		UpdateFn: func(ctx context.Context, name string, p *Pipeline) (*Pipeline, error) {
			id, ok := extractIDFromSlug(name)
			if !ok {
				return nil, fmt.Errorf("cannot determine pipeline ID from name %q: expected format \"<slug>-<id>\" or numeric ID", name)
			}
			if err := client.UpdatePipeline(ctx, id, *p); err != nil {
				return nil, fmt.Errorf("failed to update pipeline %q: %w", id, err)
			}
			updated, err := client.GetPipeline(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("failed to get updated pipeline %q: %w", id, err)
			}
			if updated == nil {
				return nil, fmt.Errorf("pipeline %q not found after update", id)
			}
			return updated, nil
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, ok := extractIDFromSlug(name)
			if !ok {
				return fmt.Errorf("cannot determine pipeline ID from name %q: expected format \"<slug>-<id>\" or numeric ID", name)
			}
			return client.DeletePipeline(ctx, id)
		},
		Namespace:   cloudCfg.Namespace,
		StripFields: []string{"id"},
		Descriptor:  pipelineDescriptorVar,
	}
	return crud, cloudCfg.Namespace, nil
}

// NewPipelineAdapterFactory returns a lazy adapter.Factory for fleet pipelines.
func NewPipelineAdapterFactory(loader CloudConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewPipelineTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewCollectorTypedCRUD creates a TypedCRUD for Fleet collectors.
func NewCollectorTypedCRUD(ctx context.Context, loader CloudConfigLoader) (*adapter.TypedCRUD[Collector], string, error) {
	cloudCfg, err := loader.LoadCloudConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load Fleet config for collectors: %w", err)
	}

	url := cloudCfg.Stack.AgentManagementInstanceURL
	if url == "" {
		return nil, "", errors.New("fleet management endpoint is not available for this stack")
	}
	if cloudCfg.Stack.AgentManagementInstanceID == 0 {
		return nil, "", errors.New("fleet management instance ID is not available for this stack")
	}
	instanceID := strconv.Itoa(cloudCfg.Stack.AgentManagementInstanceID)
	httpClient, err := cloudCfg.HTTPClient()
	if err != nil {
		return nil, "", fmt.Errorf("fleet: failed to create HTTP client for collectors: %w", err)
	}
	client := NewClient(url, instanceID, cloudCfg.Token, true, httpClient)

	crud := &adapter.TypedCRUD[Collector]{
		ListFn: client.ListCollectors,
		GetFn: func(ctx context.Context, name string) (*Collector, error) {
			return resolveCollector(ctx, client, name)
		},
		CreateFn: func(ctx context.Context, col *Collector) (*Collector, error) {
			return client.CreateCollector(ctx, *col)
		},
		UpdateFn: func(ctx context.Context, name string, col *Collector) (*Collector, error) {
			id, ok := extractIDFromSlug(name)
			if !ok {
				return nil, fmt.Errorf("cannot determine collector ID from name %q: expected format \"<slug>-<id>\" or numeric ID", name)
			}
			col.ID = id
			if err := client.UpdateCollector(ctx, *col); err != nil {
				return nil, fmt.Errorf("failed to update collector %q: %w", id, err)
			}
			updated, err := client.GetCollector(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("failed to get updated collector %q: %w", id, err)
			}
			if updated == nil {
				return nil, fmt.Errorf("collector %q not found after update", id)
			}
			return updated, nil
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, ok := extractIDFromSlug(name)
			if !ok {
				return fmt.Errorf("cannot determine collector ID from name %q: expected format \"<slug>-<id>\" or numeric ID", name)
			}
			return client.DeleteCollector(ctx, id)
		},
		Namespace:   cloudCfg.Namespace,
		StripFields: []string{"id"},
		Descriptor:  collectorDescriptorVar,
	}
	return crud, cloudCfg.Namespace, nil
}

// NewCollectorAdapterFactory returns a lazy adapter.Factory for fleet collectors.
func NewCollectorAdapterFactory(loader CloudConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewCollectorTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// ---------------------------------------------------------------------------
// Schema and example helpers
// ---------------------------------------------------------------------------

func pipelineSchema() json.RawMessage {
	s := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/fleet/Pipeline",
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": PipelineAPIVersion},
			"kind":       map[string]any{"type": "string", "const": PipelineKind},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":     map[string]any{"type": "string"},
					"enabled":  map[string]any{"type": "boolean"},
					"contents": map[string]any{"type": "string"},
					"matchers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"name", "contents"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("fleet: failed to marshal pipeline schema: %v", err))
	}
	return b
}

func pipelineExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": PipelineAPIVersion,
		"kind":       PipelineKind,
		"metadata": map[string]any{
			"name": "my-pipeline",
		},
		"spec": map[string]any{
			"name":     "my-pipeline",
			"enabled":  true,
			"contents": "logging { level = \"info\" }",
			"matchers": []string{"collector.os=linux"},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("fleet: failed to marshal pipeline example: %v", err))
	}
	return b
}

func collectorSchema() json.RawMessage {
	s := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/fleet/Collector",
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": CollectorAPIVersion},
			"kind":       map[string]any{"type": "string", "const": CollectorKind},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":              map[string]any{"type": "string"},
					"collector_type":    map[string]any{"type": "string"},
					"enabled":           map[string]any{"type": "boolean"},
					"remote_attributes": map[string]any{"type": "object"},
					"local_attributes":  map[string]any{"type": "object"},
				},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("fleet: failed to marshal collector schema: %v", err))
	}
	return b
}

func collectorExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": CollectorAPIVersion,
		"kind":       CollectorKind,
		"metadata": map[string]any{
			"name": "my-collector",
		},
		"spec": map[string]any{
			"name":           "my-collector",
			"collector_type": "alloy",
			"enabled":        true,
			"remote_attributes": map[string]string{
				"env": "production",
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("fleet: failed to marshal collector example: %v", err))
	}
	return b
}
