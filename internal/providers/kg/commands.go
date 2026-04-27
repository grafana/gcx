package kg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/shared"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scopeFlags holds the --env/--namespace/--site/--from/--to/--since flags.
type scopeFlags struct {
	env       string
	namespace string
	site      string
	from      string
	to        string
	since     string
}

func (f *scopeFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.env, "env", "", "Environment scope")
	cmd.Flags().StringVar(&f.namespace, "namespace", "", "Namespace scope")
	cmd.Flags().StringVar(&f.site, "site", "", "Site scope")
	cmd.Flags().StringVar(&f.from, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	cmd.Flags().StringVar(&f.to, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	cmd.Flags().StringVar(&f.since, "since", "", "Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)")
}

func (f *scopeFlags) resolveTime() (int64, int64, error) {
	if f.since != "" && (f.from != "" || f.to != "") {
		return 0, 0, errors.New("--since is mutually exclusive with --from/--to")
	}
	if f.from != "" || f.to != "" {
		if f.from == "" {
			return 0, 0, errors.New("--from is required when --to is set")
		}
		if f.to == "" {
			return 0, 0, errors.New("--to is required when --from is set")
		}
		now := time.Now()
		start, err := shared.ParseTime(f.from, now)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid --from: %w", err)
		}
		end, err := shared.ParseTime(f.to, now)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid --to: %w", err)
		}
		return start.UnixMilli(), end.UnixMilli(), nil
	}
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

