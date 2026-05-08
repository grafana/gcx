package rmw

import (
	"fmt"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/providers/instrumentation"
)

// AppEqual reports whether a and b represent the same namespace configuration.
// Apps[] is compared order-independently (sorted by Name for canonical comparison).
// Returns (true, "") when equal; (false, diffSummary) when not.
// Pure function — does not modify a or b.
func AppEqual(a, b instrumentation.App) (bool, string) {
	var diffs []string

	if a.Name != b.Name {
		diffs = append(diffs, fmt.Sprintf("name: %q → %q", a.Name, b.Name))
	}
	if d := boolPtrDiff("autoinstrument", a.Autoinstrument, b.Autoinstrument); d != "" {
		diffs = append(diffs, d)
	}
	if d := boolPtrDiff("tracing", a.Tracing, b.Tracing); d != "" {
		diffs = append(diffs, d)
	}
	if d := boolPtrDiff("logging", a.Logging, b.Logging); d != "" {
		diffs = append(diffs, d)
	}
	if d := boolPtrDiff("processmetrics", a.ProcessMetrics, b.ProcessMetrics); d != "" {
		diffs = append(diffs, d)
	}
	if d := boolPtrDiff("extendedmetrics", a.ExtendedMetrics, b.ExtendedMetrics); d != "" {
		diffs = append(diffs, d)
	}
	if d := boolPtrDiff("profiling", a.Profiling, b.Profiling); d != "" {
		diffs = append(diffs, d)
	}
	if d := appOverridesDiff(a.Apps, b.Apps); d != "" {
		diffs = append(diffs, d)
	}

	if len(diffs) == 0 {
		return true, ""
	}
	return false, strings.Join(diffs, ", ")
}

// ClusterEqual reports whether a and b represent the same cluster configuration.
// Returns (true, "") when equal; (false, diffSummary) when not.
func ClusterEqual(a, b instrumentation.Cluster) (bool, string) {
	var diffs []string

	if a.Name != b.Name {
		diffs = append(diffs, fmt.Sprintf("name: %q → %q", a.Name, b.Name))
	}
	if a.Selection != b.Selection {
		diffs = append(diffs, fmt.Sprintf("selection: %q → %q", a.Selection, b.Selection))
	}
	if d := boolPtrDiff("costmetrics", a.CostMetrics, b.CostMetrics); d != "" {
		diffs = append(diffs, d)
	}
	if d := boolPtrDiff("energymetrics", a.EnergyMetrics, b.EnergyMetrics); d != "" {
		diffs = append(diffs, d)
	}
	if d := boolPtrDiff("clusterevents", a.ClusterEvents, b.ClusterEvents); d != "" {
		diffs = append(diffs, d)
	}
	if d := boolPtrDiff("nodelogs", a.NodeLogs, b.NodeLogs); d != "" {
		diffs = append(diffs, d)
	}

	if len(diffs) == 0 {
		return true, ""
	}
	return false, strings.Join(diffs, ", ")
}

// boolPtrDiff returns a diff string for a named *bool field, or "" if equal.
// Treats nil ≠ false ≠ true (tri-state semantics).
func boolPtrDiff(name string, a, b *bool) string {
	if a == nil && b == nil {
		return ""
	}
	if a == nil {
		return fmt.Sprintf("%s: nil → %v", name, *b)
	}
	if b == nil {
		return fmt.Sprintf("%s: %v → nil", name, *a)
	}
	if *a != *b {
		return fmt.Sprintf("%s: %v → %v", name, *a, *b)
	}
	return ""
}

// appOverridesDiff returns a diff string for AppOverride slices, or "" if equal.
// Comparison is order-independent (by Name). Does not modify the caller's slices.
func appOverridesDiff(a, b []instrumentation.AppOverride) string {
	aCopy := make([]instrumentation.AppOverride, len(a))
	bCopy := make([]instrumentation.AppOverride, len(b))
	copy(aCopy, a)
	copy(bCopy, b)

	sort.Slice(aCopy, func(i, j int) bool { return aCopy[i].Name < aCopy[j].Name })
	sort.Slice(bCopy, func(i, j int) bool { return bCopy[i].Name < bCopy[j].Name })

	aIdx := make(map[string]string, len(aCopy))
	for _, o := range aCopy {
		aIdx[o.Name] = o.Selection
	}
	bIdx := make(map[string]string, len(bCopy))
	for _, o := range bCopy {
		bIdx[o.Name] = o.Selection
	}

	var diffs []string

	// Removed or changed overrides (present in a, absent or changed in b).
	for _, o := range aCopy {
		if sel, ok := bIdx[o.Name]; !ok {
			diffs = append(diffs, "removed apps[]: "+o.Name)
		} else if sel != o.Selection {
			diffs = append(diffs, fmt.Sprintf("apps[%s].selection: %s → %s", o.Name, o.Selection, sel))
		}
	}

	// Added overrides (present in b, absent in a).
	for _, o := range bCopy {
		if _, ok := aIdx[o.Name]; !ok {
			diffs = append(diffs, "added apps[]: "+o.Name)
		}
	}

	if len(diffs) == 0 {
		return ""
	}
	return strings.Join(diffs, ", ")
}
