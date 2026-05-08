package kg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// ---------------------------------------------------------------------------
// Check result types
// ---------------------------------------------------------------------------

// CheckStatus is the outcome of a single diagnostic check.
type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckFail CheckStatus = "fail"
	CheckWarn CheckStatus = "warn"
	CheckSkip CheckStatus = "skip"
)

// CheckResult is a single diagnostic check outcome.
type CheckResult struct {
	Name           string      `json:"name"`
	Status         CheckStatus `json:"status"`
	Detail         string      `json:"detail,omitempty"`
	Recommendation string      `json:"recommendation,omitempty"`
}

// DiagnoseResult is the full output of the diagnose command.
type DiagnoseResult struct {
	Env     string        `json:"env,omitempty"`
	Checks  []CheckResult `json:"checks"`
	Summary struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
		Warned int `json:"warned"`
	} `json:"summary"`
}

func (r *DiagnoseResult) computeSummary() {
	r.Summary.Total = len(r.Checks)
	for _, c := range r.Checks {
		switch c.Status {
		case CheckPass:
			r.Summary.Passed++
		case CheckFail:
			r.Summary.Failed++
		case CheckWarn:
			r.Summary.Warned++
		}
	}
}

// ---------------------------------------------------------------------------
// Command wiring
// ---------------------------------------------------------------------------

type diagnoseOpts struct {
	IO    cmdio.Options
	Scope scopeFlags
}

func (o *diagnoseOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &DiagnoseTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	o.Scope.register(&cobra.Command{}) // register creates flag vars; we re-bind below
}

func newDiagnoseCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &diagnoseOpts{}
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Run diagnostic checks on the Knowledge Graph pipeline.",
		Long: `Run diagnostic checks to verify the Knowledge Graph is healthy.

Checks stack status, sanity results, entity counts, scope values,
and telemetry drilldown configuration. Use --env to scope checks
to a specific environment.`,
		Example: `  gcx kg diagnose
  gcx kg diagnose --env production
  gcx kg diagnose --env staging --output json`,
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
			result := runDiagnose(cmd.Context(), client, &opts.Scope)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	// Bind scope flags directly to this command.
	cmd.Flags().StringVar(&opts.Scope.env, "env", "", "Environment scope")
	cmd.Flags().StringVar(&opts.Scope.namespace, "namespace", "", "Namespace scope")
	cmd.Flags().StringVar(&opts.Scope.site, "site", "", "Site scope")

	// IO flags (--output).
	opts.IO.RegisterCustomCodec("text", &DiagnoseTextCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(cmd.Flags())

	return cmd
}

// ---------------------------------------------------------------------------
// Check runner
// ---------------------------------------------------------------------------

func runDiagnose(ctx context.Context, client *Client, scope *scopeFlags) DiagnoseResult {
	result := DiagnoseResult{Env: scope.env}

	var (
		mu sync.Mutex
		g  errgroup.Group
	)
	addCheck := func(c CheckResult) {
		mu.Lock()
		result.Checks = append(result.Checks, c)
		mu.Unlock()
	}

	// Check 1: Stack status + sanity checks.
	g.Go(func() error {
		checks := checkStackStatus(ctx, client)
		mu.Lock()
		result.Checks = append(result.Checks, checks...)
		mu.Unlock()
		return nil
	})

	// Check 2: Entity counts.
	g.Go(func() error {
		addCheck(checkEntityCounts(ctx, client))
		return nil
	})

	// Check 3: Scope values.
	g.Go(func() error {
		addCheck(checkScopeValues(ctx, client))
		return nil
	})

	// Check 4: Telemetry configs.
	g.Go(func() error {
		checks := checkTelemetryConfigs(ctx, client)
		mu.Lock()
		result.Checks = append(result.Checks, checks...)
		mu.Unlock()
		return nil
	})

	_ = g.Wait() // errors are captured in CheckResults, not returned

	// Stable output order.
	sort.Slice(result.Checks, func(i, j int) bool {
		return checkOrder(result.Checks[i].Name) < checkOrder(result.Checks[j].Name)
	})

	result.computeSummary()
	return result
}

// checkOrder returns a sort key for deterministic output ordering.
func checkOrder(name string) int {
	order := map[string]int{
		"Stack status":       1,
		"Telemetry: log":     10,
		"Telemetry: trace":   11,
		"Telemetry: profile": 12,
	}
	if v, ok := order[name]; ok {
		return v
	}
	// Sanity checks sort between stack status and entity counts.
	if strings.HasPrefix(name, "Sanity:") {
		return 2
	}
	if name == "Entity counts" {
		return 5
	}
	if name == "Scope values" {
		return 6
	}
	return 50
}

// ---------------------------------------------------------------------------
// Individual checks
// ---------------------------------------------------------------------------

