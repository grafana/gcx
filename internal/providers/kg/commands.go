package kg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/grafana/grafanactl/internal/format"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scopeFlags holds the --env/--namespace/--site/--since flags.
type scopeFlags struct {
	env       string
	namespace string
	site      string
	since     string
}

func (f *scopeFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.env, "env", "", "Environment scope")
	cmd.Flags().StringVar(&f.namespace, "namespace", "", "Namespace scope")
	cmd.Flags().StringVar(&f.site, "site", "", "Site scope")
	cmd.Flags().StringVar(&f.since, "since", "", "Duration ago (e.g. 1h, 30m, 7d) — default 1h")
}

func (f *scopeFlags) resolveTime() (int64, int64, error) {
	return resolveTimeEpochMs(f.since)
}

func (f *scopeFlags) scopeCriteria() *ScopeCriteria {
	vals := map[string][]string{}
	if f.env != "" {
		vals["env"] = []string{f.env}
	}
	if f.site != "" {
		vals["site"] = []string{f.site}
	}
	if f.namespace != "" {
		vals["namespace"] = []string{f.namespace}
	}
	if len(vals) == 0 {
		return nil
	}
	return &ScopeCriteria{NameAndValues: vals}
}

func (f *scopeFlags) scopeMap() map[string]string {
	scope := map[string]string{}
	if f.env != "" {
		scope["env"] = f.env
	}
	if f.site != "" {
		scope["site"] = f.site
	}
	if f.namespace != "" {
		scope["namespace"] = f.namespace
	}
	if len(scope) == 0 {
		return nil
	}
	return scope
}

func resolveTimeEpochMs(since string) (int64, int64, error) {
	now := time.Now().UnixMilli()
	if since == "" {
		return now - 3600000, now, nil
	}
	d, err := time.ParseDuration(since)
	//nolint:nestif
	if err != nil {
		// Try common suffixes: 7d, 30d, etc.
		if strings.HasSuffix(since, "d") {
			days := since[:len(since)-1]
			var n int
			if _, err := fmt.Sscanf(days, "%d", &n); err == nil {
				d = time.Duration(n) * 24 * time.Hour
			} else {
				return 0, 0, fmt.Errorf("invalid duration %q", since)
			}
		} else {
			return 0, 0, fmt.Errorf("invalid duration %q: %w", since, err)
		}
	}
	return now - d.Milliseconds(), now, nil
}

func parseEntityArg(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", errors.New("entity argument required (e.g. Service--my-service)")
	}
	const sep = "--"
	idx := strings.Index(args[0], sep)
	if idx <= 0 || idx+len(sep) >= len(args[0]) {
		return "", "", fmt.Errorf("entity argument must be Type--Name (e.g. Service--my-service), got: %q", args[0])
	}
	return args[0][:idx], args[0][idx+len(sep):], nil
}

func resolveEntityTypeAndName(cmd *cobra.Command, args []string) (string, string, error) {
	if len(args) > 0 {
		return parseEntityArg(args)
	}
	name, _ := cmd.Flags().GetString("name")
	entityType, _ := cmd.Flags().GetString("type")
	if entityType == "" || name == "" {
		return "", "", errors.New("entity type and name required: use positional arg (Type--Name) or --type/--name flags")
	}
	return entityType, name, nil
}

