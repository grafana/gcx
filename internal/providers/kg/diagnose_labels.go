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
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"golang.org/x/sync/errgroup"
)

// ---------------------------------------------------------------------------
// Labels diagnosis result types
// ---------------------------------------------------------------------------

// LabelMapping represents a single deployment_environment → asserts_env mapping.
type LabelMapping struct {
	DeploymentEnv string `json:"deploymentEnvironment"`
	AssertsEnv    string `json:"assertsEnv,omitempty"`
	Status        string `json:"status"` // "mapped", "unmapped", "orphaned"
	EntityCount   int64  `json:"entityCount,omitempty"`
}

// LabelsDiagnoseResult is the full output of the diagnose labels command.
type LabelsDiagnoseResult struct {
	Mappings  []LabelMapping `json:"mappings"`
	Checks    []CheckResult  `json:"checks"`
	Diagnosis []string       `json:"diagnosis,omitempty"`
	Summary   struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
		Warned int `json:"warned"`
	} `json:"summary"`
}

func (r *LabelsDiagnoseResult) computeSummary() {
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
// Labels diagnosis runner
// ---------------------------------------------------------------------------

func runLabelsDiagnose(ctx context.Context, client *Client, promClient *prometheus.Client, datasourceUID string) LabelsDiagnoseResult {
	var result LabelsDiagnoseResult

	if promClient == nil || datasourceUID == "" {
		result.Checks = append(result.Checks, CheckResult{
			Name:           "Prometheus connectivity",
			Status:         CheckFail,
			Detail:         "no Prometheus datasource available",
			Recommendation: "Label diagnosis requires a Prometheus datasource. Use --datasource or configure datasources.prometheus in your gcx config.",
		})
		result.computeSummary()
		return result
	}

	var (
		mu             sync.Mutex
		g              errgroup.Group
		assertsEnvs    []string
		deploymentEnvs []string
		scopeEnvs      []string
		assertsErr     error
		deploymentErr  error
		scopeErr       error
	)

	// Fetch data sources in parallel.
	g.Go(func() error {
		vals, err := queryLabelValues(ctx, promClient, datasourceUID, `group by (asserts_env) (asserts:mixin_workload_job)`, "asserts_env")
		mu.Lock()
		assertsEnvs = vals
		assertsErr = err
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		vals, err := queryLabelValues(ctx, promClient, datasourceUID, `group by (deployment_environment) (traces_target_info)`, "deployment_environment")
		mu.Lock()
		deploymentEnvs = vals
		deploymentErr = err
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		scopes, err := client.ListEntityScopes(ctx, 0, 0)
		mu.Lock()
		if err == nil {
			scopeEnvs = scopes["env"]
		}
		scopeErr = err
		mu.Unlock()
		return nil
	})

	_ = g.Wait()

	// Check 1: asserts_env values exist.
	switch {
	case assertsErr != nil:
		result.Checks = append(result.Checks, CheckResult{
			Name:           "asserts_env in recording rules",
			Status:         CheckWarn,
			Detail:         fmt.Sprintf("query error: %v", assertsErr),
			Recommendation: "Could not query asserts_env values from asserts:mixin_workload_job.",
		})
	case len(assertsEnvs) == 0:
		result.Checks = append(result.Checks, CheckResult{
			Name:           "asserts_env in recording rules",
			Status:         CheckFail,
			Detail:         "no asserts_env values found",
			Recommendation: "Recording rules are not producing any asserts_env labels. Verify that deployment_environment is set in your OTel config and that Mimir relabeling rules map it to asserts_env.",
		})
	default:
		result.Checks = append(result.Checks, CheckResult{
			Name:   "asserts_env in recording rules",
			Status: CheckPass,
			Detail: fmt.Sprintf("%d value(s): %s", len(assertsEnvs), strings.Join(assertsEnvs, ", ")),
		})
	}

	// Check 2: deployment_environment values exist.
	switch {
	case deploymentErr != nil:
		result.Checks = append(result.Checks, CheckResult{
			Name:           "deployment_environment in raw metrics",
			Status:         CheckWarn,
			Detail:         fmt.Sprintf("query error: %v", deploymentErr),
			Recommendation: "Could not query deployment_environment values from traces_target_info.",
		})
	case len(deploymentEnvs) == 0:
		result.Checks = append(result.Checks, CheckResult{
			Name:           "deployment_environment in raw metrics",
			Status:         CheckWarn,
			Detail:         "no deployment_environment values found in traces_target_info",
			Recommendation: "Services may not be setting deployment.environment in OTEL_RESOURCE_ATTRIBUTES.",
		})
	default:
		result.Checks = append(result.Checks, CheckResult{
			Name:   "deployment_environment in raw metrics",
			Status: CheckPass,
			Detail: fmt.Sprintf("%d value(s): %s", len(deploymentEnvs), strings.Join(deploymentEnvs, ", ")),
		})
	}

	// Check 3: Scope env values in the graph.
	switch {
	case scopeErr != nil:
		result.Checks = append(result.Checks, CheckResult{
			Name:   "env values in Entity Graph",
			Status: CheckWarn,
			Detail: fmt.Sprintf("API error: %v", scopeErr),
		})
	case len(scopeEnvs) == 0:
		result.Checks = append(result.Checks, CheckResult{
			Name:           "env values in Entity Graph",
			Status:         CheckFail,
			Detail:         "no env scope values in the graph",
			Recommendation: "No environments found in the Entity Graph. Entities may not have asserts_env labels.",
		})
	default:
		result.Checks = append(result.Checks, CheckResult{
			Name:   "env values in Entity Graph",
			Status: CheckPass,
			Detail: fmt.Sprintf("%d value(s): %s", len(scopeEnvs), strings.Join(scopeEnvs, ", ")),
		})
	}

	// Build mapping table.
	assertsSet := toSet(assertsEnvs)
	scopeSet := toSet(scopeEnvs)

	// Map deployment_environment → asserts_env.
	for _, de := range deploymentEnvs {
		m := LabelMapping{DeploymentEnv: de}
		if assertsSet[de] {
			m.AssertsEnv = de
			m.Status = "mapped"
		} else {
			// Check if it's in scope values (graph knows about it even if recording rules use a different name).
			if scopeSet[de] {
				m.AssertsEnv = de
				m.Status = "mapped"
			} else {
				m.Status = "unmapped"
			}
		}
		result.Mappings = append(result.Mappings, m)
	}

	// Find orphaned asserts_env values (in recording rules but no matching deployment_environment).
	deploymentSet := toSet(deploymentEnvs)
	for _, ae := range assertsEnvs {
		if !deploymentSet[ae] {
			result.Mappings = append(result.Mappings, LabelMapping{
				AssertsEnv: ae,
				Status:     "orphaned",
			})
		}
	}

	// Sort mappings for stable output.
	sort.Slice(result.Mappings, func(i, j int) bool {
		// unmapped first, then orphaned, then mapped.
		order := map[string]int{"unmapped": 0, "orphaned": 1, "mapped": 2}
		if order[result.Mappings[i].Status] != order[result.Mappings[j].Status] {
			return order[result.Mappings[i].Status] < order[result.Mappings[j].Status]
		}
		ki := result.Mappings[i].DeploymentEnv + result.Mappings[i].AssertsEnv
		kj := result.Mappings[j].DeploymentEnv + result.Mappings[j].AssertsEnv
		return ki < kj
	})

	// Check 4: Mapping consistency.
	unmapped := 0
	orphaned := 0
	for _, m := range result.Mappings {
		switch m.Status {
		case "unmapped":
			unmapped++
		case "orphaned":
			orphaned++
		}
	}

	switch {
	case unmapped > 0:
		result.Checks = append(result.Checks, CheckResult{
			Name:           "Label mapping consistency",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("%d deployment_environment value(s) with no corresponding asserts_env", unmapped),
			Recommendation: "These environments exist in raw traces_target_info but not in recording rule outputs. Check that Mimir relabeling rules are mapping deployment_environment to asserts_env for all environments.",
		})
	case orphaned > 0:
		result.Checks = append(result.Checks, CheckResult{
			Name:           "Label mapping consistency",
			Status:         CheckWarn,
			Detail:         fmt.Sprintf("all deployment_environment values mapped; %d asserts_env value(s) have no corresponding deployment_environment", orphaned),
			Recommendation: "Some asserts_env values don't match any deployment_environment in traces_target_info. These may come from non-OTel sources (Prometheus scrape, AWS CloudWatch, etc.).",
		})
	case len(deploymentEnvs) > 0:
		result.Checks = append(result.Checks, CheckResult{
			Name:   "Label mapping consistency",
			Status: CheckPass,
			Detail: "all deployment_environment values have a corresponding asserts_env",
		})
	}

	// Build diagnosis.
	result.Diagnosis = interpretLabelsResults(&result, unmapped, orphaned)

	result.computeSummary()
	return result
}

// queryLabelValues runs a PromQL group-by query and extracts the values of the
// specified label from the result samples.
func queryLabelValues(ctx context.Context, client *prometheus.Client, datasourceUID, query, labelName string) ([]string, error) {
	resp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{Query: query})
	if err != nil {
		return nil, err
	}

	var values []string
	seen := map[string]bool{}
	for _, s := range resp.Data.Result {
		if v, ok := s.Metric[labelName]; ok && v != "" && !seen[v] {
			seen[v] = true
			values = append(values, v)
		}
	}
	sort.Strings(values)
	return values, nil
}