// validateScopes checks that any set scope values exist in the KG scope registry.
// If a value is not an exact match it fetches known values, finds candidates by
// substring match, and returns an error with actionable hints so the caller
// (human or LLM) can retry with the correct value. Validation is best-effort:
// if the scopes API is unavailable the error is silently ignored.
func (f *scopeFlags) validateScopes(ctx context.Context, client *Client) error {
	type check struct{ flag, dim, value string }
	checks := []check{
		{"--env", "env", f.env},
		{"--site", "site", f.site},
		{"--namespace", "namespace", f.namespace},
	}
	var active []check
	for _, c := range checks {
		if c.value != "" {
			active = append(active, c)
		}
	}
	if len(active) == 0 {
		return nil
	}
	scopes, err := client.ListEntityScopes(ctx)
	if err != nil {
		return nil //nolint:nilerr // best-effort: scope validation is advisory
	}
	var errs []string
	for _, c := range active {
		known := scopes[c.dim]
		if len(known) == 0 {
			continue
		}
		if slices.Contains(known, c.value) {
			continue
		}
		lower := strings.ToLower(c.value)
		var candidates []string
		for _, v := range known {
			if strings.Contains(strings.ToLower(v), lower) {
				candidates = append(candidates, v)
			}
		}
		sort.Strings(candidates)
		var msg string
		if len(candidates) > 0 {
			msg = fmt.Sprintf("unknown %s value %q — did you mean one of: %s", c.flag, c.value, strings.Join(candidates, ", "))
		} else {
			all := append([]string(nil), known...)
			sort.Strings(all)
			shown := all
			suffix := ""
			if len(shown) > 10 {
				shown = shown[:10]
				suffix = fmt.Sprintf(" (and %d more — run gcx kg scopes list)", len(all)-10)
			}
			msg = fmt.Sprintf("unknown %s value %q — known %s values: %s%s", c.flag, c.value, c.dim, strings.Join(shown, ", "), suffix)
		}
		errs = append(errs, msg)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
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

// resolveTimeFromFlags reads --from/--to/--since from a command and resolves them to epoch ms.
func resolveTimeFromFlags(cmd *cobra.Command) (int64, int64, error) {
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")
	since, _ := cmd.Flags().GetString("since")
	sf := scopeFlags{from: from, to: to, since: since}
	return sf.resolveTime()
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
// Server-side (5xx) failures for individual entity types are logged as warnings and skipped
// so that a broken type does not abort results for all other types.
func searchByTypes(ctx context.Context, cmd *cobra.Command, client *Client, entityTypes []string, assertionsOnly bool, sc *ScopeCriteria, startMs, endMs int64, pageNum int) ([]SearchResult, error) {
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
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.IsServerError() {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping entity type %q — server error: %v\n", et, apiErr)
				continue
			}
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
// Stack status
// ---------------------------------------------------------------------------

func newStatusCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &statusOpts{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Knowledge Graph stack status.",
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
			return opts.IO.Encode(cmd.OutOrStdout(), status)
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
			typedObjs, err := crud.List(ctx, rulesListOpts.Limit)
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
	IO    cmdio.Options
	Limit int64
}

func (o *rulesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &RuleTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all)")
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
	t := style.NewTable("NAME", "RECORD", "ALERT", "EXPR")
	for _, r := range rules {
		expr := r.Expr
		if len(expr) > 60 {
			expr = expr[:57] + "..."
		}
		t.Row(r.Name, r.Record, r.Alert, expr)
	}
	return t.Render(w)
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

			if err := showScope.validateScopes(cmd.Context(), client); err != nil {
				return err
			}
			startMs, endMs, err := showScope.resolveTime()
			if err != nil {
				return err
			}

			// Single entity mode: name provided
			if len(args) == 1 {
				return showSingleEntity(cmd, client, showType, args[0], &showScope, startMs, endMs, &ioOpts.IO)
			}

			// List mode: no name
			entityTypes, err := resolveEntityTypes(cmd, client, showType)
			if err != nil {
				return err
			}
			results, err := searchByTypes(cmd.Context(), cmd, client, entityTypes, assertionsOnly, showScope.scopeCriteria(), startMs, endMs, page)
			if err != nil {
				return err
			}
			results = adapter.TruncateSlice(results, ioOpts.Limit)
			return ioOpts.IO.Encode(cmd.OutOrStdout(), results)
		},
	}
	showCmd.Flags().StringVar(&showType, "type", "", "Entity type (required for single entity, optional for list)")
	showCmd.Flags().BoolVar(&assertionsOnly, "insights-only", false, "Only return entities with active insights (list mode)")
	showCmd.Flags().IntVar(&page, "page", 0, "Page number, 0-based (list mode)")
	showScope.register(showCmd)
	ioOpts.setup(showCmd.Flags())

	// list subcommand
	var (
		listType       string
		listAssertOnly bool
		listScope      scopeFlags
		listPage       int
	)
	listOpts := &entitiesShowOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List entities by type (omit --type to list all types).",
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
			if err := listScope.validateScopes(cmd.Context(), client); err != nil {
				return err
			}
			startMs, endMs, err := listScope.resolveTime()
			if err != nil {
				return err
			}
			entityTypes, err := resolveEntityTypes(cmd, client, listType)
			if err != nil {
				return err
			}
			results, err := searchByTypes(cmd.Context(), cmd, client, entityTypes, listAssertOnly, listScope.scopeCriteria(), startMs, endMs, listPage)
			if err != nil {
				return err
			}
			results = adapter.TruncateSlice(results, listOpts.Limit)
			return listOpts.IO.Encode(cmd.OutOrStdout(), results)
		},
	}
	listCmd.Flags().StringVar(&listType, "type", "", "Entity type (omit to list all)")
	listCmd.Flags().BoolVar(&listAssertOnly, "assertions-only", false, "Only return entities with active assertions")
	listCmd.Flags().IntVar(&listPage, "page", 0, "Page number (0-based)")
	listScope.register(listCmd)
	listOpts.setup(listCmd.Flags())

	cmd.AddCommand(showCmd, listCmd)
	return cmd
}

