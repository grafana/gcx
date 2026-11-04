package kg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// DashboardMatcher is one property matcher on a dashboard config (PropertyMatcherDto).
type DashboardMatcher struct {
	Name  string `json:"name" yaml:"name"`
	Op    string `json:"op" yaml:"op"`
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
	Type  string `json:"type,omitempty" yaml:"type,omitempty"`
	Uom   string `json:"uom,omitempty" yaml:"uom,omitempty"`
}

// DashboardConfig is one dashboard linked to an entity type's KPI drawer
// (CustomDashConfigDto / the plugin's DashboardConfigV2). The backend identifies a
// config by DashboardURI alone. It ignores Name, which the Asserts UI sends, so
// "dashboards get" never returns it.
type DashboardConfig struct {
	DashboardURI      string             `json:"dashboardUri,omitempty" yaml:"dashboardUri,omitempty"`
	UID               string             `json:"uid,omitempty" yaml:"uid,omitempty"`
	Name              string             `json:"name,omitempty" yaml:"name,omitempty"`
	Query             string             `json:"query,omitempty" yaml:"query,omitempty"`
	DashParams        map[string]string  `json:"dashParams,omitempty" yaml:"dashParams,omitempty"`
	DataSourceParam   string             `json:"dataSourceParam,omitempty" yaml:"dataSourceParam,omitempty"`
	AlertCategory     []string           `json:"alertCategory,omitempty" yaml:"alertCategory,omitempty"`
	OverrideEntityURI bool               `json:"overrideEntityUri" yaml:"overrideEntityUri"`
	PropertyMatchers  []DashboardMatcher `json:"propertyMatchers,omitempty" yaml:"propertyMatchers,omitempty"`
}

// DashboardConfigsByType is the per-entity-type payload (CustomDashConfigsDto).
// `dashboards get -o yaml` prints it, and upsert and delete accept it back.
type DashboardConfigsByType struct {
	EntityType string            `json:"entityType" yaml:"entityType"`
	Configs    []DashboardConfig `json:"dashConfigDto" yaml:"dashConfigDto"`
}

// dashboardConfigsResponse is the collection payload (CustomKpiDashConfigDto list).
type dashboardConfigsResponse struct {
	KPIDashConfigs []DashboardConfigsByType `json:"kpiDashConfigs" yaml:"kpiDashConfigs"`
}

// dashboardRow is one config tagged with its entity type, as `dashboards list` prints it.
type dashboardRow struct {
	DashboardConfig `yaml:",inline"`

	EntityType string `json:"entityType" yaml:"entityType"`
}

// dashboardOps maps every accepted spelling of a property matcher operator to its
// wire value. The wire values are the backend's MatcherOp operators; the
// underscored names are the enum keys that issue #769 uses.
//
//nolint:gochecknoglobals // read-only lookup table
var dashboardOps = map[string]string{
	"=": "=", "EQUALS": "=",
	"<>": "<>", "NOT_EQUALS": "<>",
	"STARTS WITH": "STARTS WITH", "STARTS_WITH": "STARTS WITH",
	"ENDS WITH": "ENDS WITH", "ENDS_WITH": "ENDS WITH",
	"CONTAINS": "CONTAINS",
	"IN":       "IN",
	"IS NULL":  "IS NULL", "IS_NULL": "IS NULL",
	"IS NOT NULL": "IS NOT NULL", "IS_NOT_NULL": "IS NOT NULL",
	">": ">", ">=": ">=", "<": "<", "<=": "<=",
}