func checkStackStatus(ctx context.Context, client *Client) []CheckResult {
	status, err := client.GetStatus(ctx)
	if err != nil {
		return []CheckResult{{
			Name:           "Stack status",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Verify the Grafana instance is reachable and the Asserts plugin is installed.",
		}}
	}

	var results []CheckResult

	// Main status check.
	if status.Enabled && status.Status == "complete" {
		results = append(results, CheckResult{
			Name:   "Stack status",
			Status: CheckPass,
			Detail: fmt.Sprintf("status=%s, enabled=%t", status.Status, status.Enabled),
		})
	} else {
		results = append(results, CheckResult{
			Name:           "Stack status",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("status=%s, enabled=%t", status.Status, status.Enabled),
			Recommendation: "The Knowledge Graph is not fully active. Check onboarding status in the Asserts app.",
		})
	}

	// Sanity check results from the status response.
	for _, sc := range status.SanityCheckResults {
		c := CheckResult{
			Name: fmt.Sprintf("Sanity: %s", sc.CheckName),
		}
		if sc.DataPresent {
			c.Status = CheckPass
			c.Detail = "data present"
		} else {
			c.Status = CheckFail
			c.Detail = "no data"
			c.Recommendation = "This metric sanity check found no data. Verify telemetry is flowing to Mimir."
		}
		// Surface step-level blockers/warnings.
		for _, step := range sc.StepResults {
			if len(step.Blockers) > 0 {
				c.Status = CheckFail
				c.Detail += fmt.Sprintf("; blocker in %q: %s", step.Name, strings.Join(step.Blockers, ", "))
				if step.Troubleshoot != "" {
					c.Recommendation = step.Troubleshoot
				}
			}
			if len(step.Warnings) > 0 {
				if c.Status == CheckPass {
					c.Status = CheckWarn
				}
				c.Detail += fmt.Sprintf("; warning in %q: %s", step.Name, strings.Join(step.Warnings, ", "))
			}
		}
		results = append(results, c)
	}

	return results
}

func checkEntityCounts(ctx context.Context, client *Client) CheckResult {
	counts, err := client.CountEntityTypes(ctx)
	if err != nil {
		return CheckResult{
			Name:           "Entity counts",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Failed to retrieve entity counts. Check connectivity to the Asserts API.",
		}
	}

	if len(counts) == 0 {
		return CheckResult{
			Name:           "Entity counts",
			Status:         CheckFail,
			Detail:         "no entity types found",
			Recommendation: "No entities discovered. Verify that traces_target_info or asserts:mixin_workload_job metrics are being produced.",
		}
	}

	var total int64
	var parts []string
	// Sort by type name for stable output.
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		cnt := counts[t]
		total += cnt
		parts = append(parts, fmt.Sprintf("%s: %d", t, cnt))
	}

	if total == 0 {
		return CheckResult{
			Name:           "Entity counts",
			Status:         CheckFail,
			Detail:         "all entity type counts are 0",
			Recommendation: "Entity types exist but have no instances. Check that recording rules are producing data and the graph ingestion pipeline is running.",
		}
	}

	return CheckResult{
		Name:   "Entity counts",
		Status: CheckPass,
		Detail: fmt.Sprintf("%d total (%s)", total, strings.Join(parts, ", ")),
	}
}

func checkScopeValues(ctx context.Context, client *Client) CheckResult {
	scopes, err := client.ListEntityScopes(ctx)
	if err != nil {
		return CheckResult{
			Name:           "Scope values",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Failed to retrieve scope values. Check connectivity to the Asserts API.",
		}
	}

	if len(scopes) == 0 {
		return CheckResult{
			Name:           "Scope values",
			Status:         CheckWarn,
			Detail:         "no scope dimensions returned",
			Recommendation: "No env/site/namespace values found. Entities may exist without scope labels.",
		}
	}

	var parts []string
	for _, dim := range []string{"env", "site", "namespace"} {
		if vals, ok := scopes[dim]; ok && len(vals) > 0 {
			parts = append(parts, fmt.Sprintf("%s: [%s]", dim, strings.Join(vals, ", ")))
		}
	}

	if len(parts) == 0 {
		return CheckResult{
			Name:           "Scope values",
			Status:         CheckWarn,
			Detail:         "scope dimensions present but env/site/namespace are empty",
			Recommendation: "The asserts_env label may not be set. Verify that deployment_environment is configured in your OTel SDK and that Mimir relabeling rules map it to asserts_env.",
		}
	}

	return CheckResult{
		Name:   "Scope values",
		Status: CheckPass,
		Detail: strings.Join(parts, "; "),
	}
}