func showSingleEntity(cmd *cobra.Command, client *Client, entityType, name string, scope *scopeFlags, startMs, endMs int64, io *cmdio.Options) error {
	if entityType == "" {
		return errors.New("--type is required when showing a single entity")
	}
	if sm := scope.scopeMap(); sm != nil {
		info, err := client.GetEntityInfo(cmd.Context(), entityType, name, sm, startMs, endMs)
		if err != nil {
			return err
		}
		return io.Encode(cmd.OutOrStdout(), info)
	}
	entity, err := client.LookupEntity(cmd.Context(), entityType, name, nil, startMs, endMs)
	if err != nil {
		return err
	}
	if entity == nil {
		return fmt.Errorf("entity %s/%s not found in the specified time window (it may exist with a specific --env/--namespace/--site scope)", entityType, name)
	}
	return io.Encode(cmd.OutOrStdout(), entity)
}

type entitiesShowOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *entitiesShowOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &EntityTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all)")
}

// EntityTableCodec renders search results as a table.
type EntityTableCodec struct{}

func (c *EntityTableCodec) Format() format.Format { return "table" }

func (c *EntityTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]SearchResult)
	if !ok {
		return errors.New("invalid data type for table codec: expected []SearchResult")
	}
	t := style.NewTable("TYPE", "NAME", "SCOPE", "ACTIVE")
	for _, r := range results {
		typ := r.Type
		if typ == "" {
			typ = r.EntityType
		}
		t.Row(typ, r.Name, scopeStr(r.Scope), strconv.FormatBool(r.Active))
	}
	return t.Render(w)
}

func (c *EntityTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// Scopes command
// ---------------------------------------------------------------------------

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
	IO    cmdio.Options
	Limit int64
}

func (o *scopesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all)")
}

// ---------------------------------------------------------------------------
// Insights commands
// ---------------------------------------------------------------------------