// normalize validates the config the way the backend does and rewrites it into
// the form the backend stores: matcher ops become wire values, and the dashboard
// URI is cut to its last three path segments (/d/<uid>/<slug>). The backend does
// the same trimming on upsert but not on delete, where the URI must equal the
// stored one, so trimming here keeps both calls consistent.
func (d *DashboardConfig) normalize() error {
	if strings.TrimSpace(d.DashboardURI) == "" {
		return errors.New("dashboardUri is required (for example /d/<uid>/<slug>); uid alone is not enough")
	}
	if parts := strings.Split(d.DashboardURI, "/"); len(parts) > 3 {
		d.DashboardURI = "/" + strings.Join(parts[len(parts)-3:], "/")
	}
	for i := range d.PropertyMatchers {
		m := &d.PropertyMatchers[i]
		if m.Name == "" {
			return fmt.Errorf("propertyMatchers[%d] has an empty name", i)
		}
		op, ok := dashboardOps[strings.ToUpper(strings.TrimSpace(m.Op))]
		if !ok {
			return fmt.Errorf("propertyMatchers[%d] has unsupported op %q: use one of %s",
				i, m.Op, dashboardOpList)
		}
		m.Op = op
		if op != "IS NULL" && op != "IS NOT NULL" && strings.TrimSpace(m.Value) == "" {
			return fmt.Errorf("propertyMatchers[%d] needs a value for op %q", i, op)
		}
		if m.Type == "" {
			m.Type = "String" // what the Asserts UI sends
		}
	}
	return nil
}

// dashboardOpList is the operator hint shown for an unsupported op.
const dashboardOpList = "=, <>, <, >, <=, >=, IS NULL, IS NOT NULL, STARTS WITH, ENDS WITH, CONTAINS, IN " +
	"(or EQUALS, NOT_EQUALS, STARTS_WITH, ENDS_WITH, IS_NULL, IS_NOT_NULL)"

// label names a config in mutation results and error messages.
func (d *DashboardConfig) label() string {
	for _, v := range []string{d.Name, d.UID, d.DashboardURI} {
		if v != "" {
			return v
		}
	}
	return ""
}

// ListDashboards fetches the dashboard configs of every entity type.
func (c *Client) ListDashboards(ctx context.Context) ([]DashboardConfigsByType, error) {
	var out dashboardConfigsResponse
	if err := c.GetJSON(ctx, dashboardConfigPath, &out); err != nil {
		return nil, err
	}
	return out.KPIDashConfigs, nil
}

// GetDashboards fetches the dashboard configs of one entity type.
func (c *Client) GetDashboards(ctx context.Context, entityType string) (*DashboardConfigsByType, error) {
	var out DashboardConfigsByType
	if err := c.GetJSON(ctx, fmt.Sprintf(dashboardConfigByTypeFmt, url.PathEscape(entityType)), &out); err != nil {
		return nil, err
	}
	if out.EntityType == "" {
		out.EntityType = entityType
	}
	return &out, nil
}

// UpsertDashboard links one dashboard to an entity type (create or update).
func (c *Client) UpsertDashboard(ctx context.Context, entityType string, cfg DashboardConfig) error {
	return c.postDashboard(ctx, fmt.Sprintf(dashboardConfigByTypeFmt, url.PathEscape(entityType)), cfg)
}

// DeleteDashboard unlinks one dashboard from an entity type.
//
// The backend (grafana/asserts-adi CustomKpiDashService.deleteCustomConfig)
// removes the entity type's config whose dashboardUri equals cfg.DashboardURI
// exactly and ignores every other field. An unknown URI is not an error: it
// returns 200 and changes nothing. gcx still posts the whole config, as the
// Asserts UI does.
func (c *Client) DeleteDashboard(ctx context.Context, entityType string, cfg DashboardConfig) error {
	return c.postDashboard(ctx, fmt.Sprintf(dashboardConfigDeleteByType, url.PathEscape(entityType)), cfg)
}

// postDashboard POSTs one config. The backend answers an invalid config with HTTP
// 422 and per-field errors, which validateConfig turns into a *ConfigValidationError
// so the failing field is shown instead of a bare "Invalid request".
func (c *Client) postDashboard(ctx context.Context, path string, cfg DashboardConfig) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("kg: marshal request body: %w", err)
	}
	return c.validateConfig(ctx, path, "application/json", body, "dashboard config")
}

func newDashboardsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboards",
		Short: "Manage the dashboards shown in the entity KPI drawer.",
		Long: `Manage the dashboard configs that decide which dashboards appear in an entity
type's KPI drawer in the Knowledge Graph.

Each config links a dashboard (uid or dashboardUri) to an entity type, optionally
restricted by property matchers and with dashboard variables mapped to entity
properties (dashParams).`,
	}

	listOpts := &dashboardsListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List dashboard configs for every entity type.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := listOpts.Validate(); err != nil {
				return err
			}
			client, err := newKGClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			groups, err := client.ListDashboards(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]dashboardRow, 0)
			for _, g := range groups {
				for _, c := range g.Configs {
					rows = append(rows, dashboardRow{EntityType: g.EntityType, DashboardConfig: c})
				}
			}
			return listOpts.IO.Encode(cmd.OutOrStdout(), rows)
		},
	}
	listOpts.setup(listCmd.Flags())

	getOpts := &dashboardsGetOpts{}
	getCmd := &cobra.Command{
		Use:   "get <entity-type>",
		Short: "Get the dashboard configs of one entity type.",
		Long: `Prints the dashboard configs of one entity type. The YAML output can be edited
and passed back to "dashboards upsert" or "dashboards delete".`,
		Example: `  gcx kg dashboards get Service`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			getOpts.Type = args[0]
			if err := getOpts.Validate(); err != nil {
				return err
			}
			client, err := newKGClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			result, err := client.GetDashboards(cmd.Context(), getOpts.Type)
			if err != nil {
				return err
			}
			return getOpts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	getOpts.setup(getCmd.Flags())

	upsertOpts := &dashboardsWriteOpts{}
	upsertCmd := &cobra.Command{
		Use:   "upsert <entity-type>",
		Short: "Link one or more dashboards to an entity type.",
		Long: `Creates or updates dashboard configs for an entity type from a YAML file or stdin.
Configs on the entity type that are absent from the file are never removed.

The input may be one config, a list of configs, or the entityType/dashConfigDto
document printed by "dashboards get". Field names are case-sensitive and unknown
fields are rejected. The backend identifies a config by dashboardUri, so
upserting an existing dashboardUri replaces that config; dashboardUri is required
and is cut to its last three path segments. Property matcher ops accept the wire
symbols (= <> > >= < <=), the spelled-out forms (STARTS WITH, ENDS WITH, CONTAINS,
IN, IS NULL, IS NOT NULL) and the underscored names (EQUALS, NOT_EQUALS,
STARTS_WITH, ENDS_WITH, IS_NULL, IS_NOT_NULL); they are normalized before sending.
Matcher values must be non-empty except for IS NULL and IS NOT NULL.`,
		Example: `  echo 'dashboardUri: /d/kafka-overview/kafka-overview
uid: kafka-overview
overrideEntityUri: false
propertyMatchers:
  - name: service_type
    op: EQUALS
    value: kafka
dashParams:
  cluster: cluster_name' | gcx kg dashboards upsert Service`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			upsertOpts.Type = args[0]
			if err := upsertOpts.Validate(); err != nil {
				return err
			}
			configs, err := upsertOpts.load(cmd)
			if err != nil {
				return err
			}
			client, err := newKGClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			return runDashboardBatch(cmd, &upsertOpts.IO, "upserted", configs,
				func(ctx context.Context, c DashboardConfig) error {
					return client.UpsertDashboard(ctx, upsertOpts.Type, c)
				})
		},
	}
	upsertOpts.setup(upsertCmd.Flags(), "upserted")

	deleteOpts := &dashboardsWriteOpts{destructive: true}
	deleteCmd := &cobra.Command{
		Use:   "delete <entity-type>",
		Short: "Unlink one or more dashboards from an entity type.",
		Long: `Unlinks the dashboard configs in the file from an entity type. The backend matches
on dashboardUri alone, so only that field has to be right; pass the configs as
"dashboards get" prints them. A dashboardUri that is not linked is not an error
and removes nothing. Accepts the same input shapes as "dashboards upsert".`,
		Example: `  gcx kg dashboards get Service -o yaml > dashboards.yaml
  # keep only the configs to remove
  gcx kg dashboards delete Service -f dashboards.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deleteOpts.Type = args[0]
			if err := deleteOpts.Validate(); err != nil {
				return err
			}
			configs, err := deleteOpts.load(cmd)
			if err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), deleteOpts.Force,
				fmt.Sprintf("Delete %d dashboard config(s) from entity type %q?", len(configs), deleteOpts.Type))
			if err != nil {
				return err
			}
			if !proceed {
				exitCancelled := gcxerrors.ExitCancelled
				return &gcxerrors.DetailedError{
					Summary:  "operation cancelled by user",
					Details:  "Confirmation prompt was declined; no dashboard configs were deleted.",
					ExitCode: &exitCancelled,
				}
			}
			client, err := newKGClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			return runDashboardBatch(cmd, &deleteOpts.IO, "deleted", configs,
				func(ctx context.Context, c DashboardConfig) error {
					return client.DeleteDashboard(ctx, deleteOpts.Type, c)
				})
		},
	}
	deleteOpts.setup(deleteCmd.Flags(), "deleted")
	deleteCmd.Flags().BoolVar(&deleteOpts.Force, "force", false, "Skip confirmation prompt")

	cmd.AddCommand(listCmd, getCmd, upsertCmd, deleteCmd)
	return cmd
}

func newKGClient(ctx context.Context, loader RESTConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}
	return NewClient(cfg)
}

type dashboardsListOpts struct{ IO cmdio.Options }

func (o *dashboardsListOpts) Validate() error { return o.IO.Validate() }

func (o *dashboardsListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, DashboardTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// DashboardTable renders `dashboards list` rows.
func DashboardTable() cmdio.Table[dashboardRow] {
	return cmdio.Table[dashboardRow]{Columns: []cmdio.Column[dashboardRow]{
		{Header: "ENTITY TYPE", Content: func(r dashboardRow) string { return r.EntityType }},
		{Header: "NAME", Content: func(r dashboardRow) string { return r.Name }},
		{Header: "UID", Content: func(r dashboardRow) string { return r.UID }},
		{Header: "URI", Content: func(r dashboardRow) string { return r.DashboardURI }},
		{Header: "MATCHERS", Content: func(r dashboardRow) string { return strconv.Itoa(len(r.PropertyMatchers)) }},
	}}
}

type dashboardsGetOpts struct {
	Type string // positional: the entity type, e.g. Service
	IO   cmdio.Options
}

func (o *dashboardsGetOpts) Validate() error {
	if err := validateDashboardEntityType(o.Type); err != nil {
		return err
	}
	return o.IO.Validate()
}

func (o *dashboardsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

// dashboardsWriteOpts is shared by upsert and delete: an entity type, an input
// file and the batch mutation output.
type dashboardsWriteOpts struct {
	Type  string // positional: the entity type, e.g. Service
	File  string
	Force bool
	IO    cmdio.Options

	// destructive marks delete, which prompts and so cannot read its input from stdin.
	destructive bool
}

// validateDashboardEntityType rejects an empty entity type, which would turn the
// per-type path into the collection path.
func validateDashboardEntityType(entityType string) error {
	if strings.TrimSpace(entityType) == "" {
		return errors.New("entity type must not be empty")
	}
	return nil
}

// Validate checks the entity type, the output options and, for delete, that the
// confirmation prompt has a terminal to read from.
func (o *dashboardsWriteOpts) Validate() error {
	if err := validateDashboardEntityType(o.Type); err != nil {
		return err
	}
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.destructive && o.File == "-" && !o.Force {
		return errors.New("-f - reads the whole of stdin, leaving nothing for the confirmation prompt: pass --force, or use a file path")
	}
	return nil
}

func (o *dashboardsWriteOpts) setup(flags *pflag.FlagSet, verb string) {
	flags.StringVarP(&o.File, "file", "f", "", "Input file (YAML), or '-' for stdin. Reads from stdin if omitted.")
	o.IO.RegisterCustomCodec("text", &dashboardsMutationTextCodec{verb: verb})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

// load reads the input and returns its normalized configs. Call Validate first.
func (o *dashboardsWriteOpts) load(cmd *cobra.Command) ([]DashboardConfig, error) {
	data, err := readFileOrStdin(cmd, o.File)
	if err != nil {
		return nil, err
	}
	configs, err := parseDashboardConfigs(data, o.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dashboards file: %w", err)
	}
	if len(configs) == 0 {
		return nil, errors.New("no dashboard configs found in file")
	}
	for i := range configs {
		if err := configs[i].normalize(); err != nil {
			return nil, fmt.Errorf("dashboard config %d: %w", i, err)
		}
	}
	return configs, nil
}

// parseDashboardConfigs accepts one config, a list of configs, or the
// entityType/dashConfigDto document `dashboards get` prints. Unknown fields and
// extra YAML documents are rejected so a misspelled key is not silently dropped.
func parseDashboardConfigs(data []byte, entityType string) ([]DashboardConfig, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 {
		return nil, nil
	}
	decodeStrict := func(v any) error {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(v); err != nil {
			return err
		}
		for {
			var extra yaml.Node
			err := decoder.Decode(&extra)
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if len(extra.Content) > 0 && extra.Content[0].Tag != "!!null" {
				return errors.New("multiple YAML documents are not supported; put the configs in one list")
			}
		}
	}

	root := document.Content[0]
	switch root.Kind {
	case yaml.SequenceNode:
		var configs []DashboardConfig
		return configs, decodeStrict(&configs)
	case yaml.MappingNode:
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value != "dashConfigDto" {
				continue
			}
			var wrapped DashboardConfigsByType
			if err := decodeStrict(&wrapped); err != nil {
				return nil, err
			}
			if wrapped.EntityType != "" && wrapped.EntityType != entityType {
				return nil, fmt.Errorf("file is for entity type %q but the requested entity type is %q", wrapped.EntityType, entityType)
			}
			return wrapped.Configs, nil
		}
		var config DashboardConfig
		if err := decodeStrict(&config); err != nil {
			return nil, err
		}
		return []DashboardConfig{config}, nil
	default:
		return nil, errors.New("expected a dashboard config, a list of configs, or an entityType/dashConfigDto document")
	}
}

// runDashboardBatch applies fn to each config in order. The first failure stops
// the run: with nothing applied it returns the plain error, otherwise it emits the
// partial-failure result document and exits with ExitPartialFailure.
func runDashboardBatch(cmd *cobra.Command, io *cmdio.Options, action string, configs []DashboardConfig,
	fn func(ctx context.Context, c DashboardConfig) error,
) error {
	total := len(configs)
	result := cmdio.NewBatchMutation(action)
	for i, c := range configs {
		if err := fn(cmd.Context(), c); err != nil {
			failure := fmt.Errorf("failed to apply dashboard config %q (%d/%d succeeded): %w", c.label(), i, total, err)
			if i == 0 {
				return failure
			}
			result.Summary = cmdio.MutationSummary{Succeeded: i, Failed: 1, Skipped: total - i - 1}
			result.Failures = append(result.Failures, cmdio.MutationFailure{
				Target: cmdio.MutationTarget{Kind: "DashboardConfig", Name: c.label()},
				Error:  err.Error(),
			})
			cmdio.EmitWarn(cmd.ErrOrStderr(), failure.Error())
			if encErr := io.Encode(cmd.OutOrStdout(), result); encErr != nil {
				return encErr
			}
			return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, failure)
		}
	}
	result.Summary = cmdio.MutationSummary{Succeeded: total}
	return io.Encode(cmd.OutOrStdout(), result)
}

// dashboardsMutationTextCodec renders the BatchMutation of upsert and delete as a
// one-line summary in the default text format.
type dashboardsMutationTextCodec struct{ verb string }

func (c *dashboardsMutationTextCodec) Format() format.Format { return "text" }

func (c *dashboardsMutationTextCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(cmdio.BatchMutation)
	if !ok {
		return errors.New("invalid data type for text codec: expected cmdio.BatchMutation")
	}
	if result.Summary.Failed > 0 {
		cmdio.Warning(w, "%d dashboard config(s) %s, %d failed (%d skipped)",
			result.Summary.Succeeded, c.verb, result.Summary.Failed, result.Summary.Skipped)
		return nil
	}
	cmdio.Success(w, "%d dashboard config(s) %s", result.Summary.Succeeded, c.verb)
	return nil
}

func (c *dashboardsMutationTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}