func toAnyMap(m map[string]string) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func scopeStr(scope map[string]string) string {
	if len(scope) == 0 {
		return ""
	}
	parts := make([]string, 0, len(scope))
	for k, v := range scope {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func readFileOrStdin(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path)
}

// searchByTypes fans out Search across multiple entity types and merges results.
func searchByTypes(ctx context.Context, client *Client, entityTypes []string, assertionsOnly bool, sc *ScopeCriteria, startMs, endMs int64, pageNum int) ([]SearchResult, error) {
	if startMs == 0 && endMs == 0 {
		now := time.Now().UnixMilli()
		startMs = now - 3600000
		endMs = now
	}
	var allResults []SearchResult
	for _, et := range entityTypes {
		req := SearchRequest{
			TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
			FilterCriteria: []EntityMatcher{{
				EntityType:       et,
				HavingAssertion:  assertionsOnly,
				PropertyMatchers: []PropertyMatcher{{Name: "name", Op: "IS NOT NULL", Type: "String"}},
			}},
			ScopeCriteria: sc,
			PageNum:       pageNum,
		}
		results, err := client.Search(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("search entity type %s: %w", et, err)
		}
		allResults = append(allResults, results...)
	}
	if allResults == nil {
		return []SearchResult{}, nil
	}
	return allResults, nil
}

func collectEntityTypes(cmd *cobra.Command, client *Client) ([]string, error) {
	counts, err := client.CountEntityTypes(cmd.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get entity types: %w", err)
	}
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	return types, nil
}

func resolveEntityTypes(cmd *cobra.Command, client *Client, entityType string) ([]string, error) {
	if entityType != "" {
		return []string{entityType}, nil
	}
	return collectEntityTypes(cmd, client)
}

// ---------------------------------------------------------------------------
// Lifecycle commands
// ---------------------------------------------------------------------------

func newSetupCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &setupOpts{}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize the Knowledge Graph plugin.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.Setup(cmd.Context()); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Knowledge Graph plugin initialized")
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type setupOpts struct{}

func (o *setupOpts) setup(_ *pflag.FlagSet) {}

func (o *setupOpts) Validate() error { return nil }

func newEnableCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &enableOpts{}
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable the Knowledge Graph feature.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.Enable(cmd.Context()); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Knowledge Graph enabled")
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type enableOpts struct{}

func (o *enableOpts) setup(_ *pflag.FlagSet) {}

func (o *enableOpts) Validate() error { return nil }

func newStatusCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &statusOpts{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Knowledge Graph status and entity counts.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			status, err := client.GetStatus(cmd.Context())
			if err != nil {
				return err
			}
			counts, err := client.CountEntityTypes(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), map[string]any{
				"status":       status,
				"entityCounts": counts,
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type statusOpts struct {
	IO cmdio.Options
}

func (o *statusOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Datasets commands
// ---------------------------------------------------------------------------

func newDatasetsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "datasets",
		Short: "Manage Knowledge Graph datasets.",
	}

	listOpts := &datasetsListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List Knowledge Graph datasets.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := listOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			result, err := client.GetDatasets(cmd.Context())
			if err != nil {
				return err
			}
			return listOpts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	listOpts.setup(listCmd.Flags())

	var activateFile string
	activateCmd := &cobra.Command{
		Use:   "activate <name>",
		Short: "Activate a dataset.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readFileOrStdin(cmd, activateFile)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var config DatasetConfig
			if err := yaml.Unmarshal(data, &config); err != nil { //nolint:musttag
				return fmt.Errorf("invalid YAML: %w", err)
			}
			restCfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			if err := client.ActivateDataset(cmd.Context(), args[0], config); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Dataset %s activated", args[0])
			return nil
		},
	}
	activateCmd.Flags().StringVarP(&activateFile, "file", "f", "", "File containing dataset config (YAML)")
	_ = activateCmd.MarkFlagRequired("file")

	cmd.AddCommand(listCmd, activateCmd)
	return cmd
}

type datasetsListOpts struct {
	IO cmdio.Options
}

func (o *datasetsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &DatasetTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// DatasetTableCodec renders datasets as a table.
type DatasetTableCodec struct{}

func (c *DatasetTableCodec) Format() format.Format { return "table" }

func (c *DatasetTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*DatasetsResponse)
	if !ok {
		return errors.New("invalid data type for table codec: expected *DatasetsResponse")
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDETECTED\tENABLED\tCONFIGURED")
	for _, item := range resp.Items {
		fmt.Fprintf(tw, "%s\t%v\t%v\t%v\n", item.Name, item.Detected, item.Enabled, item.Configured)
	}
	return tw.Flush()
}

func (c *DatasetTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// Vendors command
// ---------------------------------------------------------------------------

//nolint:dupl
//nolint:dupl
func newVendorsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vendors",
		Short: "Manage Knowledge Graph vendors.",
	}

	listOpts := &vendorsListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List detected vendors.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := listOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			vendors, err := client.GetVendors(cmd.Context())
			if err != nil {
				return err
			}
			return listOpts.IO.Encode(cmd.OutOrStdout(), vendors)
		},
	}
	listOpts.setup(listCmd.Flags())

	cmd.AddCommand(listCmd)
	return cmd
}

type vendorsListOpts struct {
	IO cmdio.Options
}

func (o *vendorsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Rules commands
// ---------------------------------------------------------------------------

func newRulesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage Knowledge Graph prom rules.",
	}

	rulesListOpts := &rulesListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List Knowledge Graph prom rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rulesListOpts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}
			typedObjs, err := crud.List(ctx)
			if err != nil {
				return err
			}

			// Extract rules from TypedObject
			rules := make([]Rule, len(typedObjs))
			for i := range typedObjs {
				rules[i] = typedObjs[i].Spec
			}

			// Table codec operates on raw []Rule for direct field access.
			// Other formats (yaml/json) convert to K8s envelope Resources
			// for consistency with get and round-trip support.
			if rulesListOpts.IO.OutputFormat == "table" {
				return rulesListOpts.IO.Encode(cmd.OutOrStdout(), rules)
			}

			var objs []unstructured.Unstructured
			for _, rule := range rules {
				res, err := RuleToResource(rule, cfg.Namespace)
				if err != nil {
					return fmt.Errorf("failed to convert rule %s to resource: %w", rule.Name, err)
				}
				objs = append(objs, res.ToUnstructured())
			}

			return rulesListOpts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	rulesListOpts.setup(listCmd.Flags())

	getOpts := &rulesGetOpts{}
	getCmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a Knowledge Graph prom rule by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := getOpts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}
			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			// Convert to K8s envelope Resource for all formats.
			res, err := RuleToResource(typedObj.Spec, cfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert rule %s to resource: %w", typedObj.Spec.Name, err)
			}

			return getOpts.IO.Encode(cmd.OutOrStdout(), res.ToUnstructured())
		},
	}
	getOpts.setup(getCmd.Flags())

	var createFile string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Upload Knowledge Graph prom rules from a YAML file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, createFile)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.UploadPromRules(cmd.Context(), string(data)); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Knowledge Graph rules uploaded")
			return nil
		},
	}
	createCmd.Flags().StringVarP(&createFile, "file", "f", "", "Input file (YAML)")
	_ = createCmd.MarkFlagRequired("file")

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete all Knowledge Graph rules (upload empty).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.UploadPromRules(cmd.Context(), ""); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Knowledge Graph rules cleared")
			return nil
		},
	}

	cmd.AddCommand(listCmd, getCmd, createCmd, deleteCmd)
	return cmd
}