//nolint:maintidx,gocyclo
func newAssertionsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Query Knowledge Graph insights.",
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
		Short: "Query insights for a time range.",
		RunE: assertionsRunE(func(c *Client, ctx context.Context, req AssertionsRequest) (any, error) {
			return c.QueryAssertions(ctx, req)
		}),
	}

	summaryCmd := &cobra.Command{
		Use:   "summary [Type--Name]",
		Short: "Get insights summary for a time range.",
		RunE: assertionsRunE(func(c *Client, ctx context.Context, req AssertionsRequest) (any, error) {
			return c.AssertionsSummary(ctx, req)
		}),
	}

	graphCmd := &cobra.Command{
		Use:   "graph [Type--Name]",
		Short: "Query insights with graph topology.",
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
		sub.Flags().String("from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
		sub.Flags().String("to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
		sub.Flags().String("since", "", "Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)")
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
		Short: "Show entities with active insights.",
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
			if err := activeScope.validateScopes(cmd.Context(), client); err != nil {
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
			results, err := searchByTypes(cmd.Context(), cmd, client, entityTypes, true, activeScope.scopeCriteria(), startMs, endMs, activePage)
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
		Short: "Get metric data for a specific insight on an entity.",
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
				assertionID, _ := cmd.Flags().GetString("insight-id")
				if assertionID == "" {
					return errors.New("--insight-id is required (or use --file)")
				}
				if err := entityMetricScope.validateScopes(cmd.Context(), client); err != nil {
					return err
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
	entityMetricCmd.Flags().String("insight-id", "", "Insight ID")
	entityMetricScope.register(entityMetricCmd)

	// source-metrics subcommand
	sourceMetricsCmd := &cobra.Command{
		Use:   "source-metrics",
		Short: "Get source metrics for a specific insight.",
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
				assertionID, _ := cmd.Flags().GetString("insight-id")
				if assertionID == "" {
					return errors.New("--insight-id is required (or use --file)")
				}
				startMs, endMs, err := resolveTimeFromFlags(cmd)
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
	sourceMetricsCmd.Flags().String("insight-id", "", "Insight ID")
	sourceMetricsCmd.Flags().String("from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	sourceMetricsCmd.Flags().String("to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	sourceMetricsCmd.Flags().String("since", "", "Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)")

	exampleCmd := &cobra.Command{
		Use:   "example",
		Short: "Print an example insights request YAML.",
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
	sf := scopeFlags{env: env, site: site, namespace: namespace}
	if err := sf.validateScopes(cmd.Context(), client); err != nil {
		return AssertionsRequest{}, err
	}
	startMs, endMs, err := resolveTimeFromFlags(cmd)
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
		Short: "Search Knowledge Graph entities or insights.",
	}

	// search insights
	var searchAssertionsFile string
	var searchAssertionsScope scopeFlags
	searchAssertionsCmd := &cobra.Command{
		Use:   "insights",
		Short: "Search for insights matching a query.",
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
				if err := searchAssertionsScope.validateScopes(cmd.Context(), client); err != nil {
					return err
				}
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
	var (
		searchSampleType  string
		searchSampleScope scopeFlags
	)
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
			if err := searchSampleScope.validateScopes(cmd.Context(), client); err != nil {
				return err
			}
			startMs, endMs, err := searchSampleScope.resolveTime()
			if err != nil {
				return err
			}
			req := SampleSearchRequest{
				TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
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
	searchSampleScope.register(searchSampleCmd)

	searchExampleCmd := &cobra.Command{
		Use:   "example",
		Short: "Print an example search request YAML.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printYAMLExample(cmd.OutOrStdout(), exampleSearchRequest())
		},
	}

	cmd.AddCommand(searchAssertionsCmd, searchSampleCmd, searchExampleCmd)
	return cmd
}

// ---------------------------------------------------------------------------
// Inspect command
// ---------------------------------------------------------------------------

func newInspectCommand(loader RESTConfigLoader) *cobra.Command {
	var inspectScope scopeFlags
	ioOpts := &inspectOpts{}
	cmd := &cobra.Command{
		Use:   "inspect [Type--Name]",
		Short: "Inspect an entity: info, insights, and summary.",
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
			if err := inspectScope.validateScopes(cmd.Context(), client); err != nil {
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
				"entity":   entityInfo,
				"insights": assertions,
				"summary":  summary,
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
		Short: "Show a health summary with active insight counts.",
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
			if err := healthScope.validateScopes(cmd.Context(), client); err != nil {
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
			results, err := searchByTypes(cmd.Context(), cmd, client, entityTypes, true, healthScope.scopeCriteria(), startMs, endMs, 0)
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
				"severityCounts": sevCounts,
				"totalEntities":  totalEntities,
				"totalInsights":  len(results),
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
			url := strings.TrimRight(cfg.GrafanaURL, "/") + "/a/grafana-asserts-app"
			cmdio.Info(cmd.ErrOrStderr(), "Opening %s", url)
			return deeplink.Open(url)
		},
	}
}

// ---------------------------------------------------------------------------
// Metadata command
// ---------------------------------------------------------------------------

// processGraphSchema converts a raw GraphSchemaResponse into a KGSchemaResult.
func processGraphSchema(resp GraphSchemaResponse) KGSchemaResult {
	ignoredTypes := map[string]bool{"Account": true, "Env": true}
	ignoredProps := map[string]bool{"Discovered": true, "Updated": true, "labelsForName": true}

	idToName := make(map[int64]string)
	typeProps := make(map[string]map[string]bool)

	for _, e := range resp.Data.Entities {
		name := e.Name
		if name == "" {
			name = "Unknown"
		}
		if ignoredTypes[name] {
			continue
		}
		if e.ID != nil {
			idToName[*e.ID] = name
		}
		if _, ok := typeProps[name]; !ok {
			typeProps[name] = map[string]bool{"name": true}
		}
		for prop := range e.Properties {
			if ignoredProps[prop] || strings.HasPrefix(prop, "_") ||
				strings.HasPrefix(prop, "scope_") || strings.HasPrefix(prop, "lookup_") {
				continue
			}
			typeProps[name][prop] = true
		}
	}

	types := make([]string, 0, len(typeProps))
	for t := range typeProps {
		types = append(types, t)
	}
	sort.Strings(types)

	entityTypes := make([]EntityTypeSchema, 0, len(types))
	for _, t := range types {
		props := make([]string, 0, len(typeProps[t]))
		for p := range typeProps[t] {
			props = append(props, p)
		}
		sort.Strings(props)
		entityTypes = append(entityTypes, EntityTypeSchema{Type: t, Properties: props})
	}

	relSet := make(map[string]bool)
	for _, edge := range resp.Data.Edges {
		rel := strings.TrimSpace(edge.Type)
		if rel == "" {
			continue
		}
		src := idToName[edge.Source]
		if src == "" {
			src = fmt.Sprintf("id:%d", edge.Source)
		}
		dst := idToName[edge.Destination]
		if dst == "" {
			dst = fmt.Sprintf("id:%d", edge.Destination)
		}
		relSet[fmt.Sprintf("%s --%s--> %s", src, rel, dst)] = true
	}
	rels := make([]string, 0, len(relSet))
	for r := range relSet {
		rels = append(rels, r)
	}
	sort.Strings(rels)

	return KGSchemaResult{EntityTypes: entityTypes, Relationships: rels}
}

func formatMatchCriteria(matchers []TelemetryConfigMatcher) string {
	if len(matchers) == 0 {
		return "any entity"
	}
	parts := make([]string, 0, len(matchers))
	for _, m := range matchers {
		if len(m.Values) > 0 {
			parts = append(parts, fmt.Sprintf("%s %s [%s]", m.Property, m.Op, strings.Join(m.Values, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s", m.Property, m.Op))
		}
	}
	return strings.Join(parts, " AND ")
}

func formatLabelMapping(mapping map[string]string) string {
	if len(mapping) == 0 {
		return "(none)"
	}
	pairs := make([]string, 0, len(mapping))
	for entityProp, label := range mapping {
		pairs = append(pairs, entityProp+" → "+label)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ", ")
}

func formatLogSection(cfgs []LogDrilldownConfig) string {
	if len(cfgs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(cfgs))
	for _, cfg := range cfgs {
		l := fmt.Sprintf("  - %q (priority: %d, datasource: %s, default: %t)", cfg.Name, cfg.Priority, cfg.DataSourceUID, cfg.DefaultConfig)
		l += "\n    match: " + formatMatchCriteria(cfg.Match)
		l += "\n    entityProperty→logLabel: " + formatLabelMapping(cfg.EntityPropertyToLogLabelMapping)
		if cfg.ErrorLabel != "" {
			l += "\n    errorLabel: " + cfg.ErrorLabel
		}
		if cfg.FilterByTraceID {
			l += "\n    filterByTraceId: true"
		}
		if cfg.FilterBySpanID {
			l += "\n    filterBySpanId: true"
		}
		lines = append(lines, l)
	}
	return "Log configs:\n" + strings.Join(lines, "\n")
}

func formatTraceSection(cfgs []TraceDrilldownConfig) string {
	if len(cfgs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(cfgs))
	for _, cfg := range cfgs {
		l := fmt.Sprintf("  - %q (priority: %d, datasource: %s, default: %t)", cfg.Name, cfg.Priority, cfg.DataSourceUID, cfg.DefaultConfig)
		l += "\n    match: " + formatMatchCriteria(cfg.Match)
		l += "\n    entityProperty→traceLabel: " + formatLabelMapping(cfg.EntityPropertyToTraceLabelMapping)
		lines = append(lines, l)
	}
	return "Trace configs:\n" + strings.Join(lines, "\n")
}

func formatProfileSection(cfgs []ProfileDrilldownConfig) string {
	if len(cfgs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(cfgs))
	for _, cfg := range cfgs {
		l := fmt.Sprintf("  - %q (priority: %d, datasource: %s, default: %t)", cfg.Name, cfg.Priority, cfg.DataSourceUID, cfg.DefaultConfig)
		l += "\n    match: " + formatMatchCriteria(cfg.Match)
		l += "\n    entityProperty→profileLabel: " + formatLabelMapping(cfg.EntityPropertyToProfileLabelMapping)
		lines = append(lines, l)
	}
	return "Profile configs:\n" + strings.Join(lines, "\n")
}

type describeOpts struct {
	IO cmdio.Options
}

func (o *describeOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &DescribeTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

// DescribeTextCodec renders KGMetadataOutput in the compact LLM-friendly text format
// used by the lodestone load_knowledge_graph_metadata tool.
type DescribeTextCodec struct{}

func (c *DescribeTextCodec) Format() format.Format { return "text" }

func (c *DescribeTextCodec) Encode(w io.Writer, v any) error {
	out, ok := v.(KGMetadataOutput)
	if !ok {
		return errors.New("invalid data type for text codec: expected KGMetadataOutput")
	}

	var sections []string

	if out.Schema != nil {
		var lines []string
		lines = append(lines, "Entity types and properties:")
		for _, et := range out.Schema.EntityTypes {
			lines = append(lines, fmt.Sprintf("  %s: %s", et.Type, strings.Join(et.Properties, ", ")))
		}
		if len(out.Schema.Relationships) > 0 {
			lines = append(lines, "Relationships: "+strings.Join(out.Schema.Relationships, "; "))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	if len(out.Scopes) > 0 {
		var parts []string
		for _, dim := range []string{"env", "site", "namespace"} {
			if vals := out.Scopes[dim]; len(vals) > 0 {
				parts = append(parts, dim+": "+strings.Join(vals, ", "))
			}
		}
		if len(parts) > 0 {
			sections = append(sections, "Scope values (env, site, namespace):\n  "+strings.Join(parts, "\n  "))
		}
	}

	hasTelemetry := len(out.Logs) > 0 || len(out.Traces) > 0 || len(out.Profiles) > 0
	if hasTelemetry {
		const telHeader = "Telemetry configs map entity properties to datasource labels for querying telemetry.\n" +
			"To query telemetry for an entity: find the matching config (by match criteria and priority), " +
			"then use entityProperty→label mappings to build filters from the entity's properties."
		var telSections []string
		if s := formatLogSection(out.Logs); s != "" {
			telSections = append(telSections, s)
		}
		if s := formatTraceSection(out.Traces); s != "" {
			telSections = append(telSections, s)
		}
		if s := formatProfileSection(out.Profiles); s != "" {
			telSections = append(telSections, s)
		}
		sections = append(sections, telHeader+"\n\n"+strings.Join(telSections, "\n\n"))
	}

	if len(sections) == 0 {
		fmt.Fprintln(w, "No metadata requested.")
		return nil
	}
	_, err := fmt.Fprint(w, strings.Join(sections, "\n\n"))
	return err
}

func (c *DescribeTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

func newDescribeCommand(loader RESTConfigLoader) *cobra.Command {
	var (
		flagSchema   bool
		flagScopes   bool
		flagLogs     bool
		flagTraces   bool
		flagProfiles bool
		flagAll      bool
		sf           scopeFlags
	)
	ioOpts := &describeOpts{}
	cmd := &cobra.Command{
		Use:   "describe",
		Short: "Describe the Knowledge Graph: entity types, valid env/namespace/site values, and telemetry query configs.",
		Long: `Describe the Knowledge Graph structure needed to formulate correct KG and telemetry queries.

Specify one or more section flags to load specific data:
  --schema    Entity types, properties, and relationships
  --scopes    Available env/site/namespace values
  --logs      Log drilldown configs (entity property → Loki label mappings)
  --traces    Trace drilldown configs (entity property → Tempo label mappings)
  --profiles  Profile drilldown configs (entity property → Pyroscope label mappings)
  --all       Load all sections`,
		Example: `  # Entity types and scopes — useful before a gcx kg search or inspect call
  gcx kg describe --schema --scopes

  # Log configs only — use before building a Loki query from entity properties
  gcx kg describe --logs

  # All telemetry configs as JSON
  gcx kg describe --logs --traces --profiles -o json

  # Load everything
  gcx kg describe --all`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ioOpts.IO.Validate(); err != nil {
				return err
			}

			// Require at least one section flag.
			none := !cmd.Flags().Changed("schema") && !cmd.Flags().Changed("scopes") &&
				!cmd.Flags().Changed("logs") && !cmd.Flags().Changed("traces") &&
				!cmd.Flags().Changed("profiles") && !cmd.Flags().Changed("all")
			if none {
				return errors.New("no section specified\n\nUse a section flag to load specific data:\n" +
					"  --schema    entity types, properties, and relationships\n" +
					"  --scopes    available env/site/namespace values\n" +
					"  --logs      log drilldown configs (entity property → Loki label mappings)\n" +
					"  --traces    trace drilldown configs (entity property → Tempo label mappings)\n" +
					"  --profiles  profile drilldown configs (entity property → Pyroscope label mappings)\n" +
					"  --all       load all sections")
			}

			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}

			loadSchema := flagSchema || flagAll
			loadScopes := flagScopes || flagAll
			loadLogs := flagLogs || flagAll
			loadTraces := flagTraces || flagAll
			loadProfiles := flagProfiles || flagAll

			startMs, endMs, err := sf.resolveTime()
			if err != nil {
				return err
			}

			var out KGMetadataOutput

			if loadSchema {
				schemaResp, schemaErr := client.FetchGraphSchema(cmd.Context(), startMs, endMs)
				if schemaErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: schema failed to load: %v\n", schemaErr)
				} else {
					result := processGraphSchema(schemaResp)
					out.Schema = &result
				}
			}

			if loadScopes {
				scopes, scopeErr := client.ListEntityScopes(cmd.Context())
				if scopeErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: scope values failed to load: %v\n", scopeErr)
				} else {
					out.Scopes = scopes
				}
			}

			if loadLogs {
				logResp, logErr := client.FetchLogConfigs(cmd.Context())
				if logErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: log configs failed to load: %v\n", logErr)
				} else {
					out.Logs = logResp.LogDrilldownConfigs
				}
			}

			if loadTraces {
				traceResp, traceErr := client.FetchTraceConfigs(cmd.Context())
				if traceErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: trace configs failed to load: %v\n", traceErr)
				} else {
					out.Traces = traceResp.TraceDrilldownConfigs
				}
			}

			if loadProfiles {
				profileResp, profileErr := client.FetchProfileConfigs(cmd.Context())
				if profileErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: profile configs failed to load: %v\n", profileErr)
				} else {
					out.Profiles = profileResp.ProfileDrilldownConfigs
				}
			}

			return ioOpts.IO.Encode(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().BoolVar(&flagSchema, "schema", false, "Load entity types, properties, and relationships")
	cmd.Flags().BoolVar(&flagScopes, "scopes", false, "Load available env/site/namespace values")
	cmd.Flags().BoolVar(&flagLogs, "logs", false, "Load log drilldown configs (entity property → Loki label mappings)")
	cmd.Flags().BoolVar(&flagTraces, "traces", false, "Load trace drilldown configs (entity property → Tempo label mappings)")
	cmd.Flags().BoolVar(&flagProfiles, "profiles", false, "Load profile drilldown configs (entity property → Pyroscope label mappings)")
	cmd.Flags().BoolVar(&flagAll, "all", false, "Load all sections")
	cmd.Flags().StringVar(&sf.from, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	cmd.Flags().StringVar(&sf.to, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	cmd.Flags().StringVar(&sf.since, "since", "", "Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)")
	ioOpts.setup(cmd.Flags())
	return cmd
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
