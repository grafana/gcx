package kg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/shared"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scopeFlags holds the --env/--namespace/--site/--from/--to/--since flags used by entity search commands.
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
	cmd.Flags().StringVar(&f.since, "since", "", "Duration before --to (or now); mutually exclusive with --from/--to (e.g. 1h, 30m, 7d)")
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
	startMs, endMs, err := f.resolveTime()
	if err != nil {
		return err
	}
	scopes, err := client.ListEntityScopes(ctx, startMs, endMs)
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
				suffix = fmt.Sprintf(" (and %d more — run gcx kg meta scopes)", len(all)-10)
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

// parsePropertyFlag parses a property filter string into a PropertyMatcher.
// Supported formats:
//
//	name=value   — exact match (EQUALS)
//	name=~value  — substring match (CONTAINS); mirrors PromQL label-selector syntax
func parsePropertyFlag(s string) (PropertyMatcher, error) {
	if name, value, ok := strings.Cut(s, "=~"); ok && name != "" {
		return PropertyMatcher{Name: name, Op: "CONTAINS", Value: value}, nil
	}
	name, value, ok := strings.Cut(s, "=")
	if !ok || name == "" {
		return PropertyMatcher{}, fmt.Errorf("--property %q: expected format name=value or name=~value", s)
	}
	return PropertyMatcher{Name: name, Op: "=", Value: value}, nil
}

// insightMatcher is one predicate from --insight, applied client-side against
// the per-assertion entries inlined in SearchResult.Assertion and
// SearchResult.ConnectedAssertion when the request sets withInsights=true.
//
// The bare value "any" is represented as Key="" (wildcard) — it filters to
// entities that have at least one assertion but applies no predicate.
type insightMatcher struct {
	Key   string // "name", "severity", or "" for wildcard ("any")
	Op    string // "=" or "CONTAINS"; "" for wildcard
	Value string
}

// parseInsightFlag parses a --insight predicate.
//
//	any         — match any assertion (entity must have at least one)
//	key=value   — exact match
//	key=~value  — substring match (CONTAINS), allowed for name only
//
// Valid keys: name, severity.
func parseInsightFlag(s string) (insightMatcher, error) {
	if strings.EqualFold(strings.TrimSpace(s), "any") {
		return insightMatcher{}, nil
	}
	if name, value, ok := strings.Cut(s, "=~"); ok && name != "" {
		m := insightMatcher{Key: strings.ToLower(name), Op: "CONTAINS", Value: value}
		if err := validateInsightMatcher(m); err != nil {
			return insightMatcher{}, err
		}
		return m, nil
	}
	name, value, ok := strings.Cut(s, "=")
	if !ok || name == "" {
		return insightMatcher{}, fmt.Errorf("--insight %q: expected 'any' or format key=value / key=~value", s)
	}
	m := insightMatcher{Key: strings.ToLower(name), Op: "=", Value: value}
	if err := validateInsightMatcher(m); err != nil {
		return insightMatcher{}, err
	}
	return m, nil
}

func validateInsightMatcher(m insightMatcher) error {
	switch m.Key {
	case "name":
		return nil
	case "severity":
		if m.Op == "CONTAINS" {
			return errors.New("--insight severity: substring match (=~) is not supported; use severity=critical|warning|info")
		}
		return nil
	default:
		return fmt.Errorf("--insight %q: unsupported key (valid keys: name, severity)", m.Key)
	}
}

func readFileOrStdin(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	if path == "" {
		fi, err := os.Stdin.Stat()
		if err != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
			return nil, errors.New("no input: use -f <file> or pipe YAML via stdin")
		}
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path)
}

// searchByTypes fans out Search across multiple entity types and merges results.
// Server-side (5xx) failures for individual entity types are logged as warnings and skipped
// so that a broken type does not abort results for all other types.
// When the backend signals more pages (MaxLimitHit, or !LastPage) for a per-type
// page, a hint is emitted to stderr suggesting --page to fetch further results.
func searchByTypes(ctx context.Context, cmd *cobra.Command, client *Client, entityTypes []string, assertionsOnly, includePropagated bool, sc *ScopeCriteria, startMs, endMs int64, pageNum int, propertyFilters []PropertyMatcher) ([]SearchResult, error) {
	if startMs == 0 && endMs == 0 {
		now := time.Now().UnixMilli()
		startMs = now - 3600000
		endMs = now
	}
	var allResults []SearchResult
	for _, et := range entityTypes {
		matchers := append([]PropertyMatcher{{Name: "name", Op: "IS NOT NULL", Type: "String"}}, propertyFilters...)
		req := SearchRequest{
			TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
			FilterCriteria: []EntityMatcher{{
				EntityType:                 et,
				HavingAssertion:            assertionsOnly,
				HavingPropagatedAssertions: assertionsOnly && includePropagated,
				PropertyMatchers:           matchers,
			}},
			ScopeCriteria: sc,
			PageNum:       pageNum,
		}
		page, err := client.Search(ctx, req)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.IsServerError() {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping entity type %q — server error: %v\n", et, apiErr)
				continue
			}
			return nil, fmt.Errorf("search entity type %s: %w", et, err)
		}
		if page.MaxLimitHit || (!page.LastPage && len(page.Entities) > 0) {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"hint: more results available for type %q (page %d returned %d) — use --page %d or narrow with --property/--namespace\n",
				et, pageNum, len(page.Entities), pageNum+1)
		}
		allResults = append(allResults, page.Entities...)
	}
	if allResults == nil {
		return []SearchResult{}, nil
	}
	return allResults, nil
}