type rulesListOpts struct {
	IO cmdio.Options
}

func (o *rulesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &RuleTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

type rulesGetOpts struct {
	IO cmdio.Options
}

func (o *rulesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

// RuleTableCodec renders rules as a table.
type RuleTableCodec struct{}

func (c *RuleTableCodec) Format() format.Format { return "table" }

func (c *RuleTableCodec) Encode(w io.Writer, v any) error {
	rules, ok := v.([]Rule)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Rule")
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tRECORD\tALERT\tEXPR")
	for _, r := range rules {
		expr := r.Expr
		if len(expr) > 60 {
			expr = expr[:57] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Record, r.Alert, expr)
	}
	return tw.Flush()
}

func (c *RuleTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// Model rules, suppressions, relabel rules commands
// ---------------------------------------------------------------------------

//nolint:dupl
func newModelRulesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model-rules",
		Short: "Push model rules to the Knowledge Graph.",
	}
	var fileFlag string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Upload model rules from a YAML file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, fileFlag)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.UploadModelRules(cmd.Context(), string(data)); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Model rules uploaded")
			return nil
		},
	}
	createCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Input file (YAML)")
	_ = createCmd.MarkFlagRequired("file")
	cmd.AddCommand(createCmd)
	return cmd
	//nolint:dupl
}

//nolint:dupl
func newSuppressionsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suppressions",
		Short: "Push suppressions to the Knowledge Graph.",
	}
	var fileFlag string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Upload suppressions from a YAML file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, fileFlag)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.UploadSuppressions(cmd.Context(), string(data)); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Suppressions uploaded")
			return nil
		},
	}
	createCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Input file (YAML)")
	_ = createCmd.MarkFlagRequired("file")
	cmd.AddCommand(createCmd)
	//nolint:dupl
	return cmd
}

//nolint:dupl
func newRelabelRulesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relabel-rules",
		Short: "Push relabel rules to the Knowledge Graph.",
	}
	var fileFlag string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Upload relabel rules from a YAML file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, fileFlag)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.UploadRelabelRules(cmd.Context(), string(data)); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Relabel rules uploaded")
			return nil
		},
	}
	createCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Input file (YAML)")
	_ = createCmd.MarkFlagRequired("file")
	cmd.AddCommand(createCmd)
	return cmd
}

func newServiceDashboardCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service-dashboard",
		Short: "Configure service dashboard settings.",
	}
	var fileFlag string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Configure service dashboard from a file (YAML).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, fileFlag)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var config ServiceDashboardConfig
			if err := yaml.Unmarshal(data, &config); err != nil {
				return fmt.Errorf("invalid YAML: %w", err)
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.AddServiceDashboard(cmd.Context(), config); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Service dashboard configured")
			return nil
		},
	}
	createCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Input file (YAML)")
	_ = createCmd.MarkFlagRequired("file")
	cmd.AddCommand(createCmd)
	return cmd
}

//nolint:dupl
func newKPIDisplayCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kpi-display",
		Short: "Configure KPI display settings.",
	}
	var fileFlag string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Configure KPI display from a file (YAML).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, fileFlag)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var config KPIDisplayConfig
			if err := yaml.Unmarshal(data, &config); err != nil {
				return fmt.Errorf("invalid YAML: %w", err)
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.ConfigureKPIDisplay(cmd.Context(), &config); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "KPI display configured")
			return nil
		},
	}
	createCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Input file (YAML)")
	//nolint:dupl
	_ = createCmd.MarkFlagRequired("file")
	cmd.AddCommand(createCmd)
	return cmd
}

//nolint:dupl
func newFrontendRulesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "frontend-rules",
		Short: "Push frontend observability rules.",
	}
	var fileFlag string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Upload frontend observability rules from a file (YAML).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, fileFlag)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var rules FrontendO11yRuleGroup
			if err := yaml.Unmarshal(data, &rules); err != nil {
				return fmt.Errorf("invalid YAML: %w", err)
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.UploadFrontendO11yRules(cmd.Context(), &rules); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Frontend observability rules uploaded")
			return nil
		},
	}
	createCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Input file (YAML)")
	_ = createCmd.MarkFlagRequired("file")
	cmd.AddCommand(createCmd)
	return cmd
}

// ---------------------------------------------------------------------------
// Environment commands
// ---------------------------------------------------------------------------

func newEnvCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage Knowledge Graph environment configuration.",
	}

	getOpts := &envGetOpts{}
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get the current environment configuration.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := getOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			envCfg, err := client.GetEnvironment(cmd.Context())
			if err != nil {
				return err
			}
			return getOpts.IO.Encode(cmd.OutOrStdout(), envCfg)
		},
	}
	getOpts.setup(getCmd.Flags())

	var setFile string
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Configure environment from a file (YAML).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, setFile)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var envCfg EnvironmentConfig
			if err := yaml.Unmarshal(data, &envCfg); err != nil {
				return fmt.Errorf("invalid YAML: %w", err)
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.ConfigureEnvironment(cmd.Context(), envCfg); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Environment configured")
			return nil
		},
	}
	setCmd.Flags().StringVarP(&setFile, "file", "f", "", "Input file (YAML)")
	_ = setCmd.MarkFlagRequired("file")

	cmd.AddCommand(getCmd, setCmd)
	return cmd
}

