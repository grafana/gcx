package datasources

import (
	"fmt"
	"sort"
	"strings"
)

// ChangeSummary describes the field-level differences between two manifests,
// computed for `--dry-run`. Secret values are never recorded — only the change
// kind per secure key.
type ChangeSummary struct {
	SpecChanges   []FieldChange
	SecretChanges []FieldChange
}

// FieldChange is a single added / changed / cleared field.
type FieldChange struct {
	Field string
	Kind  string // "added" | "changed" | "cleared" | "set"
}

// Empty reports whether there are no detected changes.
func (s ChangeSummary) Empty() bool {
	return len(s.SpecChanges) == 0 && len(s.SecretChanges) == 0
}

// Render returns a human-readable, secret-safe summary of the changes.
func (s ChangeSummary) Render() string {
	if s.Empty() {
		return "No changes."
	}
	var b strings.Builder
	if len(s.SpecChanges) > 0 {
		b.WriteString("spec:\n")
		for _, c := range s.SpecChanges {
			fmt.Fprintf(&b, "  %s: %s\n", c.Field, c.Kind)
		}
	}
	if len(s.SecretChanges) > 0 {
		b.WriteString("secure:\n")
		for _, c := range s.SecretChanges {
			fmt.Fprintf(&b, "  %s: %s\n", c.Field, c.Kind)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// DiffManifest computes the change summary of applying next over current.
// current may be nil (e.g. a create), in which case all populated fields are
// reported as added. Secret values are reported only as set / changed /
// cleared, never disclosed.
func DiffManifest(current, next *DataSourceManifest) ChangeSummary {
	var summary ChangeSummary

	var currentSpec map[string]any
	if current != nil {
		currentSpec, _ = specToMap(current.Spec)
	}
	nextSpec, _ := specToMap(next.Spec)

	summary.SpecChanges = diffMaps(currentSpec, nextSpec)
	summary.SecretChanges = diffSecrets(secretKeys(current), next.Secure)
	return summary
}

func diffMaps(current, next map[string]any) []FieldChange {
	changes := make([]FieldChange, 0)
	seen := make(map[string]bool)

	for k, nv := range next {
		seen[k] = true
		cv, ok := current[k]
		switch {
		case !ok:
			changes = append(changes, FieldChange{Field: k, Kind: "added"})
		case fmt.Sprintf("%v", cv) != fmt.Sprintf("%v", nv):
			changes = append(changes, FieldChange{Field: k, Kind: "changed"})
		}
	}
	for k := range current {
		if !seen[k] {
			changes = append(changes, FieldChange{Field: k, Kind: "cleared"})
		}
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Field < changes[j].Field })
	return changes
}

func secretKeys(m *DataSourceManifest) map[string]bool {
	keys := map[string]bool{}
	if m == nil {
		return keys
	}
	for k := range m.Secure {
		keys[k] = true
	}
	return keys
}

func diffSecrets(current map[string]bool, next map[string]SecureValue) []FieldChange {
	changes := make([]FieldChange, 0)
	for k, sv := range next {
		switch {
		case sv.Remove:
			if current[k] {
				changes = append(changes, FieldChange{Field: k, Kind: "cleared"})
			}
		case current[k]:
			changes = append(changes, FieldChange{Field: k, Kind: "changed"})
		default:
			changes = append(changes, FieldChange{Field: k, Kind: "set"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Field < changes[j].Field })
	return changes
}
