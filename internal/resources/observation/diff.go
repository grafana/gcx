package observation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Type comparison status values.
const (
	// TypeCompared means both captures read the type completely.
	TypeCompared = "compared"
	// TypePartial means at least one capture read the type incompletely;
	// only changes supported by both reads are reported.
	TypePartial = "partial"
	// TypeIndeterminate means the type cannot be compared at all.
	TypeIndeterminate = "indeterminate"
)

// Field change operations.
const (
	FieldAdded   = "added"
	FieldRemoved = "removed"
	FieldChanged = "changed"
)

// Limitations states what a comparison cannot establish. It is part of every
// result so that consumers do not read more into it than it supports.
//
//nolint:gochecknoglobals // constant-like list.
var limitations = []string{
	"captures are observations over a collection interval, not atomic snapshots; concurrent changes can appear in the result",
	"secure values are redacted; only the presence of secret names is compared, and a changed secret value cannot be observed",
	"a matching result is evidence about observable resource state, not proof that an operation succeeded",
}

// IncompatibleError reports captures that cannot be compared.
type IncompatibleError struct {
	Reason string
}

func (e *IncompatibleError) Error() string {
	return "captures are not comparable: " + e.Reason
}

// DiffResult is the structured comparison of two captures.
type DiffResult struct {
	Type                 string        `json:"type"`
	SchemaVersion        string        `json:"schema_version"`
	NormalizationVersion string        `json:"normalization_version"`
	Stack                StackIdentity `json:"stack"`
	Before               CaptureRef    `json:"before"`
	After                CaptureRef    `json:"after"`
	// Complete is true when every type was compared on complete reads and
	// no result is indeterminate.
	Complete    bool             `json:"complete"`
	Summary     DiffSummary      `json:"summary"`
	Types       []TypeComparison `json:"types"`
	Changes     []ResourceChange `json:"changes"`
	Limitations []string         `json:"limitations"`
}