type envGetOpts struct {
	IO cmdio.Options
}

func (o *envGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Entities commands
// ---------------------------------------------------------------------------

func newEntitiesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entities",
		Short: "Manage Knowledge Graph entities.",
	}

	// show subcommand: no args = list all, with <name> = single entity
	var (
		showType       string
		showScope      scopeFlags
		assertionsOnly bool
		page           int
	)
	ioOpts := &entitiesShowOpts{}
	showCmd := &cobra.Command{
		Use:   "show [name]",
		Short: "Show entities. Without a name, lists all; with a name, shows one.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ioOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}

			// Single entity mode: name provided
			if len(args) == 1 {
				return showSingleEntity(cmd, client, showType, args[0], &showScope, &ioOpts.IO)
			}

			// List mode: no name
			entityTypes, err := resolveEntityTypes(cmd, client, showType)
			if err != nil {
				return err
			}
			results, err := searchByTypes(cmd.Context(), client, entityTypes, assertionsOnly, showScope.scopeCriteria(), 0, 0, page)
			if err != nil {
				return err
			}
			return ioOpts.IO.Encode(cmd.OutOrStdout(), results)
		},
	}
	showCmd.Flags().StringVar(&showType, "type", "", "Entity type (required for single entity, optional for list)")
	showCmd.Flags().BoolVar(&assertionsOnly, "assertions-only", false, "Only return entities with active assertions (list mode)")
	showCmd.Flags().IntVar(&page, "page", 0, "Page number, 0-based (list mode)")
	showScope.register(showCmd)
	ioOpts.setup(showCmd.Flags())

	cmd.AddCommand(showCmd)
	return cmd
}

func showSingleEntity(cmd *cobra.Command, client *Client, entityType, name string, scope *scopeFlags, io *cmdio.Options) error {
	if entityType == "" {
		return errors.New("--type is required when showing a single entity")
	}
	if sm := scope.scopeMap(); sm != nil {
		info, err := client.GetEntityInfo(cmd.Context(), entityType, name, sm, 0, 0)
		if err != nil {
			return err
		}
		return io.Encode(cmd.OutOrStdout(), info)
	}
	entity, err := client.LookupEntity(cmd.Context(), entityType, name, nil, 0, 0)
	if err != nil {
		return err
	}
	if entity == nil {
		return fmt.Errorf("entity %s/%s not found in last 1 hour (it may exist with a specific --env/--namespace/--site scope)", entityType, name)
	}
	return io.Encode(cmd.OutOrStdout(), entity)
}

type entitiesShowOpts struct {
	IO cmdio.Options
}

func (o *entitiesShowOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &EntityTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// EntityTableCodec renders search results as a table.
type EntityTableCodec struct{}

func (c *EntityTableCodec) Format() format.Format { return "table" }

func (c *EntityTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]SearchResult)
	if !ok {
		return errors.New("invalid data type for table codec: expected []SearchResult")
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tNAME\tSCOPE\tACTIVE")
	for _, r := range results {
		t := r.Type
		if t == "" {
			t = r.EntityType
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", t, r.Name, scopeStr(r.Scope), r.Active)
	}
	return tw.Flush()
}

func (c *EntityTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// Entity types commands
// ---------------------------------------------------------------------------

//nolint:dupl
func newEntityTypesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entity-types",
		Short: "Manage Knowledge Graph entity types.",
	}

	listOpts := &entityTypesListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List entity types with counts.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := listOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			counts, err := client.CountEntityTypes(cmd.Context())
			if err != nil {
				return err
			}
			return listOpts.IO.Encode(cmd.OutOrStdout(), counts)
		},
	}
	listOpts.setup(listCmd.Flags())

	cmd.AddCommand(listCmd)
	return cmd
}

type entityTypesListOpts struct {
	IO cmdio.Options
}

func (o *entityTypesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &EntityTypeTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// EntityTypeTableCodec renders entity types as a table.
type EntityTypeTableCodec struct{}

func (c *EntityTypeTableCodec) Format() format.Format { return "table" }

func (c *EntityTypeTableCodec) Encode(w io.Writer, v any) error {
	counts, ok := v.(map[string]int64)
	if !ok {
		return errors.New("invalid data type for table codec: expected map[string]int64")
	}
	// Sort types alphabetically for stable output.
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Strings(types)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tCOUNT")
	for _, t := range types {
		fmt.Fprintf(tw, "%s\t%d\n", t, counts[t])
	}
	return tw.Flush()
}

func (c *EntityTypeTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// Scopes command
// ---------------------------------------------------------------------------

//nolint:dupl
func newScopesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scopes",
		Short: "Manage Knowledge Graph entity scopes.",
	}

	ioOpts := &scopesListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List entity scopes.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ioOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			scopes, err := client.ListEntityScopes(cmd.Context())
			if err != nil {
				return err
			}
			return ioOpts.IO.Encode(cmd.OutOrStdout(), scopes)
		},
	}
	ioOpts.setup(listCmd.Flags())

	cmd.AddCommand(listCmd)
	return cmd
}