func toSet(vals []string) map[string]bool {
	s := make(map[string]bool, len(vals))
	for _, v := range vals {
		s[v] = true
	}
	return s
}

func interpretLabelsResults(r *LabelsDiagnoseResult, unmapped, orphaned int) []string {
	var diagnosis []string

	if unmapped > 0 {
		var names []string
		for _, m := range r.Mappings {
			if m.Status == "unmapped" {
				names = append(names, m.DeploymentEnv)
			}
		}
		diagnosis = append(diagnosis,
			fmt.Sprintf("The following deployment_environment values have no corresponding asserts_env: %s. "+
				"Services in these environments will not appear in Entity Graph. "+
				"Check the Mimir relabeling rules for your stack.", strings.Join(names, ", ")))
	}

	if orphaned > 0 {
		var names []string
		for _, m := range r.Mappings {
			if m.Status == "orphaned" {
				names = append(names, m.AssertsEnv)
			}
		}
		diagnosis = append(diagnosis,
			fmt.Sprintf("The following asserts_env values exist in recording rules but not in traces_target_info: %s. "+
				"These may come from non-OTel sources (Prometheus scrape targets, AWS CloudWatch integrations, etc.).", strings.Join(names, ", ")))
	}

	if unmapped == 0 && orphaned == 0 && len(r.Mappings) > 0 {
		diagnosis = append(diagnosis, "All deployment_environment values are correctly mapped to asserts_env. The label pipeline looks healthy.")
	}

	return diagnosis
}