func collectEntityTypes(cmd *cobra.Command, client *Client) ([]string, error) {
	now := time.Now()
	counts, err := client.CountEntityTypes(cmd.Context(), now.Add(-1*time.Hour).UnixMilli(), now.UnixMilli(), nil)
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
		Use:   "prom-rules",
		Short: "Manage Knowledge Graph Custom Prometheus rules.",
	}

	rulesListOpts := &rulesListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List Knowledge Graph Custom Prometheus rules.",
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

			// Convert to K8s envelope unstructured once; all codecs (table,
			// wide, yaml, json) consume the same shape.
			objs := make([]unstructured.Unstructured, 0, len(typedObjs))
			for i := range typedObjs {
				rule := typedObjs[i].Spec
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
		Short: "Get a Knowledge Graph Custom Prometheus rule by name.",
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
		Short: "Upload Knowledge Graph Custom Prometheus rules from a YAML file.",
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
		Use:   "delete <name>",
		Short: "Delete a Knowledge Graph Custom Prometheus rule by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}
			if err := crud.Delete(ctx, name); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Knowledge Graph rule %q deleted", name)
			return nil
		},
	}

	schemaOpts := &rulesSchemaOpts{}
	schemaCmd := &cobra.Command{
		Use:   "schema",
		Short: "Fetch the live JSON Schema for Custom Prometheus rules from the backend.",
		Long: `Fetches the JSON Schema (Draft 2020-12) that describes the Custom Prometheus rules configuration
shape, derived from the backend DTO tree. Pipe to a file and point your editor at it for autocomplete and
deep validation when authoring prom-rules manifests.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := schemaOpts.IO.Validate(); err != nil {
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
			schema, err := client.GetPromRulesSchema(cmd.Context())
			if err != nil {
				return err
			}
			return schemaOpts.IO.Encode(cmd.OutOrStdout(), schema)
		},
	}
	schemaOpts.setup(schemaCmd.Flags())

	cmd.AddCommand(listCmd, getCmd, createCmd, deleteCmd, schemaCmd)
	return cmd
}

type rulesSchemaOpts struct {
	IO cmdio.Options
}

func (o *rulesSchemaOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

type rulesListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *rulesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &RuleTableCodec{})
	o.IO.RegisterCustomCodec("wide", &RuleWideTableCodec{})
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

// ruleStats holds counts derived from a rule file spec.
type ruleStats struct {
	groups, rules, alerts, recording int
}

// ruleSpecStats walks the rule file spec and returns counts.
func ruleSpecStats(obj unstructured.Unstructured) ruleStats {
	var s ruleStats
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return s
	}
	groupList, _ := spec["groups"].([]any)
	s.groups = len(groupList)
	for _, g := range groupList {
		gMap, ok := g.(map[string]any)
		if !ok {
			continue
		}
		ruleList, _ := gMap["rules"].([]any)
		s.rules += len(ruleList)
		for _, r := range ruleList {
			rMap, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if alert, _ := rMap["alert"].(string); alert != "" {
				s.alerts++
			}
			if record, _ := rMap["record"].(string); record != "" {
				s.recording++
			}
		}
	}
	return s
}

// RuleTableCodec renders rule files as a compact table: name + group/rule counts.
type RuleTableCodec struct{}

func (c *RuleTableCodec) Format() format.Format { return "table" }

func (c *RuleTableCodec) Encode(w io.Writer, v any) error {
	objs, ok := v.([]unstructured.Unstructured)
	if !ok {
		return errors.New("invalid data type for table codec: expected []unstructured.Unstructured")
	}
	t := style.NewTable("NAME", "GROUPS", "RULES")
	for _, obj := range objs {
		s := ruleSpecStats(obj)
		t.Row(obj.GetName(), strconv.Itoa(s.groups), strconv.Itoa(s.rules))
	}
	return t.Render(w)
}

func (c *RuleTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// RuleWideTableCodec adds alert/recording breakdowns to the basic table view.
type RuleWideTableCodec struct{}

func (c *RuleWideTableCodec) Format() format.Format { return "wide" }

func (c *RuleWideTableCodec) Encode(w io.Writer, v any) error {
	objs, ok := v.([]unstructured.Unstructured)
	if !ok {
		return errors.New("invalid data type for wide codec: expected []unstructured.Unstructured")
	}
	t := style.NewTable("NAME", "GROUPS", "RULES", "ALERTS", "RECORDING")
	for _, obj := range objs {
		s := ruleSpecStats(obj)
		t.Row(obj.GetName(),
			strconv.Itoa(s.groups),
			strconv.Itoa(s.rules),
			strconv.Itoa(s.alerts),
			strconv.Itoa(s.recording))
	}
	return t.Render(w)
}

func (c *RuleWideTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("wide format does not support decoding")
}

// ---------------------------------------------------------------------------
// Model rules, suppressions, relabel rules commands
// ---------------------------------------------------------------------------

func newModelRulesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model-rules",
		Short: "Manage model rules in the Knowledge Graph.",
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

	listOpts := &modelRulesListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all custom model rules configurations.",
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
			names, err := client.ListModelRuleNames(cmd.Context())
			if err != nil {
				return err
			}
			return listOpts.IO.Encode(cmd.OutOrStdout(), names)
		},
	}
	listOpts.setup(listCmd.Flags())

	getOpts := &modelRulesGetOpts{}
	getCmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a custom model rules configuration by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			rules, err := client.GetModelRules(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return getOpts.IO.Encode(cmd.OutOrStdout(), rules)
		},
	}
	getOpts.setup(getCmd.Flags())

	var force bool
	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a custom model rules configuration by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.OutOrStdout(), force,
				fmt.Sprintf("Delete model rules %q?", name))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.DeleteModelRules(cmd.Context(), name); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Model rules %q deleted", name)
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	schemaOpts := &modelRulesSchemaOpts{}
	schemaCmd := &cobra.Command{
		Use:   "schema",
		Short: "Fetch the live JSON Schema for ModelRules from the backend.",
		Long: `Fetches the JSON Schema (Draft 2020-12) that describes the ModelRules configuration shape,
derived from the backend DTO tree. Pipe to a file and point your editor at it for autocomplete and
deep validation when authoring model rules manifests.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := schemaOpts.IO.Validate(); err != nil {
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
			schema, err := client.GetModelRulesSchema(cmd.Context())
			if err != nil {
				return err
			}
			return schemaOpts.IO.Encode(cmd.OutOrStdout(), schema)
		},
	}
	schemaOpts.setup(schemaCmd.Flags())

	cmd.AddCommand(createCmd, listCmd, getCmd, deleteCmd, schemaCmd)
	return cmd
}

type modelRulesSchemaOpts struct {
	IO cmdio.Options
}