type scopesListOpts struct {
	IO cmdio.Options
}

func (o *scopesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Assertions commands
// ---------------------------------------------------------------------------

//nolint:maintidx
func newAssertionsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assertions",
		Short: "Query Knowledge Graph assertions.",
	}

	// assertionsRunE builds a RunE that constructs an assertions request from flags,
	// calls apiFn, and outputs the result as JSON.
	assertionsRunE := func(apiFn func(*Client, context.Context, AssertionsRequest) (any, error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			req, err := buildAssertionsRequestFromFlags(cmd, args, client)
			if err != nil {
				return err
			}
			result, err := apiFn(client, cmd.Context(), req)
			if err != nil {
				return err
			}
			io := &cmdio.Options{OutputFormat: "json"}
			return io.Encode(cmd.OutOrStdout(), result)
		}
	}

	queryCmd := &cobra.Command{
		Use:   "query [Type--Name]",
		Short: "Query assertions for a time range.",
		RunE: assertionsRunE(func(c *Client, ctx context.Context, req AssertionsRequest) (any, error) {
			return c.QueryAssertions(ctx, req)
		}),
	}

	summaryCmd := &cobra.Command{
		Use:   "summary [Type--Name]",
		Short: "Get assertions summary for a time range.",
		RunE: assertionsRunE(func(c *Client, ctx context.Context, req AssertionsRequest) (any, error) {
			return c.AssertionsSummary(ctx, req)
		}),
	}

	graphCmd := &cobra.Command{
		Use:   "graph [Type--Name]",
		Short: "Query assertions with graph topology.",
		RunE: assertionsRunE(func(c *Client, ctx context.Context, req AssertionsRequest) (any, error) {
			return c.AssertionsGraph(ctx, req)
		}),
	}

	for _, sub := range []*cobra.Command{queryCmd, summaryCmd, graphCmd} {
		sub.Flags().StringP("file", "f", "", "Input file (YAML) — overrides all other flags")
		sub.Flags().String("name", "", "Entity name")
		sub.Flags().String("type", "", "Entity type")
		sub.Flags().String("env", "", "Environment scope")
		sub.Flags().String("namespace", "", "Namespace scope")
		sub.Flags().String("site", "", "Site scope")
		sub.Flags().String("since", "", "Duration ago (e.g. 1h, 30m, 7d) — default 1h")
	}

	// active subcommand
	var (
		activeScope      scopeFlags
		activeEntityType string
		activeSeverity   string
		activePage       int
	)
	activeOpts := &assertionsActiveOpts{}
	activeCmd := &cobra.Command{
		Use:   "active",
		Short: "Show entities with active assertions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := activeOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			startMs, endMs, err := activeScope.resolveTime()
			if err != nil {
				return err
			}
			entityTypes, err := resolveEntityTypes(cmd, client, activeEntityType)
			if err != nil {
				return err
			}
			results, err := searchByTypes(cmd.Context(), client, entityTypes, true, activeScope.scopeCriteria(), startMs, endMs, activePage)
			if err != nil {
				return err
			}
			if activeSeverity != "" {
				results = filterBySeverity(results, activeSeverity)
			}
			return activeOpts.IO.Encode(cmd.OutOrStdout(), results)
		},
	}
	activeScope.register(activeCmd)
	activeCmd.Flags().StringVar(&activeEntityType, "type", "", "Filter by entity type")
	activeCmd.Flags().StringVar(&activeSeverity, "severity", "", "Filter by severity (e.g. CRITICAL, WARNING)")
	activeCmd.Flags().IntVar(&activePage, "page", 0, "Page number (0-based)")
	activeOpts.setup(activeCmd.Flags())

	// entity-metric subcommand
	var entityMetricScope scopeFlags
	entityMetricCmd := &cobra.Command{
		Use:   "entity-metric [Type--Name]",
		Short: "Get metric data for a specific assertion on an entity.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			var req EntityMetricRequest
			//nolint:nestif
			if cmd.Flags().Changed("file") {
				file, _ := cmd.Flags().GetString("file")
				data, err := readFileOrStdin(cmd, file)
				if err != nil {
					return fmt.Errorf("failed to read file: %w", err)
				}
				if err := yaml.Unmarshal(data, &req); err != nil {
					return fmt.Errorf("invalid YAML: %w", err)
				}
			} else {
				entityType, name, err := resolveEntityTypeAndName(cmd, args)
				if err != nil {
					return err
				}
				assertionID, _ := cmd.Flags().GetString("assertion-id")
				if assertionID == "" {
					return errors.New("--assertion-id is required (or use --file)")
				}
				startMs, endMs, err := entityMetricScope.resolveTime()
				if err != nil {
					return err
				}
				labels := map[string]string{
					"alertname":           assertionID,
					"asserts_entity_type": entityType,
					"asserts_entity_name": name,
				}
				maps.Copy(labels, entityMetricScope.scopeMap())
				req = EntityMetricRequest{
					StartTime: startMs,
					EndTime:   endMs,
					Labels:    labels,
				}
			}
			result, err := client.AssertionEntityMetric(cmd.Context(), req)
			if err != nil {
				return err
			}
			return (&cmdio.Options{OutputFormat: "json"}).Encode(cmd.OutOrStdout(), result)
		},
	}
	entityMetricCmd.Flags().StringP("file", "f", "", "Input file (YAML)")
	entityMetricCmd.Flags().String("name", "", "Entity name")
	entityMetricCmd.Flags().String("type", "", "Entity type")
	entityMetricCmd.Flags().String("assertion-id", "", "Assertion ID")
	entityMetricScope.register(entityMetricCmd)

	// source-metrics subcommand
	var sourceMetricsSince string
	sourceMetricsCmd := &cobra.Command{
		Use:   "source-metrics",
		Short: "Get source metrics for a specific assertion.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req SourceMetricsRequest
			//nolint:nestif
			if cmd.Flags().Changed("file") {
				file, _ := cmd.Flags().GetString("file")
				data, err := readFileOrStdin(cmd, file)
				if err != nil {
					return fmt.Errorf("failed to read file: %w", err)
				}
				if err := yaml.Unmarshal(data, &req); err != nil {
					return fmt.Errorf("invalid YAML: %w", err)
				}
			} else {
				assertionID, _ := cmd.Flags().GetString("assertion-id")
				if assertionID == "" {
					return errors.New("--assertion-id is required (or use --file)")
				}
				startMs, endMs, err := resolveTimeEpochMs(sourceMetricsSince)
				if err != nil {
					return err
				}
				req = SourceMetricsRequest{
					AssertionID: assertionID,
					StartTime:   startMs,
					EndTime:     endMs,
				}
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			results, err := client.AssertionSourceMetrics(cmd.Context(), req)
			if err != nil {
				return err
			}
			return (&cmdio.Options{OutputFormat: "json"}).Encode(cmd.OutOrStdout(), results)
		},
	}
	sourceMetricsCmd.Flags().StringP("file", "f", "", "Input file (YAML)")
	sourceMetricsCmd.Flags().String("assertion-id", "", "Assertion ID")
	sourceMetricsCmd.Flags().StringVar(&sourceMetricsSince, "since", "", "Duration ago (e.g. 1h, 30m, 7d)")

	exampleCmd := &cobra.Command{
		Use:   "example",
		Short: "Print an example assertions request YAML.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printYAMLExample(cmd.OutOrStdout(), exampleAssertionsRequest())
		},
	}

	cmd.AddCommand(queryCmd, summaryCmd, graphCmd, activeCmd, entityMetricCmd, sourceMetricsCmd, exampleCmd)
	return cmd
}

