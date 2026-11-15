// Package observation captures selected resources from one Grafana stack at a
// point in time and compares two such captures offline.
//
// A capture is an observation over a collection interval, not an atomic
// snapshot. It records the stack identity, the selected scope, per-type
// coverage, and an inventory of normalized resource files. A comparison
// reports additions, modifications and deletions only where the coverage of
// both captures supports the conclusion; everything else is indeterminate.
//
// The normalization rules are versioned (NormalizationVersion). Captures with
// different schema or normalization versions are not comparable.
package observation

import "time"

const (
	// ManifestType discriminates the capture manifest document.
	ManifestType = "gcx.resources.capture"
	// DiffType discriminates the comparison result document.
	DiffType = "gcx.resources.diff"

	// SchemaVersion is the version of the manifest and diff document shapes.
	SchemaVersion = "1"
	// NormalizationVersion identifies the normalization rules in normalize.go.
	// Bump it whenever a rule changes what is kept or removed.
	NormalizationVersion = "1"

	// ManifestFile is the name of the manifest at the capture root. It is
	// written last, so a capture without it is unfinished.
	ManifestFile = "manifest.json"
)

// Coverage status values for one resource type in a capture.
const (
	// CoverageComplete means every item of the type was listed and read.
	CoverageComplete = "complete"
	// CoverageIncomplete means the read failed, was truncated, skipped items,
	// or the caller's read scope for the type is not "all".
	CoverageIncomplete = "incomplete"
	// CoverageUnavailable means the type could not be resolved on the stack.
	CoverageUnavailable = "unavailable"
)

// Change values for one resource in a comparison.
const (
	ChangeAdded         = "added"
	ChangeModified      = "modified"
	ChangeDeleted       = "deleted"
	ChangeIndeterminate = "indeterminate"
)

// StackIdentity identifies the stack a capture was taken from. Credentials are
// never recorded.
type StackIdentity struct {
	// Server is the Grafana server URL without user information.
	Server string `json:"server"`
	// Namespace is the Kubernetes namespace of the stack (org or stack ID).
	Namespace string `json:"namespace"`
	// Context is the gcx config context used for the capture. It is
	// informational and is not part of the comparison identity.
	Context string `json:"context,omitempty"`
}

// SameStack reports whether two identities refer to the same stack.
func (s StackIdentity) SameStack(other StackIdentity) bool {
	return s.Server == other.Server && s.Namespace == other.Namespace
}

// Manifest describes a finalized capture.
type Manifest struct {
	Type                 string        `json:"type"`
	SchemaVersion        string        `json:"schema_version"`
	NormalizationVersion string        `json:"normalization_version"`
	GCXVersion           string        `json:"gcx_version,omitempty"`
	Stack                StackIdentity `json:"stack"`
	// Caller is who took the capture. Captures by different callers are
	// not comparable, because permissions decide what a caller can see.
	Caller *CallerIdentity `json:"caller"`
	// Selectors are the resource type selectors that defined the scope.
	Selectors  []string         `json:"selectors"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	Types      []TypeCoverage   `json:"types"`
	Inventory  []InventoryEntry `json:"inventory"`
}

// TypeCoverage records how completely one selected resource type was read.
type TypeCoverage struct {
	// Type is the resource type key, plural.group (for example
	// dashboards.dashboard.grafana.app).
	Type string `json:"type"`
	// Selector is the selector that requested the type.
	Selector string `json:"selector"`
	// Version is the API version that was read. Empty when unavailable.
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Status  string `json:"status"`
	// ReadScope reports whether the caller can read every resource of the
	// type (all, restricted or unknown). Anything other than all makes the
	// type incomplete.
	ReadScope string `json:"read_scope,omitempty"`
	// Count is the number of resources written for the type.
	Count int `json:"count"`
	// Reasons explains an incomplete or unavailable status.
	Reasons []string `json:"reasons,omitempty"`
}

// Complete reports whether the type was read completely.
func (t TypeCoverage) Complete() bool { return t.Status == CoverageComplete }

// InventoryEntry is one resource file in a capture.
type InventoryEntry struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// Path is relative to the capture root and uses forward slashes.
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