func (o *modelRulesSchemaOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

type modelRulesListOpts struct {
	IO cmdio.Options
}

func (o *modelRulesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ModelRulesNameTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

type modelRulesGetOpts struct {
	IO cmdio.Options
}

func (o *modelRulesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

// ModelRulesNameTableCodec renders model rule names as a single-column table.
type ModelRulesNameTableCodec struct{}

func (c *ModelRulesNameTableCodec) Format() format.Format { return "table" }

func (c *ModelRulesNameTableCodec) Encode(w io.Writer, v any) error {
	names, ok := v.([]string)
	if !ok {
		return errors.New("invalid data type for table codec: expected []string")
	}
	t := style.NewTable("NAME")
	for _, n := range names {
		t.Row(n)
	}
	return t.Render(w)
}

func (c *ModelRulesNameTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newSuppressionsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suppressions",
		Short: "Manage alert suppressions in the Knowledge Graph.",
	}

	listOpts := &suppressionsListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all alert suppressions.",
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
			suppressions, err := client.GetSuppressions(cmd.Context())
			if err != nil {
				return err
			}
			return listOpts.IO.Encode(cmd.OutOrStdout(), suppressions.DisabledAlertConfigs)
		},
	}
	listOpts.setup(listCmd.Flags())

	var fileFlag string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create or update one or more suppressions from a YAML file or stdin.",
		Example: `  gcx kg suppressions create -f suppressions.yaml

  echo 'disabledAlertConfigs:
    - name: my-suppression
      matchLabels:
        alertname: ErrorRatioBreach
        job: my-service' | gcx kg suppressions create`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readFileOrStdin(cmd, fileFlag)
			if err != nil {
				return err
			}
			var suppressions Suppressions
			if err := yaml.Unmarshal(data, &suppressions); err != nil {
				return fmt.Errorf("failed to parse suppressions file: %w", err)
			}
			if len(suppressions.DisabledAlertConfigs) == 0 {
				return errors.New("no suppressions found in file")
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			total := len(suppressions.DisabledAlertConfigs)
			for i, s := range suppressions.DisabledAlertConfigs {
				if err := client.UpsertSuppression(cmd.Context(), s); err != nil {
					return fmt.Errorf("failed to upsert suppression %q (%d/%d succeeded): %w", s.Name, i, total, err)
				}
			}
			cmdio.Success(cmd.OutOrStdout(), "%d suppression(s) upserted", total)
			return nil
		},
	}
	createCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Input file (YAML), or '-' for stdin. Reads from stdin if omitted.")

	var force bool
	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a suppression by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.OutOrStdout(), force,
				fmt.Sprintf("Delete suppression %q?", name))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.DeleteSuppression(cmd.Context(), name); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Suppression %q deleted", name)
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	cmd.AddCommand(listCmd, createCmd, deleteCmd)
	return cmd
}

type suppressionsListOpts struct {
	IO cmdio.Options
}

func (o *suppressionsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &SuppressionTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// SuppressionTableCodec renders suppressions as a table.
type SuppressionTableCodec struct{}

func (c *SuppressionTableCodec) Format() format.Format { return "table" }

func (c *SuppressionTableCodec) Encode(w io.Writer, v any) error {
	suppressions, ok := v.([]Suppression)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Suppression")
	}
	t := style.NewTable("NAME", "MATCH LABELS")
	for _, s := range suppressions {
		t.Row(s.Name, scopeStr(s.MatchLabels))
	}
	return t.Render(w)
}

func (c *SuppressionTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newRelabelRulesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relabel-rules",
		Short: "Inspect Mimir relabel rules used by the Knowledge Graph.",
	}

	var (
		ruleType string
		io       cmdio.Options
	)
	io.RegisterCustomCodec("table", &RelabelRuleTableCodec{})
	io.DefaultFormat("table")

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Fetch a relabel rule group (prologue, epilogue, or generated).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			t := RelabelRuleType(ruleType)
			if !t.IsValid() {
				return fmt.Errorf("invalid --type %q: must be one of prologue, epilogue, generated", ruleType)
			}
			if err := io.Validate(); err != nil {
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
			rules, err := client.GetRelabelRules(cmd.Context(), t)
			if err != nil {
				return err
			}
			if rules == nil {
				cmdio.Info(cmd.ErrOrStderr(), "No %s relabel rules configured.", t)
				return nil
			}
			return io.Encode(cmd.OutOrStdout(), rules)
		},
	}
	getCmd.Flags().StringVar(&ruleType, "type", string(RelabelRuleTypeGenerated),
		"Rule group to fetch: prologue, epilogue, or generated")
	io.BindFlags(getCmd.Flags())

	cmd.AddCommand(getCmd)
	return cmd
}

// RelabelRuleTableCodec renders a relabel rule group as a compact table.
// Shows the commonly-inspected fields — use `-o yaml` or `-o json` for
// the remaining ones (transform_*, join_separator).
type RelabelRuleTableCodec struct{}

func (c *RelabelRuleTableCodec) Format() format.Format { return "table" }

func (c *RelabelRuleTableCodec) Encode(w io.Writer, v any) error {
	group, ok := v.(map[string]any)
	if !ok {
		return errors.New("invalid data type for table codec: expected map[string]any")
	}
	rawRules, _ := group["rules"].([]any)
	t := style.NewTable("SELECTOR", "TARGET LABEL", "JOIN LABELS", "RANKED CHOICE", "REPLACEMENT", "DROP")
	for _, r := range rawRules {
		rule, _ := r.(map[string]any)
		if rule == nil {
			continue
		}
		t.Row(
			stringField(rule["selector"]),
			stringField(rule["target_label"]),
			joinStringSlice(rule["join_labels"]),
			joinStringSlice(rule["ranked_choice"]),
			stringField(rule["replacement"]),
			boolField(rule["drop"]),
		)
	}
	return t.Render(w)
}