// CaptureRef identifies one side of a comparison.
type CaptureRef struct {
	Path       string    `json:"path"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// DiffSummary counts resource outcomes.
type DiffSummary struct {
	Added         int `json:"added"`
	Modified      int `json:"modified"`
	Deleted       int `json:"deleted"`
	Unchanged     int `json:"unchanged"`
	Indeterminate int `json:"indeterminate"`
}

// TypeComparison reports how one resource type was compared.
type TypeComparison struct {
	Type         string   `json:"type"`
	Status       string   `json:"status"`
	BeforeStatus string   `json:"before_status,omitempty"`
	AfterStatus  string   `json:"after_status,omitempty"`
	Reasons      []string `json:"reasons,omitempty"`
}

// ResourceChange is one changed (or undeterminable) resource.
type ResourceChange struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Change string `json:"change"`
	// Reason explains an indeterminate result.
	Reason string `json:"reason,omitempty"`
	// Fields lists the field changes of a modified resource.
	Fields []FieldChange `json:"fields,omitempty"`
	// Object is the observed configuration of an added resource, or the last
	// observed configuration of a deleted one.
	Object map[string]any `json:"object,omitempty"`
}

// FieldChange is one change at a JSON pointer. Before is absent for an added
// field and After is absent for a removed one; a JSON null value is kept as
// null, so absence and null are distinguishable.
type FieldChange struct {
	Path   string          `json:"path"`
	Op     string          `json:"op"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// Compare compares two loaded captures. It returns an *IncompatibleError when
// the captures come from different stacks or use different normalization.
func Compare(before, after *Capture, beforePath, afterPath string) (*DiffResult, error) {
	bm, am := before.Manifest, after.Manifest
	if bm.NormalizationVersion != am.NormalizationVersion {
		return nil, &IncompatibleError{Reason: fmt.Sprintf("normalization versions differ (%q and %q)", bm.NormalizationVersion, am.NormalizationVersion)}
	}
	if bm.NormalizationVersion != NormalizationVersion {
		return nil, &IncompatibleError{Reason: fmt.Sprintf("normalization version %q is not supported (this gcx uses %q)", bm.NormalizationVersion, NormalizationVersion)}
	}
	if !bm.Stack.SameStack(am.Stack) {
		return nil, &IncompatibleError{Reason: fmt.Sprintf("captures come from different stacks (%s %s and %s %s)", bm.Stack.Server, bm.Stack.Namespace, am.Stack.Server, am.Stack.Namespace)}
	}

	result := &DiffResult{
		Type:                 DiffType,
		SchemaVersion:        SchemaVersion,
		NormalizationVersion: NormalizationVersion,
		Stack:                StackIdentity{Server: am.Stack.Server, Namespace: am.Stack.Namespace},
		Before:               CaptureRef{Path: beforePath, StartedAt: bm.StartedAt, FinishedAt: bm.FinishedAt},
		After:                CaptureRef{Path: afterPath, StartedAt: am.StartedAt, FinishedAt: am.FinishedAt},
		Types:                []TypeComparison{},
		Changes:              []ResourceChange{},
		Limitations:          append([]string(nil), limitations...),
	}

	bTypes := coverageByType(bm.Types)
	aTypes := coverageByType(am.Types)
	keys := map[string]bool{}
	for k := range bTypes {
		keys[k] = true
	}
	for k := range aTypes {
		keys[k] = true
	}
	typeKeys := make([]string, 0, len(keys))
	for k := range keys {
		typeKeys = append(typeKeys, k)
	}
	sort.Strings(typeKeys)

	for _, typeKey := range typeKeys {
		bCov, inBefore := bTypes[typeKey]
		aCov, inAfter := aTypes[typeKey]
		cmp := TypeComparison{Type: typeKey, BeforeStatus: bCov.Status, AfterStatus: aCov.Status}

		switch {
		case !inBefore:
			cmp.Status = TypeIndeterminate
			cmp.Reasons = []string{"type is not in the scope of the before capture"}
		case !inAfter:
			cmp.Status = TypeIndeterminate
			cmp.Reasons = []string{"type is not in the scope of the after capture"}
		case bCov.Status == CoverageUnavailable || aCov.Status == CoverageUnavailable:
			cmp.Status = TypeIndeterminate
			cmp.Reasons = []string{"type was unavailable in at least one capture"}
		case bCov.Version != aCov.Version:
			cmp.Status = TypeIndeterminate
			cmp.Reasons = []string{fmt.Sprintf("API versions differ (%s and %s)", bCov.Version, aCov.Version)}
		default:
			cmp.Status = TypeCompared
			if !bCov.Complete() {
				cmp.Status = TypePartial
				cmp.Reasons = append(cmp.Reasons, "before capture is incomplete")
			}
			if !aCov.Complete() {
				cmp.Status = TypePartial
				cmp.Reasons = append(cmp.Reasons, "after capture is incomplete")
			}
			compareType(result, typeKey, before.Objects[typeKey], after.Objects[typeKey], bCov.Complete(), aCov.Complete())
		}
		result.Types = append(result.Types, cmp)
	}

	result.Complete = result.Summary.Indeterminate == 0
	for _, t := range result.Types {
		if t.Status != TypeCompared {
			result.Complete = false
		}
	}
	return result, nil
}

func coverageByType(types []TypeCoverage) map[string]TypeCoverage {
	out := make(map[string]TypeCoverage, len(types))
	for _, t := range types {
		out[t.Type] = t
	}
	return out
}

func compareType(result *DiffResult, typeKey string, before, after map[string]map[string]any, beforeComplete, afterComplete bool) {
	names := map[string]bool{}
	for n := range before {
		names[n] = true
	}
	for n := range after {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	for _, name := range sorted {
		b, inBefore := before[name]
		a, inAfter := after[name]
		switch {
		case inBefore && inAfter:
			fields := diffValues("", b, a, true, true)
			if len(fields) == 0 {
				result.Summary.Unchanged++
				continue
			}
			result.Summary.Modified++
			result.Changes = append(result.Changes, ResourceChange{Type: typeKey, Name: name, Change: ChangeModified, Fields: fields})
		case inAfter:
			if !beforeComplete {
				result.Summary.Indeterminate++
				result.Changes = append(result.Changes, ResourceChange{Type: typeKey, Name: name, Change: ChangeIndeterminate, Reason: "absent from an incomplete before capture; it may have existed already"})
				continue
			}
			result.Summary.Added++
			result.Changes = append(result.Changes, ResourceChange{Type: typeKey, Name: name, Change: ChangeAdded, Object: a})
		default:
			if !afterComplete {
				result.Summary.Indeterminate++
				result.Changes = append(result.Changes, ResourceChange{Type: typeKey, Name: name, Change: ChangeIndeterminate, Reason: "absent from an incomplete after capture; deletion cannot be established"})
				continue
			}
			result.Summary.Deleted++
			result.Changes = append(result.Changes, ResourceChange{Type: typeKey, Name: name, Change: ChangeDeleted, Object: b})
		}
	}
}

// diffValues returns the field changes between two decoded JSON values.
// Objects are compared by key in sorted order; arrays are compared by index,
// so order is significant.
func diffValues(ptr string, before, after any, hasBefore, hasAfter bool) []FieldChange {
	switch {
	case !hasBefore && !hasAfter:
		return nil
	case !hasBefore:
		return []FieldChange{{Path: ptrOrRoot(ptr), Op: FieldAdded, After: raw(after)}}
	case !hasAfter:
		return []FieldChange{{Path: ptrOrRoot(ptr), Op: FieldRemoved, Before: raw(before)}}
	}

	bMap, bIsMap := before.(map[string]any)
	aMap, aIsMap := after.(map[string]any)
	if bIsMap && aIsMap {
		keys := map[string]bool{}
		for k := range bMap {
			keys[k] = true
		}
		for k := range aMap {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		var out []FieldChange
		for _, k := range sorted {
			bv, bok := bMap[k]
			av, aok := aMap[k]
			out = append(out, diffValues(ptr+"/"+escapePointer(k), bv, av, bok, aok)...)
		}
		return out
	}

	bArr, bIsArr := before.([]any)
	aArr, aIsArr := after.([]any)
	if bIsArr && aIsArr {
		var out []FieldChange
		n := max(len(bArr), len(aArr))
		for i := range n {
			var bv, av any
			bok, aok := i < len(bArr), i < len(aArr)
			if bok {
				bv = bArr[i]
			}
			if aok {
				av = aArr[i]
			}
			out = append(out, diffValues(ptr+"/"+strconv.Itoa(i), bv, av, bok, aok)...)
		}
		return out
	}

	if reflect.DeepEqual(before, after) {
		return nil
	}
	return []FieldChange{{Path: ptrOrRoot(ptr), Op: FieldChanged, Before: raw(before), After: raw(after)}}
}

func ptrOrRoot(ptr string) string {
	if ptr == "" {
		return "/"
	}
	return ptr
}

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func raw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		// Values come from decoded JSON, so this cannot happen; keep the
		// field present rather than silently dropping it.
		return json.RawMessage(`null`)
	}
	return data
}