type assertionsActiveOpts struct {
	IO cmdio.Options
}

func (o *assertionsActiveOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func buildAssertionsRequestFromFlags(cmd *cobra.Command, args []string, client *Client) (AssertionsRequest, error) {
	if cmd.Flags().Changed("file") {
		file, _ := cmd.Flags().GetString("file")
		data, err := readFileOrStdin(cmd, file)
		if err != nil {
			return AssertionsRequest{}, fmt.Errorf("failed to read file: %w", err)
		}
		var req AssertionsRequest
		if err := yaml.Unmarshal(data, &req); err != nil {
			return AssertionsRequest{}, fmt.Errorf("invalid YAML: %w", err)
		}
		if req.StartTime == 0 || req.EndTime == 0 {
			return AssertionsRequest{}, errors.New("startTime and endTime are required")
		}
		return req, nil
	}

	entityType, name, err := resolveEntityTypeAndName(cmd, args)
	if err != nil {
		return AssertionsRequest{}, err
	}
	env, _ := cmd.Flags().GetString("env")
	namespace, _ := cmd.Flags().GetString("namespace")
	site, _ := cmd.Flags().GetString("site")
	since, _ := cmd.Flags().GetString("since")
	startMs, endMs, err := resolveTimeEpochMs(since)
	if err != nil {
		return AssertionsRequest{}, err
	}

	scope := map[string]any{}
	if env != "" {
		scope["env"] = env
	}
	if namespace != "" {
		scope["namespace"] = namespace
	}
	if site != "" {
		scope["site"] = site
	}
	if len(scope) == 0 {
		// Auto-resolve scope via LookupEntity.
		entity, lookupErr := client.LookupEntity(cmd.Context(), entityType, name, nil, startMs, endMs)
		if lookupErr != nil {
			return AssertionsRequest{}, lookupErr
		}
		if entity != nil {
			scope = toAnyMap(entity.Scope)
		}
	}

	return AssertionsRequest{
		StartTime:  startMs,
		EndTime:    endMs,
		EntityKeys: []EntityKey{{Type: entityType, Name: name, Scope: scope}},
	}, nil
}

func filterBySeverity(results []SearchResult, sev string) []SearchResult {
	want := strings.ToUpper(sev)
	var out []SearchResult
	for _, r := range results {
		if s, ok := r.Assertion["severity"].(string); ok && strings.ToUpper(s) == want {
			out = append(out, r)
		}
	}
	if out == nil {
		return []SearchResult{}
	}
	return out
}

// ---------------------------------------------------------------------------
// Search commands
// ---------------------------------------------------------------------------

func newSearchCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search Knowledge Graph entities or assertions.",
	}

	// search assertions
	var searchAssertionsFile string
	var searchAssertionsScope scopeFlags
	searchAssertionsCmd := &cobra.Command{
		Use:   "assertions",
		Short: "Search for assertions matching a query.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			var req SearchRequest
			//nolint:nestif
			if searchAssertionsFile != "" {
				data, err := readFileOrStdin(cmd, searchAssertionsFile)
				if err != nil {
					return fmt.Errorf("failed to read file: %w", err)
				}
				if err := yaml.Unmarshal(data, &req); err != nil {
					return fmt.Errorf("invalid YAML: %w", err)
				}
			} else {
				entityType, _ := cmd.Flags().GetString("type")
				entityName, _ := cmd.Flags().GetString("name")
				startMs, endMs, err := searchAssertionsScope.resolveTime()
				if err != nil {
					return err
				}
				var filterCriteria []EntityMatcher
				if entityType != "" || entityName != "" {
					if entityType == "" {
						entityType = "Service"
					}
					matcher := EntityMatcher{EntityType: entityType}
					if entityName != "" {
						matcher.PropertyMatchers = []PropertyMatcher{{Name: "name", Op: "EQUALS", Value: entityName}}
					}
					filterCriteria = []EntityMatcher{matcher}
				}
				req = SearchRequest{
					TimeCriteria:   &TimeCriteria{Start: startMs, End: endMs},
					FilterCriteria: filterCriteria,
					ScopeCriteria:  searchAssertionsScope.scopeCriteria(),
				}
			}
			if req.DefinitionId == nil {
				one := 1
				req.DefinitionId = &one
			}
			result, err := client.SearchAssertions(cmd.Context(), req)
			if err != nil {
				return err
			}
			return (&cmdio.Options{OutputFormat: "json"}).Encode(cmd.OutOrStdout(), result)
		},
	}
	searchAssertionsCmd.Flags().StringVarP(&searchAssertionsFile, "file", "f", "", "Input file (YAML)")
	searchAssertionsCmd.Flags().String("type", "", "Entity type filter")
	searchAssertionsCmd.Flags().String("name", "", "Entity name filter")
	searchAssertionsScope.register(searchAssertionsCmd)

	// search sample
	var searchSampleType string
	searchSampleCmd := &cobra.Command{
		Use:   "sample",
		Short: "Return a sample of entities by type.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			now := time.Now().UnixMilli()
			req := SampleSearchRequest{
				TimeCriteria: &TimeCriteria{Start: now - 3600000, End: now},
				FilterCriteria: []EntityMatcher{{
					EntityType:       searchSampleType,
					PropertyMatchers: []PropertyMatcher{{Name: "name", Op: "IS NOT NULL", Type: "String"}},
				}},
				SampleSize: 300,
			}
			results, err := client.SearchSample(cmd.Context(), req)
			if err != nil {
				return err
			}
			return (&cmdio.Options{OutputFormat: "json"}).Encode(cmd.OutOrStdout(), results)
		},
	}
	searchSampleCmd.Flags().StringVar(&searchSampleType, "type", "", "Entity type")
	_ = searchSampleCmd.MarkFlagRequired("type")

	// search entities
	var (
		searchEntitiesType  string
		searchEntitiesScope scopeFlags
		searchEntitiesPage  int
	)
	searchEntitiesOpts := &searchEntitiesListOpts{}
	searchEntitiesCmd := &cobra.Command{
		Use:   "entities",
		Short: "Search for entities by type.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := searchEntitiesOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			entityTypes, err := resolveEntityTypes(cmd, client, searchEntitiesType)
			if err != nil {
				return err
			}
			results, err := searchByTypes(cmd.Context(), client, entityTypes, false, searchEntitiesScope.scopeCriteria(), 0, 0, searchEntitiesPage)
			if err != nil {
				return err
			}
			return searchEntitiesOpts.IO.Encode(cmd.OutOrStdout(), results)
		},
	}
	searchEntitiesCmd.Flags().StringVar(&searchEntitiesType, "type", "", "Entity type (omit to search all)")
	searchEntitiesCmd.Flags().IntVar(&searchEntitiesPage, "page", 0, "Page number (0-based)")
	searchEntitiesScope.register(searchEntitiesCmd)
	searchEntitiesOpts.setup(searchEntitiesCmd.Flags())

	searchExampleCmd := &cobra.Command{
		Use:   "example",
		Short: "Print an example search request YAML.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printYAMLExample(cmd.OutOrStdout(), exampleSearchRequest())
		},
	}

	cmd.AddCommand(searchAssertionsCmd, searchSampleCmd, searchEntitiesCmd, searchExampleCmd)
	return cmd
}

