package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/rmw"
)

// validateWorkloadExists checks whether the given (cluster, namespace, service)
// tuple appears in RunK8sDiscovery results. Returns a gcxerrors.DetailedError if not
// found, nil if found.
func validateWorkloadExists(
	ctx context.Context,
	client *instrumentation.Client,
	cluster, namespace, service string,
) error {
	resp, err := client.RunK8sDiscovery(ctx)
	if err != nil {
		return fmt.Errorf("workload validation: %w", err)
	}
	for _, item := range resp.Items {
		if item.ClusterName == cluster && item.Namespace == namespace && item.Name == service {
			return nil
		}
	}
	exitCode := gcxerrors.ExitGeneralError
	return &gcxerrors.DetailedError{
		Summary: "Resource not found",
		Details: fmt.Sprintf("workload %q not found in namespace %q (cluster %q) via RunK8sDiscovery", service, namespace, cluster),
		Suggestions: []string{
			fmt.Sprintf("Run: gcx instrumentation services list --cluster=%s --namespace=%s", cluster, namespace),
		},
		ExitCode: &exitCode,
	}
}

const (
	selectionIncluded = "SELECTION_INCLUDED"
	selectionExcluded = "SELECTION_EXCLUDED"
)

// normalizeStatus maps short alias status names to their full proto-enum values.
// "ERROR" → StatusError ("INSTRUMENTATION_ERROR"); full proto enum values pass through.
// Input is trimmed and uppercased for case-insensitive matching.
func normalizeStatus(s string) instrumentation.InstrumentationStatus {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ERROR":
		return instrumentation.StatusError
	case "INSTRUMENTED":
		return instrumentation.StatusInstrumented
	case "PENDING_INSTRUMENTATION":
		return instrumentation.StatusPendingInstrumentation
	case "PENDING_UNINSTRUMENTATION":
		return instrumentation.StatusPendingUninstrumentation
	case "NOT_INSTRUMENTED":
		return instrumentation.StatusNotInstrumented
	case "EXCLUDED":
		return instrumentation.StatusExcluded
	default:
		// Pass through as-is (callers may provide the full proto enum value).
		return instrumentation.InstrumentationStatus(strings.ToUpper(strings.TrimSpace(s)))
	}
}

// findNamespace returns a pointer to the App entry with the given name, or nil.
func findNamespace(apps []instrumentation.App, name string) *instrumentation.App {
	for i := range apps {
		if apps[i].Name == name {
			return &apps[i]
		}
	}
	return nil
}

// copyApp returns a shallow copy of app with a deep-copied Apps slice.
// AppOverride contains only string fields, so copying the slice is sufficient.
func copyApp(app instrumentation.App) instrumentation.App {
	result := app
	if app.Apps != nil {
		result.Apps = make([]instrumentation.AppOverride, len(app.Apps))
		copy(result.Apps, app.Apps)
	}
	return result
}

// filterOverrides returns a copy of overrides excluding any entry with the given name
// and the given selection. If selection is "", all entries with the name are removed.
func filterOverrides(overrides []instrumentation.AppOverride, name, selection string) []instrumentation.AppOverride {
	result := make([]instrumentation.AppOverride, 0, len(overrides))
	for _, o := range overrides {
		if o.Name == name && (selection == "" || o.Selection == selection) {
			continue
		}
		result = append(result, o)
	}
	return result
}

// hasOverride reports whether overrides contains an entry with the given name and selection.
func hasOverride(overrides []instrumentation.AppOverride, name, selection string) bool {
	for _, o := range overrides {
		if o.Name == name && o.Selection == selection {
			return true
		}
	}
	return false
}

// applyIncludeMutation returns a copy of ns with the include DWIM mutation applied for service.
//
// DWIM semantics:
//   - Remove any existing EXCLUDED override for service.
//   - Add INCLUDED override iff namespace autoinstrument is NOT explicitly true
//     (i.e., nil or false — the namespace default is off, so explicit opt-in is needed).
//   - If autoinstrument is true, no override is added (namespace default is already on).
func applyIncludeMutation(ns instrumentation.App, service string) instrumentation.App {
	result := copyApp(ns)
	// Remove any existing EXCLUDED override.
	result.Apps = filterOverrides(result.Apps, service, selectionExcluded)
	// Add INCLUDED override iff namespace autoinstrument is not explicitly true.
	if ns.Autoinstrument == nil || !*ns.Autoinstrument {
		if !hasOverride(result.Apps, service, selectionIncluded) {
			result.Apps = append(result.Apps, instrumentation.AppOverride{
				Name:      service,
				Selection: selectionIncluded,
			})
		}
	}
	return result
}

// applyExcludeMutation returns a copy of ns with the exclude DWIM mutation applied for service.
//
// DWIM semantics:
//   - Remove any existing INCLUDED override for service.
//   - Add EXCLUDED override iff namespace autoinstrument is explicitly true
//     (the namespace default is on, so explicit opt-out is needed).
//   - If autoinstrument is false/nil, no override is added (namespace default is already off).
func applyExcludeMutation(ns instrumentation.App, service string) instrumentation.App {
	result := copyApp(ns)
	// Remove any existing INCLUDED override.
	result.Apps = filterOverrides(result.Apps, service, selectionIncluded)
	// Add EXCLUDED override iff namespace autoinstrument is explicitly true.
	if ns.Autoinstrument != nil && *ns.Autoinstrument {
		if !hasOverride(result.Apps, service, selectionExcluded) {
			result.Apps = append(result.Apps, instrumentation.AppOverride{
				Name:      service,
				Selection: selectionExcluded,
			})
		}
	}
	return result
}

// applyClearMutation returns a copy of ns with any per-workload override for service removed.
// Both INCLUDED and EXCLUDED overrides are removed, restoring namespace-default behavior.
func applyClearMutation(ns instrumentation.App, service string) instrumentation.App {
	result := copyApp(ns)
	result.Apps = filterOverrides(result.Apps, service, "")
	return result
}

// applyMutationToNamespaces applies the mutate function to the namespace entry with the given
// name and returns a new []App. If the namespace is not found the original slice is returned.
func applyMutationToNamespaces(namespaces []instrumentation.App, namespace string, mutate func(instrumentation.App) instrumentation.App) []instrumentation.App {
	result := make([]instrumentation.App, len(namespaces))
	copy(result, namespaces)
	for i, ns := range result {
		if ns.Name == namespace {
			result[i] = mutate(ns)
			return result
		}
	}
	return result
}

// namespacesEqual compares two []App slices order-independently (matched by Name).
// Returns (true, "") when equal; (false, diffSummary) when not.
// Used as the equalsFn for rmw.Update.
func namespacesEqual(a, b []instrumentation.App) (bool, string) {
	aMap := make(map[string]instrumentation.App, len(a))
	for _, app := range a {
		aMap[app.Name] = app
	}
	bMap := make(map[string]instrumentation.App, len(b))
	for _, app := range b {
		bMap[app.Name] = app
	}

	var diffs []string

	for name, appA := range aMap {
		appB, ok := bMap[name]
		if !ok {
			diffs = append(diffs, "removed namespace: "+name)
			continue
		}
		equal, diff := rmw.AppEqual(appA, appB)
		if !equal {
			diffs = append(diffs, diff)
		}
	}
	for name := range bMap {
		if _, ok := aMap[name]; !ok {
			diffs = append(diffs, "added namespace: "+name)
		}
	}

	if len(diffs) == 0 {
		return true, ""
	}
	sort.Strings(diffs)
	return false, strings.Join(diffs, ", ")
}