func (c *RelabelRuleTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func stringField(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func boolField(v any) string {
	b, ok := v.(bool)
	if !ok || !b {
		return ""
	}
	return "true"
}

func joinStringSlice(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// Entities commands
// ---------------------------------------------------------------------------

func newEntitiesCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entities",
		Short: "Manage Knowledge Graph entities.",
		Long: `Manage Knowledge Graph entities.

Prefer 'list' for listing and for basic lookups (an entity's identity and
properties — the labels used to build PromQL/Loki queries); it is cheap. Use
'inspect' only when you need an entity's insight timeline or related entities
for root-cause analysis — it is heavier and can return large output,
so don't use it just to read properties.`,
	}

	// list subcommand
	var (
		listType        string
		listScope       scopeFlags
		listPage        int
		listPropertyRaw []string
		listInsightRaw  []string
	)
	listOpts := &entitiesListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List entities by type/scope, or look up an entity's identity and properties.",
		Long: `List Knowledge Graph entities for a given type, env, site, namespace.

The cheap default for listing entities or looking up basic information about
one: its identity (name, type, scope), properties (the labels used to build
PromQL/Loki queries — job, namespace, container, image, etc.), and a small
snapshot of its current insights. Filter to a single entity with
'--property name=<exact-name>'.

For an entity's full insight timeline or related entities for root-cause
analysis, use 'gcx kg entities inspect' instead.`,
		Example: `  gcx kg entities list --type Service --env <env> --namespace <namespace> --property name=<service-name>
  gcx kg entities list --type Service --env <env> --insight any
  gcx kg entities list --type Service --env <env> --insight name=Saturation --insight severity=critical
  gcx kg entities list --type Service --env <env> --property name=~api --insight severity=critical
  gcx kg entities list --type Service --env <env> --insight severity=critical --json name,scope`,
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
			startMs, endMs, err := listScope.resolveTime()
			if err != nil {
				return err
			}
			if err := listScope.validateScopes(cmd.Context(), client); err != nil {
				return err
			}
			entityTypes, err := resolveEntityTypes(cmd, client, listType)
			if err != nil {
				return err
			}
			var propertyFilters []PropertyMatcher
			for _, raw := range listPropertyRaw {
				pm, err := parsePropertyFlag(raw)
				if err != nil {
					return err
				}
				propertyFilters = append(propertyFilters, pm)
			}
			var insightMatchers []insightMatcher
			for _, raw := range listInsightRaw {
				im, err := parseInsightFlag(raw)
				if err != nil {
					return err
				}
				insightMatchers = append(insightMatchers, im)
			}
			// --insight requests inlined assertion data so we can filter client-side.
			fetchInsights := len(insightMatchers) > 0
			results, err := searchByTypes(cmd.Context(), cmd, client, entityTypes, fetchInsights, true, listScope.scopeCriteria(), startMs, endMs, listPage, propertyFilters)
			if err != nil {
				return err
			}
			if len(insightMatchers) > 0 {
				results = filterByInsightMatchers(results, insightMatchers)
			}
			results = adapter.TruncateSlice(results, listOpts.Limit)
			if listOpts.Limit > 0 && int64(len(results)) >= listOpts.Limit {
				fmt.Fprintf(os.Stderr, "hint: --limit of %d reached — results may be truncated; raise --limit or pass --limit 0 for all\n", listOpts.Limit)
			}
			return listOpts.IO.Encode(cmd.OutOrStdout(), results)
		},
	}
	listCmd.Flags().StringVar(&listType, "type", "", "Entity type to list (run 'gcx kg meta schema' to see available types)")
	listCmd.Flags().IntVar(&listPage, "page", 0, "Page number (0-based)")
	listCmd.Flags().StringArrayVar(&listPropertyRaw, "property", nil, "Filter by property: name=value (exact) or name=~value (contains); repeatable (run 'gcx kg meta schema' to list property names)")
	listCmd.Flags().StringArrayVar(&listInsightRaw, "insight", nil, "Filter to entities with an active insight: 'any' (has any insight) or key=value (key=~value for substring; name only); valid keys: name, severity (critical|warning|info); repeatable — multiple predicates must match the same assertion")
	listScope.register(listCmd)
	listCmd.Flags().Lookup("env").Usage = "Environment scope (run 'gcx kg meta scopes' to see valid values)"
	listCmd.Flags().Lookup("namespace").Usage = "Namespace scope (run 'gcx kg meta scopes' to see valid values)"
	listCmd.Flags().Lookup("site").Usage = "Site scope (run 'gcx kg meta scopes' to see valid values)"
	listOpts.setup(listCmd.Flags())
	_ = listCmd.MarkFlagRequired("type")

	cmd.AddCommand(listCmd, newEntitiesInspectCommand(loader), newCypherCommand(loader),
		newEntitiesCreateCommand(loader), newEntitiesDeleteCommand(loader))
	return cmd
}

type entitiesListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *entitiesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &EntityTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all; the backend may still page results — use --page to paginate)")
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
// Insights commands
// ---------------------------------------------------------------------------

func newAssertionsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Fetch chart data and source metrics for an active insight.",
	}

	// chart subcommand
	var entityMetricScope scopeFlags
	var entityMetricLabels []string
	entityMetricCmd := &cobra.Command{
		Use:   "chart [Type--Name]",
		Short: "Get chart data (series + thresholds) for a specific insight on an entity.",
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
				assertionID, _ := cmd.Flags().GetString("insight")
				if assertionID == "" {
					return errors.New("--insight is required (or use --file)")
				}
				startMs, endMs, err := entityMetricScope.resolveTime()
				if err != nil {
					return err
				}
				if err := entityMetricScope.validateScopes(cmd.Context(), client); err != nil {
					return err
				}
				labels := map[string]string{
					"alertname":           assertionID,
					"asserts_entity_type": entityType,
					"asserts_entity_name": name,
				}
				maps.Copy(labels, entityMetricScope.scopeMap())
				for _, kv := range entityMetricLabels {
					k, v, ok := strings.Cut(kv, "=")
					if !ok || k == "" {
						return fmt.Errorf("invalid --label %q: expected key=value", kv)
					}
					labels[k] = v
				}
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
	entityMetricCmd.Flags().String("insight", "", "Insight name (e.g. LatencyAverageBreach, ResourceRateAnomaly) — sets the 'alertname' label")
	entityMetricCmd.Flags().StringArrayVar(&entityMetricLabels, "label", nil, "Extra assertion label as key=value (repeatable; e.g. asserts_resource_type=jvm:live_threads to narrow ResourceRateAnomaly to a specific resource)")
	entityMetricScope.register(entityMetricCmd)

	// sources subcommand. The server matches on the assertion's full label set
	// (alertname + request_context + request_type + job + ...), so the user
	// typically pastes the labels block from `kg entities inspect`
	// timeLines[].labels (which includes alertname). --insight is sugar
	// for --label alertname=<value>.
	var sourceMetricsScope scopeFlags
	var sourceMetricsLabels []string
	sourceMetricsCmd := &cobra.Command{
		Use:   "sources [Type--Name]",
		Short: "List the underlying metrics (name + label matchers) that source a specific insight.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
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
				entityType, name, err := resolveEntityTypeAndName(cmd, args)
				if err != nil {
					return err
				}
				startMs, endMs, err := sourceMetricsScope.resolveTime()
				if err != nil {
					return err
				}
				if err := sourceMetricsScope.validateScopes(cmd.Context(), client); err != nil {
					return err
				}
				labels := map[string]string{
					"asserts_entity_type": entityType,
					"asserts_entity_name": name,
				}
				if id, _ := cmd.Flags().GetString("insight"); id != "" {
					labels["alertname"] = id
				}
				maps.Copy(labels, sourceMetricsScope.scopeMap())
				for _, kv := range sourceMetricsLabels {
					k, v, ok := strings.Cut(kv, "=")
					if !ok || k == "" {
						return fmt.Errorf("invalid --label %q: expected key=value", kv)
					}
					labels[k] = v
				}
				req = SourceMetricsRequest{
					StartTime: startMs,
					EndTime:   endMs,
					Labels:    labels,
				}
			}
			results, err := client.AssertionSourceMetrics(cmd.Context(), req)
			if err != nil {
				return err
			}
			return (&cmdio.Options{OutputFormat: "json"}).Encode(cmd.OutOrStdout(), results)
		},
	}
	sourceMetricsCmd.Flags().StringP("file", "f", "", "Input file (YAML)")
	sourceMetricsCmd.Flags().String("name", "", "Entity name")
	sourceMetricsCmd.Flags().String("type", "", "Entity type")
	sourceMetricsCmd.Flags().String("insight", "", "Insight name (e.g. LatencyAverageBreach, ResourceRateAnomaly) — sets the 'alertname' label")
	sourceMetricsCmd.Flags().StringArrayVar(&sourceMetricsLabels, "label", nil, "Assertion label as key=value (repeatable; typically copied from 'kg entities inspect' timeLines[].labels)")
	sourceMetricsScope.register(sourceMetricsCmd)

	cmd.AddCommand(entityMetricCmd, sourceMetricsCmd)
	return cmd
}