type searchEntitiesListOpts struct {
	IO cmdio.Options
}

func (o *searchEntitiesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Graph config command
// ---------------------------------------------------------------------------

func newGraphConfigCommand(loader RESTConfigLoader) *cobra.Command {
	ioOpts := &graphConfigOpts{}
	cmd := &cobra.Command{
		Use:   "graph-config",
		Short: "Get the graph display configuration.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ioOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			result, err := client.GetGraphDisplayConfig(cmd.Context())
			if err != nil {
				return err
			}
			return ioOpts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	ioOpts.setup(cmd.Flags())
	return cmd
}

type graphConfigOpts struct {
	IO cmdio.Options
}

func (o *graphConfigOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Inspect command
// ---------------------------------------------------------------------------

func newInspectCommand(loader RESTConfigLoader) *cobra.Command {
	var inspectScope scopeFlags
	ioOpts := &inspectOpts{}
	cmd := &cobra.Command{
		Use:   "inspect [Type--Name]",
		Short: "Inspect an entity: info, assertions, and summary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ioOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			startMs, endMs, err := inspectScope.resolveTime()
			if err != nil {
				return err
			}
			entityType, name, err := resolveEntityTypeAndName(cmd, args)
			if err != nil {
				return err
			}
			scope := inspectScope.scopeMap()
			if scope == nil {
				lookup, err := client.LookupEntity(cmd.Context(), entityType, name, nil, startMs, endMs)
				if err != nil {
					return err
				}
				if lookup != nil {
					scope = lookup.Scope
				}
			}

			entityInfo, err := client.GetEntityInfo(cmd.Context(), entityType, name, scope, startMs, endMs)
			if err != nil {
				return err
			}

			scopeAny := toAnyMap(scope)
			req := AssertionsRequest{
				StartTime:  startMs,
				EndTime:    endMs,
				EntityKeys: []EntityKey{{Type: entityType, Name: name, Scope: scopeAny}},
			}
			assertions, err := client.QueryAssertions(cmd.Context(), req)
			if err != nil {
				return err
			}
			summary, err := client.AssertionsSummary(cmd.Context(), req)
			if err != nil {
				return err
			}

			result := map[string]any{
				"entity":     entityInfo,
				"assertions": assertions,
				"summary":    summary,
			}
			return ioOpts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().String("type", "", "Entity type")
	cmd.Flags().String("name", "", "Entity name")
	inspectScope.register(cmd)
	ioOpts.setup(cmd.Flags())
	return cmd
}

type inspectOpts struct {
	IO cmdio.Options
}

func (o *inspectOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Health command
// ---------------------------------------------------------------------------

func newHealthCommand(loader RESTConfigLoader) *cobra.Command {
	var (
		healthScope      scopeFlags
		healthEntityType string
	)
	ioOpts := &healthOpts{}
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show a health summary with active assertion counts.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ioOpts.IO.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			startMs, endMs, err := healthScope.resolveTime()
			if err != nil {
				return err
			}

			counts, err := client.CountEntityTypes(cmd.Context())
			if err != nil {
				return err
			}
			var entityTypes []string
			var totalEntities int
			for t, cnt := range counts {
				totalEntities += int(cnt)
				if cnt > 0 {
					entityTypes = append(entityTypes, t)
				}
			}
			if healthEntityType != "" {
				entityTypes = []string{healthEntityType}
			}
			results, err := searchByTypes(cmd.Context(), client, entityTypes, true, healthScope.scopeCriteria(), startMs, endMs, 0)
			if err != nil {
				return err
			}
			sevCounts := map[string]int{}
			for _, r := range results {
				if s, ok := r.Assertion["severity"].(string); ok {
					sevCounts[strings.ToUpper(s)]++
				} else {
					sevCounts["UNKNOWN"]++
				}
			}
			return ioOpts.IO.Encode(cmd.OutOrStdout(), map[string]any{
				"severityCounts":  sevCounts,
				"totalEntities":   totalEntities,
				"totalAssertions": len(results),
			})
		},
	}
	healthScope.register(cmd)
	cmd.Flags().StringVar(&healthEntityType, "type", "", "Limit to a specific entity type")
	ioOpts.setup(cmd.Flags())
	return cmd
}

type healthOpts struct {
	IO cmdio.Options
}

func (o *healthOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

// ---------------------------------------------------------------------------
// Open command
// ---------------------------------------------------------------------------

func newOpenCommand(loader RESTConfigLoader) *cobra.Command {
	return &cobra.Command{
		Use:   "open",
		Short: "Open the Knowledge Graph app in the browser.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			host := strings.TrimRight(cfg.Host, "/")
			url := host + "/a/grafana-asserts-app"
			cmdio.Info(cmd.OutOrStdout(), "Opening %s", url)
			if err := exec.CommandContext(cmd.Context(), "open", url).Start(); err != nil {
				return fmt.Errorf("failed to open browser: %w", err)
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Example helpers
// ---------------------------------------------------------------------------

func printYAMLExample(w io.Writer, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal example: %w", err)
	}
	_, err = w.Write(b)
	return err
}

func exampleAssertionsRequest() AssertionsRequest {
	return AssertionsRequest{
		StartTime: 1700000000,
		EndTime:   1700003600,
		EntityKeys: []EntityKey{
			{
				Type:  "Service",
				Name:  "checkout",
				Scope: map[string]any{"env": "production", "namespace": "default"},
			},
		},
		IncludeConnectedAssertions: true,
		AlertCategories:            []string{"Saturation", "Anomaly"},
		Severities:                 []string{"critical", "warning"},
	}
}

func exampleSearchRequest() SearchRequest {
	return SearchRequest{
		FilterCriteria: []EntityMatcher{
			{
				EntityType:      "Service",
				HavingAssertion: true,
			},
		},
		TimeCriteria: &TimeCriteria{
			Start: 1700000000,
			End:   1700003600,
		},
		PageNum: 0,
	}
}

// ---------------------------------------------------------------------------
// Unused import guard for pflag
// ---------------------------------------------------------------------------

var _ = (*pflag.FlagSet)(nil)