func checkTelemetryConfigs(ctx context.Context, client *Client) []CheckResult {
	var (
		results []CheckResult
		mu      sync.Mutex
		g       errgroup.Group
	)

	g.Go(func() error {
		resp, err := client.FetchLogConfigs(ctx)
		c := CheckResult{Name: "Telemetry: log"}
		if err != nil {
			c.Status = CheckWarn
			c.Detail = fmt.Sprintf("API error: %v", err)
			c.Recommendation = "Could not fetch log drilldown configs. Log drilldown from entities may not work."
		} else if len(resp.LogDrilldownConfigs) == 0 {
			c.Status = CheckWarn
			c.Detail = "no log drilldown configs"
			c.Recommendation = "No log configs found. Configure a Loki datasource mapping in the Asserts app to enable log drilldown."
		} else {
			c.Status = CheckPass
			names := make([]string, 0, len(resp.LogDrilldownConfigs))
			for _, cfg := range resp.LogDrilldownConfigs {
				names = append(names, cfg.Name)
			}
			c.Detail = fmt.Sprintf("%d config(s): %s", len(resp.LogDrilldownConfigs), strings.Join(names, ", "))
		}
		mu.Lock()
		results = append(results, c)
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		resp, err := client.FetchTraceConfigs(ctx)
		c := CheckResult{Name: "Telemetry: trace"}
		if err != nil {
			c.Status = CheckWarn
			c.Detail = fmt.Sprintf("API error: %v", err)
			c.Recommendation = "Could not fetch trace drilldown configs. Trace drilldown from entities may not work."
		} else if len(resp.TraceDrilldownConfigs) == 0 {
			c.Status = CheckWarn
			c.Detail = "no trace drilldown configs"
			c.Recommendation = "No trace configs found. Configure a Tempo datasource mapping in the Asserts app to enable trace drilldown."
		} else {
			c.Status = CheckPass
			names := make([]string, 0, len(resp.TraceDrilldownConfigs))
			for _, cfg := range resp.TraceDrilldownConfigs {
				names = append(names, cfg.Name)
			}
			c.Detail = fmt.Sprintf("%d config(s): %s", len(resp.TraceDrilldownConfigs), strings.Join(names, ", "))
		}
		mu.Lock()
		results = append(results, c)
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		resp, err := client.FetchProfileConfigs(ctx)
		c := CheckResult{Name: "Telemetry: profile"}
		if err != nil {
			c.Status = CheckWarn
			c.Detail = fmt.Sprintf("API error: %v", err)
			c.Recommendation = "Could not fetch profile drilldown configs."
		} else if len(resp.ProfileDrilldownConfigs) == 0 {
			c.Status = CheckWarn
			c.Detail = "no profile drilldown configs"
			c.Recommendation = "No profile configs found. This is optional — configure Pyroscope if continuous profiling is available."
		} else {
			c.Status = CheckPass
			names := make([]string, 0, len(resp.ProfileDrilldownConfigs))
			for _, cfg := range resp.ProfileDrilldownConfigs {
				names = append(names, cfg.Name)
			}
			c.Detail = fmt.Sprintf("%d config(s): %s", len(resp.ProfileDrilldownConfigs), strings.Join(names, ", "))
		}
		mu.Lock()
		results = append(results, c)
		mu.Unlock()
		return nil
	})

	_ = g.Wait()
	return results
}

// ---------------------------------------------------------------------------
// Text codec for human-readable output
// ---------------------------------------------------------------------------

// DiagnoseTextCodec renders DiagnoseResult as a human-readable table.
type DiagnoseTextCodec struct{}

func (c *DiagnoseTextCodec) Format() format.Format { return "text" }

func (c *DiagnoseTextCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(DiagnoseResult)
	if !ok {
		return errors.New("invalid data type for text codec: expected DiagnoseResult")
	}

	header := "Knowledge Graph Diagnostics"
	if result.Env != "" {
		header += fmt.Sprintf(" — env: %s", result.Env)
	}
	fmt.Fprintln(w, header)
	fmt.Fprintln(w)

	// Find max name width for alignment.
	maxName := 0
	for _, c := range result.Checks {
		if len(c.Name) > maxName {
			maxName = len(c.Name)
		}
	}

	for _, check := range result.Checks {
		icon := statusIcon(check.Status)
		status := strings.ToUpper(string(check.Status))
		fmt.Fprintf(w, "  %-*s  %s %-4s  %s\n", maxName, check.Name, icon, status, check.Detail)
		if check.Recommendation != "" && check.Status != CheckPass {
			fmt.Fprintf(w, "  %-*s         %s\n", maxName, "", check.Recommendation)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d/%d checks passed", result.Summary.Passed, result.Summary.Total)
	if result.Summary.Failed > 0 {
		fmt.Fprintf(w, ", %d failed", result.Summary.Failed)
	}
	if result.Summary.Warned > 0 {
		fmt.Fprintf(w, ", %d warning(s)", result.Summary.Warned)
	}
	fmt.Fprintln(w)

	return nil
}

func (c *DiagnoseTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

func statusIcon(s CheckStatus) string {
	switch s {
	case CheckPass:
		return "✓"
	case CheckFail:
		return "✗"
	case CheckWarn:
		return "!"
	case CheckSkip:
		return "-"
	default:
		return "?"
	}
}