// filterByInsightMatchers filters results to those whose inlined assertions
// satisfy all matchers. An entity matches when at least one assertion in
// SearchResult.Assertion.assertions or SearchResult.ConnectedAssertion.assertions
// satisfies every matcher (matchers AND on the same assertion).
func filterByInsightMatchers(results []SearchResult, matchers []insightMatcher) []SearchResult {
	if len(matchers) == 0 {
		return results
	}
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if anyAssertionMatches(r.Assertion, matchers) || anyAssertionMatches(r.ConnectedAssertion, matchers) {
			out = append(out, r)
		}
	}
	return out
}

func anyAssertionMatches(group map[string]any, matchers []insightMatcher) bool {
	if group == nil {
		return false
	}
	arr, ok := group["assertions"].([]any)
	if !ok {
		return false
	}
	for _, a := range arr {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		if assertionMatchesAll(m, matchers) {
			return true
		}
	}
	return false
}

func assertionMatchesAll(a map[string]any, matchers []insightMatcher) bool {
	for _, m := range matchers {
		var field string
		switch m.Key {
		case "":
			// Wildcard ("any") — predicate is a no-op; the surrounding
			// anyAssertionMatches already enforces that an assertion exists.
			continue
		case "name":
			field, _ = a["assertionName"].(string)
		case "severity":
			field, _ = a["severity"].(string)
		default:
			return false
		}
		switch m.Op {
		case "=":
			if !strings.EqualFold(field, m.Value) {
				return false
			}
		case "CONTAINS":
			if !strings.Contains(strings.ToLower(field), strings.ToLower(m.Value)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Entities describe command
// ---------------------------------------------------------------------------

// inspectScopeHint searches for an entity by exact name across all scopes and
// returns formatted retry suggestions when LLMSummary returns 404. This helps
// agents and users recover when the scope is incomplete or wrong.
func inspectScopeHint(ctx context.Context, client *Client, entityType, name string, startMs, endMs int64) string {
	req := SampleSearchRequest{
		TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
		FilterCriteria: []EntityMatcher{{
			EntityType:       entityType,
			PropertyMatchers: []PropertyMatcher{{Name: "name", Op: "=", Value: name}},
		}},
		SampleSize: 10,
	}
	results, err := client.SearchSample(ctx, req)
	if err != nil || len(results) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("Found %d matching %s entr%s in other scopes — retry with:", len(results), entityType, map[bool]string{true: "y", false: "ies"}[len(results) == 1]))
	for _, r := range results {
		parts := []string{fmt.Sprintf("  gcx kg entities inspect %s--%s", r.Type, r.Name)}
		for _, dim := range []string{"env", "namespace", "site"} {
			if v := r.Scope[dim]; v != "" {
				parts = append(parts, fmt.Sprintf("--%s %s", dim, v))
			}
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	return strings.Join(lines, "\n")
}

// isEmptyLLMResult returns true when llm-summary returns 200 but no real data.
// The endpoint echoes the requested entity in "summaries" regardless of whether
// it exists, so summaries being non-empty is not a "found" signal. The only
// reliable indicator of actual data is a non-empty "graphData".
func isEmptyLLMResult(result map[string]any) bool {
	gd, _ := result["graphData"].([]any)
	return len(gd) == 0
}

// discoverEntityScope resolves the scope for an entity when the caller didn't
// provide one. It first tries LookupEntity, then falls back to a name-exact
// search. Returns nil scope (not an error) when the entity simply isn't found —
// the caller lets LLMSummary produce the definitive not-found response.
func discoverEntityScope(cmd *cobra.Command, client *Client, entityType, name, domain string, startMs, endMs int64) (map[string]string, error) {
	lookup, err := client.LookupEntity(cmd.Context(), entityType, name, nil, domain, startMs, endMs)
	if err != nil {
		return nil, err
	}
	if lookup != nil {
		return lookup.Scope, nil
	}
	// Only LookupEntity honors the domain filter. The searchByTypes fallback
	// below has no domain filter (the Search API's ScopeCriteria is label
	// values only, and SearchResult carries no domain), so widening to it
	// when --domain is set could surface an entity from another domain —
	// contradicting the flag. Stop here and let the caller produce the
	// not-found response, keeping discovery scoped to the requested domain.
	if domain != "" {
		return nil, nil //nolint:nilnil // deliberate: no match within the requested domain is not an error
	}
	results, err := searchByTypes(cmd.Context(), cmd, client, []string{entityType}, false, false, nil, startMs, endMs, 0, []PropertyMatcher{{Name: "name", Op: "=", Value: name}})
	if err != nil {
		return nil, err
	}
	switch len(results) {
	case 0:
		return nil, nil //nolint:nilnil // deliberate: no scope found is not an error; caller lets LLMSummary produce the not-found response
	case 1:
		return results[0].Scope, nil
	default:
		var lines []string
		lines = append(lines, fmt.Sprintf("found %d entities named %q — re-run with one of:", len(results), name))
		for _, r := range results {
			parts := []string{fmt.Sprintf("  gcx kg entities inspect %s--%s", r.Type, r.Name)}
			for _, dim := range []string{"env", "namespace", "site"} {
				if v := r.Scope[dim]; v != "" {
					parts = append(parts, fmt.Sprintf("--%s %s", dim, v))
				}
			}
			lines = append(lines, strings.Join(parts, " "))
		}
		return nil, errors.New(strings.Join(lines, "\n"))
	}
}

func newEntitiesInspectCommand(loader RESTConfigLoader) *cobra.Command {
	var inspectScope scopeFlags
	var inspectDomain string
	ioOpts := &inspectOpts{}
	cmd := &cobra.Command{
		Use:   "inspect [Type--Name]",
		Short: "Show the insight timeline and related entities for a single entity (root-cause analysis).",
		Long: `Show detailed root-cause-analysis context for a single entity, including a link
to the RCA Workbench.

Use this only when you need the detail: the entity's insight/assertion timeline
and the related entities to investigate next. It calls a heavier
endpoint and can return large output.

Do not use 'inspect' just to read an entity's identity or properties — for that,
use 'gcx kg entities list' (optionally with '--property name=<exact-name>' to
narrow to one entity), which is cheaper and returns those fields directly.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ioOpts.Validate(cmd.Flags()); err != nil {
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
			if err := inspectScope.validateScopes(cmd.Context(), client); err != nil {
				return err
			}
			entityType, name, err := resolveEntityTypeAndName(cmd, args)
			if err != nil {
				return err
			}
			scope := inspectScope.scopeMap()
			if scope == nil {
				discovered, err := discoverEntityScope(cmd, client, entityType, name, inspectDomain, startMs, endMs)
				if err != nil {
					return err
				}
				scope = discovered
			}

			hideOlderHours, hideChronicPct := ioOpts.resolveInsightFilters(cmd.Flags())

			llmReq := LLMSummaryRequest{
				StartTime: startMs,
				EndTime:   endMs,
				EntityKeys: []EntityKey{{
					Type:  entityType,
					Name:  name,
					Scope: toAnyMap(scope),
				}},
				SuggestionSrcEntities:                         []EntityKey{},
				AlertCategories:                               ioOpts.InsightCategories,
				HideAssertionsOlderThanNHours:                 hideOlderHours,
				HideAssertionsPresentMoreThanPercentageOfTime: hideChronicPct,
				IncludeSuggestions:                            true,
				IncludeRcaPatterns:                            false,
			}
			result, err := client.LLMSummary(cmd.Context(), llmReq)
			if err != nil {
				var apiErr *APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
					if hint := inspectScopeHint(cmd.Context(), client, entityType, name, startMs, endMs); hint != "" {
						return fmt.Errorf("%w\n\n%s", err, hint)
					}
				}
				return err
			}
			// The llm-summary endpoint echoes an empty "rcaPatterns" array (we
			// request IncludeRcaPatterns: false). Drop it when empty so inspect
			// output doesn't carry a permanently-empty field.
			if p, ok := result["rcaPatterns"].([]any); ok && len(p) == 0 {
				delete(result, "rcaPatterns")
			}
			if isEmptyLLMResult(result) {
				scopeDesc := ""
				if len(scope) > 0 {
					scopeDesc = " in " + scopeStr(scope)
				}
				if hint := inspectScopeHint(cmd.Context(), client, entityType, name, startMs, endMs); hint != "" {
					return fmt.Errorf("%s/%s not found%s\n\n%s", entityType, name, scopeDesc, hint)
				}
				return fmt.Errorf("%s/%s not found%s\nRun 'gcx kg entities list --type %s --property name=~%s' to find matching entities and their correct scope", entityType, name, scopeDesc, entityType, name)
			}
			if u := rcaWorkbenchURL(cfg.GrafanaURL, entityType, name, scope, startMs, endMs, inspectScope.since); u != "" {
				if ioOpts.ShareLink {
					cmdio.Info(cmd.ErrOrStderr(), "RCA Workbench: %s", u)
				}
				if ioOpts.Open {
					cmdio.Info(cmd.ErrOrStderr(), "Opening RCA Workbench for %s/%s", entityType, name)
					if err := deeplink.Open(u); err != nil {
						cmdio.Warning(cmd.ErrOrStderr(), "could not open browser: %v", err)
					}
				}
			}
			return ioOpts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().String("type", "", "Entity type (run 'gcx kg meta schema' to see available types)")
	cmd.Flags().String("name", "", "Entity name")
	cmd.Flags().StringVar(&inspectDomain, "domain", "", "Restrict scope auto-discovery to a single KG domain (only applies when scope is auto-discovered; no effect when --env/--namespace/--site is given)")
	inspectScope.register(cmd)
	cmd.Flags().Lookup("env").Usage = "Environment scope (run 'gcx kg meta scopes' to see valid values)"
	cmd.Flags().Lookup("namespace").Usage = "Namespace scope (run 'gcx kg meta scopes' to see valid values)"
	cmd.Flags().Lookup("site").Usage = "Site scope (run 'gcx kg meta scopes' to see valid values)"
	ioOpts.setup(cmd.Flags())
	return cmd
}

type inspectOpts struct {
	IO                      cmdio.Options
	ShareLink               bool
	Open                    bool
	InsightCategories       []string
	InsightHideNoise        bool
	InsightHideOlderThan    time.Duration
	InsightHideChronicAbove int
}

func (o *inspectOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.ShareLink, "share-link", false, "Print the RCA Workbench URL for this entity to stderr")
	flags.BoolVar(&o.Open, "open", false, "Open the entity in the RCA Workbench in your browser")
	flags.StringSliceVar(&o.InsightCategories, "insight-categories", nil, "Filter insights by category (comma-separated, e.g. saturation,anomaly,failure,error,amend); empty = all categories")
	flags.BoolVar(&o.InsightHideNoise, "insight-hide-noise", false, "Apply RCA Workbench noise filter: hide insights older than 48h or present >90% of the window")
	flags.DurationVar(&o.InsightHideOlderThan, "insight-hide-older-than", 0, "Hide insights older than a whole number of hours (e.g. 24h); overrides --insight-hide-noise on this axis")
	flags.IntVar(&o.InsightHideChronicAbove, "insight-hide-chronic-above", 0, "Hide insights present more than this percent of the window (0-100); overrides --insight-hide-noise on this axis")
}

func (o *inspectOpts) Validate(flags *pflag.FlagSet) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if flags.Changed("insight-hide-older-than") {
		if o.InsightHideOlderThan <= 0 || o.InsightHideOlderThan%time.Hour != 0 {
			return fmt.Errorf("--insight-hide-older-than must be a positive whole number of hours (e.g. 24h), got %s", o.InsightHideOlderThan)
		}
	}
	if flags.Changed("insight-hide-chronic-above") {
		if o.InsightHideChronicAbove < 0 || o.InsightHideChronicAbove > 100 {
			return fmt.Errorf("--insight-hide-chronic-above must be between 0 and 100, got %d", o.InsightHideChronicAbove)
		}
	}
	return nil
}

// resolveInsightFilters returns the hours and percent thresholds to send to the
// LLM summary API, applying the --insight-hide-noise preset and per-axis overrides.
func (o *inspectOpts) resolveInsightFilters(flags *pflag.FlagSet) (int, int) {
	hideOlderHours := 0
	hideChronicPct := 0
	if o.InsightHideNoise {
		hideOlderHours = 48
		hideChronicPct = 90
	}
	if flags.Changed("insight-hide-older-than") {
		hideOlderHours = int(o.InsightHideOlderThan.Hours())
	}
	if flags.Changed("insight-hide-chronic-above") {
		hideChronicPct = o.InsightHideChronicAbove
	}
	return hideOlderHours, hideChronicPct
}

// rcaWorkbenchURL builds a deep link to the Asserts RCA Workbench for a single entity.
// start/end use the relative expression (e.g. "now-24h"/"now") when since is set,
// otherwise fall back to millisecond epoch timestamps.
func rcaWorkbenchURL(host, entityType, name string, scope map[string]string, startMs, endMs int64, since string) string {
	if host == "" {
		return ""
	}
	start, end := strconv.FormatInt(startMs, 10), strconv.FormatInt(endMs, 10)
	if since != "" {
		start, end = "now-"+since, "now"
	}

	q := url.Values{}
	q.Set("start", start)
	q.Set("end", end)
	if v := scope["env"]; v != "" {
		q.Set("env[0]", v)
	}
	if v := scope["namespace"]; v != "" {
		q.Set("namespace[0]", v)
	}
	if v := scope["site"]; v != "" {
		q.Set("site[0]", v)
	}
	q.Set("we[0][n]", name)
	q.Set("we[0][tp]", entityType)
	if v := scope["namespace"]; v != "" {
		q.Set("we[0][sc][ns]", v)
	}
	if v := scope["env"]; v != "" {
		q.Set("we[0][sc][env]", v)
	}
	if v := scope["site"]; v != "" {
		q.Set("we[0][sc][site]", v)
	}
	q.Set("view", "BY_ENTITY")

	// url.Values.Encode percent-encodes brackets; replace them back for readability.
	encoded := strings.NewReplacer("%5B", "[", "%5D", "]").Replace(q.Encode())
	return strings.TrimRight(host, "/") + "/a/grafana-asserts-app/assertions?" + encoded
}

// ---------------------------------------------------------------------------
// Summary command
// ---------------------------------------------------------------------------

func newSummaryCommand(loader RESTConfigLoader) *cobra.Command {
	var (
		summaryScope      scopeFlags
		summaryEntityType string
	)
	ioOpts := &summaryOpts{}
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show a summary of entities and active insights, broken down by type, severity, and insight name.",
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
			startMs, endMs, err := summaryScope.resolveTime()
			if err != nil {
				return err
			}
			if err := summaryScope.validateScopes(cmd.Context(), client); err != nil {
				return err
			}

			counts, err := client.CountEntityTypes(cmd.Context(), startMs, endMs, summaryScope.scopeCriteria())
			if err != nil {
				return err
			}
			typeCounts := map[string]int64{}
			var entityTypes []string
			var totalEntities int64
			for t, cnt := range counts {
				totalEntities += cnt
				if cnt > 0 {
					typeCounts[t] = cnt
					entityTypes = append(entityTypes, t)
				}
			}
			if summaryEntityType != "" {
				entityTypes = []string{summaryEntityType}
			}
			results, err := searchByTypes(cmd.Context(), cmd, client, entityTypes, true, false, summaryScope.scopeCriteria(), startMs, endMs, 0, nil)
			if err != nil {
				return err
			}
			sevCounts := map[string]int{}
			nameCounts := map[string]int{}
			totalInsights := 0
			for _, r := range results {
				assertions, _ := r.Assertion["assertions"].([]any)
				for _, a := range assertions {
					m, ok := a.(map[string]any)
					if !ok {
						continue
					}
					totalInsights++
					name, _ := m["assertionName"].(string)
					if name == "" {
						name = "UNKNOWN"
					}
					nameCounts[name]++
					sev, _ := m["severity"].(string)
					if sev == "" {
						sevCounts["UNKNOWN"]++
					} else {
						sevCounts[strings.ToUpper(sev)]++
					}
				}
			}
			type entitiesSummary struct {
				Total  int64            `json:"total" yaml:"total"`
				ByType map[string]int64 `json:"byType" yaml:"byType"`
			}
			type insightsSummary struct {
				Total      int            `json:"total" yaml:"total"`
				BySeverity map[string]int `json:"bySeverity" yaml:"bySeverity"`
				ByName     map[string]int `json:"byName" yaml:"byName"`
			}
			return ioOpts.IO.Encode(cmd.OutOrStdout(), struct {
				Entities entitiesSummary `json:"entities" yaml:"entities"`
				Insights insightsSummary `json:"insights" yaml:"insights"`
			}{
				Entities: entitiesSummary{Total: totalEntities, ByType: typeCounts},
				Insights: insightsSummary{Total: totalInsights, BySeverity: sevCounts, ByName: nameCounts},
			})
		},
	}
	summaryScope.register(cmd)
	cmd.Flags().StringVar(&summaryEntityType, "type", "", "Limit to a specific entity type")
	ioOpts.setup(cmd.Flags())
	return cmd
}

type summaryOpts struct {
	IO cmdio.Options
}

func (o *summaryOpts) setup(flags *pflag.FlagSet) {
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
// Cypher query command
// ---------------------------------------------------------------------------

func newCypherCommand(loader RESTConfigLoader) *cobra.Command {
	var (
		cypherScope  scopeFlags
		cypherPage   int
		withInsights bool
	)
	ioOpts := &cypherOpts{}
	cmd := &cobra.Command{
		Use:   "query <cypher-query>",
		Short: "Query entities by running a read-only Cypher query against the Knowledge Graph.",
		Long: `Query entities by running a read-only Cypher query against the Knowledge Graph.

Run 'gcx kg meta schema' to discover valid entity types, property names, and relationship names.

Response shape (not raw Cypher rows — results are aggregated into this envelope):

  {
    "entities": [ { "type", "name", "scope", "properties", "insights" }, ... ],
    "edges":    [ { "type", "sourceName", "sourceType", "sourceScope",
                    "destinationName", "destinationType", "destinationScope" }, ... ],
    "pageNum":  <int>,
    "lastPage": <bool>
  }

Tips:
  - Prefer whole-entity projections like 'RETURN s, d' over scalar projections
    like 'RETURN d.name'. Whole entities populate the 'entities' array with
    full type/name/scope/properties; scalar projections do not round-trip
    through this envelope.
  - The --json flag selects keys from the envelope above (entities, edges,
    pageNum, lastPage) — it does NOT filter properties inside each entity.
    Use '--json list' to see the envelope keys, then '--json entities' and
    pipe to jq/python for per-entity field shaping.`,
		Example: `  gcx kg entities query "MATCH (s:Service) RETURN s LIMIT 10"
  gcx kg entities query "MATCH (s:Service)-[:CALLS]->(d:Service) RETURN s, d" --since 1h
  gcx kg entities query "MATCH (s:Service {namespace: 'prod'}) RETURN s" --since 1h`,
		Args: cobra.ExactArgs(1),
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
			startMs, endMs, err := cypherScope.resolveTime()
			if err != nil {
				return err
			}
			req := CypherSearchRequest{
				CypherQuery:  args[0],
				TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
				PageNum:      cypherPage,
				WithInsights: withInsights,
			}
			resp, err := client.CypherSearch(cmd.Context(), req)
			if err != nil {
				return err
			}
			return ioOpts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&cypherScope.from, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	cmd.Flags().StringVar(&cypherScope.to, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	cmd.Flags().StringVar(&cypherScope.since, "since", "", "Duration before --to (or now); mutually exclusive with --from/--to (e.g. 1h, 30m, 7d)")
	cmd.Flags().IntVar(&cypherPage, "page", 0, "Page number (0-based)")
	cmd.Flags().BoolVar(&withInsights, "insights-only", false, "Return only entities with active insights")
	ioOpts.setup(cmd.Flags())
	return cmd
}

type cypherOpts struct {
	IO cmdio.Options
}

func (o *cypherOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &CypherTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// CypherTableCodec renders a CypherSearchResponse as a table of entities.
type CypherTableCodec struct{}

func (c *CypherTableCodec) Format() format.Format { return "table" }

func (c *CypherTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*CypherSearchResponse)
	if !ok {
		return errors.New("invalid data type for table codec: expected *CypherSearchResponse")
	}
	t := style.NewTable("TYPE", "NAME", "SCOPE")
	for _, e := range resp.Entities {
		var parts []string
		for k, val := range e.Scope {
			parts = append(parts, fmt.Sprintf("%s=%v", k, val))
		}
		sort.Strings(parts)
		t.Row(e.Type, e.Name, strings.Join(parts, ", "))
	}
	return t.Render(w)
}

func (c *CypherTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
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
		scopeRename := map[string]string{
			"scope_env":       "env",
			"scope_site":      "site",
			"scope_namespace": "namespace",
		}
		for prop := range e.Properties {
			if ignoredProps[prop] || strings.HasPrefix(prop, "_") || strings.HasPrefix(prop, "lookup_") {
				continue
			}
			if renamed, ok := scopeRename[prop]; ok {
				typeProps[name][renamed] = true
				continue
			}
			if strings.HasPrefix(prop, "scope_") {
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
	IO   cmdio.Options
	Time scopeFlags
}

func (o *describeOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &DescribeTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *describeOpts) setupWithTime(flags *pflag.FlagSet) {
	o.setup(flags)
	flags.StringVar(&o.Time.from, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&o.Time.to, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&o.Time.since, "since", "", "Duration before --to (or now); mutually exclusive with --from/--to (e.g. 1h, 30m, 7d)")
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

	if out.Metrics != nil {
		sections = append(sections, formatMetricSection(*out.Metrics))
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
	cmd := &cobra.Command{
		Use:   "meta",
		Short: "Show Knowledge Graph metadata: entity types, valid env/namespace/site values, and telemetry query configs.",
	}
	cmd.AddCommand(
		newDescribeSchemaCmd(loader),
		newDescribeScopesCmd(loader),
		newDescribeLogsCmd(loader),
		newDescribeTracesCmd(loader),
		newDescribeProfilesCmd(loader),
		newDescribeMetricsCmd(loader),
		newDescribeAllCmd(loader),
	)
	return cmd
}

func newDescribeSchemaCmd(loader RESTConfigLoader) *cobra.Command {
	opts := &describeOpts{}
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Show entity types, properties, and relationships.",
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
			startMs, endMs, err := opts.Time.resolveTime()
			if err != nil {
				return err
			}
			schemaResp, err := client.FetchGraphSchema(cmd.Context(), startMs, endMs)
			if err != nil {
				return err
			}
			result := processGraphSchema(schemaResp)
			return opts.IO.Encode(cmd.OutOrStdout(), KGMetadataOutput{Schema: &result})
		},
	}
	opts.setupWithTime(cmd.Flags())
	return cmd
}

func newDescribeScopesCmd(loader RESTConfigLoader) *cobra.Command {
	opts := &describeOpts{}
	cmd := &cobra.Command{
		Use:   "scopes",
		Short: "Show all valid env/namespace/site filter values.",
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
			startMs, endMs, err := opts.Time.resolveTime()
			if err != nil {
				return err
			}
			scopes, err := client.ListEntityScopes(cmd.Context(), startMs, endMs)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), KGMetadataOutput{Scopes: scopes})
		},
	}
	opts.setupWithTime(cmd.Flags())
	return cmd
}

func newDescribeLogsCmd(loader RESTConfigLoader) *cobra.Command {
	opts := &describeOpts{}
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show Loki label mappings for log drilldown.",
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
			logResp, err := client.FetchLogConfigs(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), KGMetadataOutput{Logs: logResp.LogDrilldownConfigs})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newDescribeTracesCmd(loader RESTConfigLoader) *cobra.Command {
	opts := &describeOpts{}
	cmd := &cobra.Command{
		Use:   "traces",
		Short: "Show Tempo label mappings for trace drilldown.",
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
			traceResp, err := client.FetchTraceConfigs(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), KGMetadataOutput{Traces: traceResp.TraceDrilldownConfigs})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newDescribeProfilesCmd(loader RESTConfigLoader) *cobra.Command {
	opts := &describeOpts{}
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "Show Pyroscope label mappings for profile drilldown.",
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
			profileResp, err := client.FetchProfileConfigs(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), KGMetadataOutput{Profiles: profileResp.ProfileDrilldownConfigs})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newDescribeAllCmd(loader RESTConfigLoader) *cobra.Command {
	opts := &describeOpts{}
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Load all sections: schema, scopes, logs, traces, profiles, and metrics.",
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
			startMs, endMs, err := opts.Time.resolveTime()
			if err != nil {
				return err
			}
			var (
				out     KGMetadataOutput
				mu      sync.Mutex
				g, gCtx = errgroup.WithContext(cmd.Context())
			)
			g.Go(func() error {
				schemaResp, schemaErr := client.FetchGraphSchema(gCtx, startMs, endMs)
				if schemaErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: schema failed to load: %v\n", schemaErr)
					return nil
				}
				result := processGraphSchema(schemaResp)
				mu.Lock()
				out.Schema = &result
				mu.Unlock()
				return nil
			})
			g.Go(func() error {
				scopes, scopeErr := client.ListEntityScopes(gCtx, startMs, endMs)
				if scopeErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: scope values failed to load: %v\n", scopeErr)
					return nil
				}
				mu.Lock()
				out.Scopes = scopes
				mu.Unlock()
				return nil
			})
			g.Go(func() error {
				logResp, logErr := client.FetchLogConfigs(gCtx)
				if logErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: log configs failed to load: %v\n", logErr)
					return nil
				}
				mu.Lock()
				out.Logs = logResp.LogDrilldownConfigs
				mu.Unlock()
				return nil
			})
			g.Go(func() error {
				traceResp, traceErr := client.FetchTraceConfigs(gCtx)
				if traceErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: trace configs failed to load: %v\n", traceErr)
					return nil
				}
				mu.Lock()
				out.Traces = traceResp.TraceDrilldownConfigs
				mu.Unlock()
				return nil
			})
			g.Go(func() error {
				profileResp, profileErr := client.FetchProfileConfigs(gCtx)
				if profileErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: profile configs failed to load: %v\n", profileErr)
					return nil
				}
				mu.Lock()
				out.Profiles = profileResp.ProfileDrilldownConfigs
				mu.Unlock()
				return nil
			})
			if err := g.Wait(); err != nil {
				return err
			}
			// Metric guide is locally-defined
			// so it's set directly rather than via an errgroup fetch.
			guide := DefaultAssertsMetricGuide()
			out.Metrics = &guide
			return opts.IO.Encode(cmd.OutOrStdout(), out)
		},
	}
	opts.setupWithTime(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// Unused import guard for pflag
// ---------------------------------------------------------------------------

var _ = (*pflag.FlagSet)(nil)