// ---------------------------------------------------------------------------
// Labels diagnosis text codec
// ---------------------------------------------------------------------------

// LabelsDiagnoseTableCodec renders LabelsDiagnoseResult as a human-readable table.
type LabelsDiagnoseTableCodec struct{}

func (c *LabelsDiagnoseTableCodec) Format() format.Format { return "table" }

func (c *LabelsDiagnoseTableCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(LabelsDiagnoseResult)
	if !ok {
		return errors.New("invalid data type for table codec: expected LabelsDiagnoseResult")
	}

	// Checks table.
	fmt.Fprintln(w, "Checks:")
	t := style.NewTable("CHECK", "STATUS", "DETAIL")
	for _, check := range r.Checks {
		t.Row(check.Name, strings.ToUpper(string(check.Status)), check.Detail)
	}
	_ = t.Render(w)

	// Recommendations.
	var recs []string
	for _, check := range r.Checks {
		if check.Recommendation != "" && check.Status != CheckPass {
			recs = append(recs, fmt.Sprintf("  %s: %s", check.Name, check.Recommendation))
		}
	}
	if len(recs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Recommendations:")
		for _, rec := range recs {
			fmt.Fprintln(w, rec)
		}
	}

	// Mapping table.
	if len(r.Mappings) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Label mapping (deployment_environment → asserts_env):")
		mt := style.NewTable("DEPLOYMENT_ENVIRONMENT", "ASSERTS_ENV", "STATUS")
		for _, m := range r.Mappings {
			switch m.Status {
			case "mapped":
				mt.Row(m.DeploymentEnv, m.AssertsEnv, "mapped")
			case "unmapped":
				mt.Row(m.DeploymentEnv, "(missing)", "not mapped — check relabeling rules")
			case "orphaned":
				mt.Row("(unknown source)", m.AssertsEnv, "orphaned (no deployment_environment source)")
			}
		}
		_ = mt.Render(w)
	}

	// Diagnosis.
	if len(r.Diagnosis) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Diagnosis:")
		for _, d := range r.Diagnosis {
			fmt.Fprintf(w, "  %s\n", d)
		}
	}

	// Summary.
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%d/%d checks passed", r.Summary.Passed, r.Summary.Total)
	if r.Summary.Failed > 0 {
		fmt.Fprintf(w, ", %d failed", r.Summary.Failed)
	}
	if r.Summary.Warned > 0 {
		fmt.Fprintf(w, ", %d warning(s)", r.Summary.Warned)
	}
	fmt.Fprintln(w)

	return nil
}

func (c *LabelsDiagnoseTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
