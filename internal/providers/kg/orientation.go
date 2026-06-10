package kg

import (
	"fmt"
	"slices"
	"strings"
)

// Orientation summarizes a stack's Entity Graph state in terms a user can
// orient against: which of the five common scenarios their run matches, what
// the entity / scope shape looks like, and where to start scoping if the
// stack is unfamiliar.
//
// It is computed from the data already collected by runDiagnose and adds no
// new API calls. The text codec renders it above the per-check table; the
// JSON codec exposes it as a structured `orientation` field on
// DiagnoseResult.
//
// Design rules (carried from maintainer feedback on prior PRs):
//   - Numbers printed in this block are runtime data computed from the
//     user's DiagnoseResult — never authored thresholds.
//   - Scenario detection conditions are binary, not threshold-based.
//   - Scenarios are named verbatim every time; never referenced by index.
type Orientation struct {
	// PipelineHealth is a one-word summary of the overall check pass/fail
	// state. Maps to the existing summary counts:
	//   "healthy"  — all checks PASS
	//   "degraded" — at least one WARN, no FAIL
	//   "failed"   — at least one FAIL
	PipelineHealth PipelineHealth `json:"pipelineHealth"`

	// EntityOverview describes the entity inventory the run discovered.
	EntityOverview EntityOverview `json:"entityOverview"`

	// Scope describes the scope dimensions in play for this run.
	Scope ScopeSummary `json:"scope"`

	// MatchedScenarios lists the original five Entity Graph scenarios that
	// this run's data matches. Multiple may match simultaneously; ordering
	// is by confidence (high first). Empty when none match.
	MatchedScenarios []MatchedScenario `json:"matchedScenarios,omitempty"`

	// StartingPoints suggests ways to scope further when the user has not
	// set any --env / --namespace / --site filter. Omitted (nil) when any
	// scope flag was set.
	StartingPoints []StartingPoint `json:"startingPoints,omitempty"`
}

// PipelineHealth is a coarse one-word verdict on the existing per-check
// pass/fail counts. Distinct type so the text codec and JSON consumers can
// pattern-match without parsing the verb string.
type PipelineHealth string

const (
	PipelineHealthy  PipelineHealth = "healthy"
	PipelineDegraded PipelineHealth = "degraded"
	PipelineFailed   PipelineHealth = "failed"
)

// EntityOverview is the user-facing summary of how many entities exist and
// how many of them are emitting traces. All numbers are computed from the
// existing entity-count and metric-check responses in DiagnoseResult.
type EntityOverview struct {
	// Total entities across all types, EXCLUDING catalog meta types
	// (EntityType, Schema, Env). Those types describe the catalog itself,
	// not the user's workloads, and inflate the count without informational
	// value.
	Total int64 `json:"total"`

	// ByType is the per-type breakdown that drove Total. Same exclusion
	// applied. Sorted output is the codec's concern, not this struct's.
	ByType map[string]int64 `json:"byType"`

	// TracedServiceCount is the number of Tempo-traced services for the
	// scope being examined. Derived from the traces_target_info series
	// count returned by the existing metric check.
	TracedServiceCount int64 `json:"tracedServiceCount"`

	// TotalServiceCount is the total Service-typed entity count, from
	// ByType["Service"]. Surfaced separately so the codec can render
	// "M of N services emit traces" without a separate lookup.
	TotalServiceCount int64 `json:"totalServiceCount"`
}

// ScopeSummary captures what scope dimensions are known to the stack and
// what the user filtered on for this run.
type ScopeSummary struct {
	// FilterSet is true if the user supplied any of --env / --namespace /
	// --site. The starting-points block in Orientation is suppressed when
	// this is true — the user has already expressed a preference.
	FilterSet bool `json:"filterSet"`

	// EnvsKnown is the list of env scope values the Asserts API reports.
	// Includes the "none" bucket as a literal value when present; the
	// codec calls it out so the user doesn't mistake it for a real env.
	EnvsKnown []string `json:"envsKnown,omitempty"`

	// NamespacesKnown is the list of namespace scope values the Asserts
	// API reports. Used by the starting-points block to suggest scoping
	// by namespace.
	NamespacesKnown []string `json:"namespacesKnown,omitempty"`

	// NoneBucketPresent is reserved for a future detector that distinguishes
	// a "none" env value backed by entities from bare presence in the scope
	// list. Producing it truthfully requires a scoped CountEntityTypes(
	// env="none") call (not currently issued by runDiagnose), so this field
	// is left at its zero value until that producer is wired.
	NoneBucketPresent bool `json:"noneBucketPresent"`
}

// MatchedScenario describes one of the five original Entity Graph scenarios
// that this run's data matches. Names appear verbatim — never referenced
// by index — so the agent and the user have a stable string to align on.
type MatchedScenario struct {
	// ID is a stable machine-readable identifier for agents.
	ID ScenarioID `json:"id"`

	// Label is the user-facing scenario name, quoted verbatim from the
	// founding troubleshooting guide. Example: "I see no entities at all".
	Label string `json:"label"`

	// Confidence is how strongly the detector matches. Sort order in
	// MatchedScenarios uses this field (high before medium).
	Confidence Confidence `json:"confidence"`

	// Reasoning is a one-line explanation of why the detector triggered,
	// using only data the user can verify by re-running diagnose or
	// running an adjacent gcx command. No hard-coded thresholds.
	Reasoning string `json:"reasoning"`

	// NextCommands are concrete gcx commands the user (or agent) can run
	// next. Each must be a real, current gcx command — no aspirational
	// flags or subcommands.
	NextCommands []string `json:"nextCommands,omitempty"`
}

// ScenarioID is a stable identifier for one of the five scenarios.
// Scenario 4 ("disconnected clusters") is deferred to a follow-up PR
// (needs per-env comparison we don't currently do at stack level), so it
// has an ID reserved here but no detector yet.
type ScenarioID string

const (
	ScenarioNoEntities           ScenarioID = "no-entities"
	ScenarioMissingEntities      ScenarioID = "missing-entities"
	ScenarioEntitiesNoEdges      ScenarioID = "entities-no-edges"
	ScenarioDisconnectedClusters ScenarioID = "disconnected-clusters" // reserved; not yet detected
	ScenarioCantFilter           ScenarioID = "cant-filter"
)

// Confidence is the strength of a scenario match. Only two levels — keeping
// the vocabulary small avoids judgment-call boundaries.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
)

// StartingPoint is one suggested way to scope an unscoped run. Each
// starting point has a one-line context (using runtime data from this run,
// e.g. "you have N namespaces") and a concrete gcx command.
type StartingPoint struct {
	// ID is a stable machine-readable identifier.
	ID StartingPointID `json:"id"`

	// Label is the user-facing description of this scoping mode.
	Label string `json:"label"`

	// Context is runtime-substituted detail using only data from the
	// current DiagnoseResult — e.g. "you have N namespaces on this stack".
	// No judgment about whether a count is "too many".
	Context string `json:"context,omitempty"`

	// Command is the gcx command the user runs to act on this starting
	// point. Must be a real, current gcx command.
	Command string `json:"command"`
}

// StartingPointID is a stable identifier for one of the starting-point
// modes: namespace, name-pattern, or single-service-and-neighbors.
type StartingPointID string

const (
	StartingPointByNamespace     StartingPointID = "by-namespace"
	StartingPointByNamePattern   StartingPointID = "by-name-pattern"
	StartingPointBySingleService StartingPointID = "by-single-service"
)

// ---------------------------------------------------------------------------
// Detection
// ---------------------------------------------------------------------------

// isMetaEntityType reports whether the given entity type is a catalog-level
// type that describes the Entity Graph schema itself rather than user
// workloads. These inflate entity counts without informational value; the
// EntityOverview excludes them.
func isMetaEntityType(t string) bool {
	switch t {
	case "EntityType", "Schema", "Env":
		return true
	default:
		return false
	}
}

// OrientationInput is the raw data the detectors need. It is populated by
// runDiagnose from the same API responses that drive the per-check table,
// then passed to computeOrientation. Separating the input from the
// detection logic keeps the detectors pure and table-testable.
type OrientationInput struct {
	// StackEnabled is true when the Asserts plugin reports
	// status="complete" and enabled=true. False signals scenario 1
	// directly (the stack isn't ready).
	StackEnabled bool

	// EntityCounts is the raw per-type count map returned by
	// CountEntityTypes. Includes meta types; the detector filters them
	// out when computing the user-facing total.
	EntityCounts map[string]int64

	// Scopes is the ListEntityScopes response (env / site / namespace
	// dimension values). May include "none" as a literal value for the
	// unscoped bucket.
	Scopes map[string][]string

	// NoneBucketHasEntities is reserved for a future detector that
	// distinguishes a "none" env value backed by entities from bare
	// presence in the scope list. Producing it truthfully requires a
	// scoped CountEntityTypes(env="none") call that runDiagnose does not
	// currently issue, so this field is left at its zero value until
	// that producer is wired.
	NoneBucketHasEntities bool

	// TracesTargetInfoSeries is the count of traces_target_info series
	// returned by the existing metric check, scoped to the current
	// --env / --namespace filter (or unscoped if none set). Used to
	// compute TracedServiceCount.
	TracesTargetInfoSeries int64

	// AssertsRelationCallsSeries is the count of asserts:relation:calls
	// series for the scope. Zero with non-empty services signals
	// scenario 3 ("entities with no edges").
	AssertsRelationCallsSeries int64

	// AssertsMixinWorkloadJobSeries is the count of
	// asserts:mixin_workload_job series for the scope. Used to detect
	// scenario 2 ("some expected entities are missing") when the user
	// has set a scope filter but the scoped metric checks returned
	// nothing.
	AssertsMixinWorkloadJobSeries int64
}

// computeOrientation produces the user-facing Orientation block from raw
// API data and the scope flags. It runs after the per-check goroutines
// complete; it adds no new API calls of its own.
//
// Scenario ordering in the output is explicit: high-confidence entries
// come before medium-confidence; within a confidence tier, ScenarioID
// alphabetical order is the tiebreaker. The sort is applied after all
// detectors run so adding or reordering detector calls cannot change
// the user-visible order.
func computeOrientation(in OrientationInput, scope *scopeFlags) Orientation {
	overview := buildEntityOverview(in)
	scopeSummary := buildScopeSummary(in, scope)

	var matched []MatchedScenario
	if s := detectNoEntities(in, overview); s != nil {
		matched = append(matched, *s)
	}
	if s := detectEntitiesNoEdges(in, overview, scope); s != nil {
		matched = append(matched, *s)
	}
	if s := detectCantFilter(in, scope); s != nil {
		matched = append(matched, *s)
	}
	if s := detectMissingEntities(in, scope); s != nil {
		matched = append(matched, *s)
	}
	slices.SortStableFunc(matched, func(a, b MatchedScenario) int {
		if a.Confidence != b.Confidence {
			// high before medium
			if a.Confidence == ConfidenceHigh {
				return -1
			}
			return 1
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})

	var startingPoints []StartingPoint
	if !scopeSummary.FilterSet {
		startingPoints = buildStartingPoints(scopeSummary)
	}

	return Orientation{
		PipelineHealth:   PipelineHealthy, // overwritten by runDiagnose after check summary
		EntityOverview:   overview,
		Scope:            scopeSummary,
		MatchedScenarios: matched,
		StartingPoints:   startingPoints,
	}
}

// buildEntityOverview filters meta types out of EntityCounts and computes
// the totals the user-facing block needs.
func buildEntityOverview(in OrientationInput) EntityOverview {
	out := EntityOverview{
		ByType:             make(map[string]int64, len(in.EntityCounts)),
		TracedServiceCount: in.TracesTargetInfoSeries,
	}
	for t, n := range in.EntityCounts {
		if isMetaEntityType(t) {
			continue
		}
		out.ByType[t] = n
		out.Total += n
		if t == "Service" {
			out.TotalServiceCount = n
		}
	}
	return out
}

// buildScopeSummary captures the scope state for the run. NoneBucketPresent
// is propagated from OrientationInput.NoneBucketHasEntities, which is
// reserved for a future detector (see field doc); today it is always
// zero in production.
func buildScopeSummary(in OrientationInput, scope *scopeFlags) ScopeSummary {
	out := ScopeSummary{
		FilterSet:         scope.env != "" || scope.namespace != "" || scope.site != "",
		NoneBucketPresent: in.NoneBucketHasEntities,
	}
	if envs, ok := in.Scopes["env"]; ok {
		out.EnvsKnown = append(out.EnvsKnown, envs...)
	}
	if ns, ok := in.Scopes["namespace"]; ok {
		out.NamespacesKnown = append(out.NamespacesKnown, ns...)
	}
	return out
}

// ---------------------------------------------------------------------------
// Scenario detectors (binary conditions only — no thresholds)
// ---------------------------------------------------------------------------

// detectNoEntities → scenario 1, "I see no entities at all".
// Triggers when the stack is disabled OR the non-meta entity total is zero.
func detectNoEntities(in OrientationInput, overview EntityOverview) *MatchedScenario {
	if in.StackEnabled && overview.Total > 0 {
		return nil
	}
	reasoning := "No workload entities are discovered on this stack."
	if !in.StackEnabled {
		reasoning = "The Asserts plugin reports the stack is not fully enabled."
	}
	return &MatchedScenario{
		ID:         ScenarioNoEntities,
		Label:      "I see no entities at all",
		Confidence: ConfidenceHigh,
		Reasoning:  reasoning,
		NextCommands: []string{
			"gcx kg status",
			"gcx kg diagnose --env <env>",
		},
	}
}

// detectEntitiesNoEdges → scenario 3, "I see entities with no edges".
// Triggers when workload entities exist for the scope (scoped signal:
// asserts:mixin_workload_job > 0) but asserts:relation:calls has zero
// series for the same scope. Both signals share scope so a --env/--namespace
// filter pointed at an empty scope no longer false-positives against
// stack-wide service counts.
func detectEntitiesNoEdges(in OrientationInput, _ EntityOverview, scope *scopeFlags) *MatchedScenario {
	// Workload signal is scoped (metricChecks(scope.env, scope.namespace)).
	// No workloads in scope → defer to detectMissingEntities; this detector
	// must not fire on stack-wide service counts.
	if in.AssertsMixinWorkloadJobSeries == 0 {
		return nil
	}
	if in.AssertsRelationCallsSeries > 0 {
		return nil
	}
	scopeHint := "this stack"
	if scope.env != "" {
		scopeHint = "env=" + scope.env
	}
	return &MatchedScenario{
		ID:         ScenarioEntitiesNoEdges,
		Label:      "I see entities with no edges",
		Confidence: ConfidenceHigh,
		Reasoning:  "Workload entities are discovered for " + scopeHint + " but asserts:relation:calls has no series.",
		NextCommands: []string{
			"gcx kg diagnose labels",
			"gcx kg diagnose service <name>",
		},
	}
}

// detectCantFilter → scenario 5, "I can't filter to the entities I want".
// Triggers when the user supplied a scope value that doesn't match any
// known scope.
//
// A second trigger (the "none" env bucket backed by entities) is reserved
// for a future detector; see OrientationInput.NoneBucketHasEntities. Until
// that producer is wired, this detector relies on scopeValueUnknown alone.
func detectCantFilter(in OrientationInput, scope *scopeFlags) *MatchedScenario {
	if !scopeValueUnknown(in, scope) {
		return nil
	}
	return &MatchedScenario{
		ID:         ScenarioCantFilter,
		Label:      "I can't filter to the entities I want",
		Confidence: ConfidenceHigh,
		Reasoning:  "The scope value you provided does not match any value the stack knows about.",
		NextCommands: []string{
			"gcx kg meta scopes",
			"gcx kg diagnose labels",
		},
	}
}

// detectMissingEntities → scenario 2, "Some expected entities are missing".
// Triggers when the user supplied a scope filter AND the scoped
// asserts:mixin_workload_job check returned zero series (no workloads
// discovered in scope). Medium confidence — also fires when a scope is
// genuinely empty, which is a legitimate state, hence the lower confidence
// vs. scenarios 1 and 3.
func detectMissingEntities(in OrientationInput, scope *scopeFlags) *MatchedScenario {
	filterSet := scope.env != "" || scope.namespace != "" || scope.site != ""
	if !filterSet {
		return nil
	}
	if in.AssertsMixinWorkloadJobSeries > 0 {
		return nil
	}
	return &MatchedScenario{
		ID:         ScenarioMissingEntities,
		Label:      "Some expected entities are missing",
		Confidence: ConfidenceMedium,
		Reasoning:  "The scope filter you applied returned no workload entities — either the scope is genuinely empty or the asserts_env mapping does not cover it.",
		NextCommands: []string{
			"gcx kg diagnose labels",
			"gcx kg entities query \"MATCH (n) WHERE n.name CONTAINS 'X' RETURN n LIMIT 10\"",
		},
	}
}

// scopeValueUnknown reports whether any user-supplied scope filter value
// (--env / --namespace / --site) is missing from the corresponding scope
// dimension returned by the Asserts API. Best-effort: if a dimension is
// missing from Scopes entirely we treat it as not-unknown so we don't
// false-positive on stacks that don't populate every dimension.
func scopeValueUnknown(in OrientationInput, scope *scopeFlags) bool {
	checks := []struct {
		value, dim string
	}{
		{scope.env, "env"},
		{scope.namespace, "namespace"},
		{scope.site, "site"},
	}
	for _, c := range checks {
		if c.value == "" {
			continue
		}
		known, ok := in.Scopes[c.dim]
		if !ok {
			continue
		}
		if !slices.Contains(known, c.value) {
			return true
		}
	}
	return false
}

// buildStartingPoints emits the three scoping suggestions. The codec is
// responsible for rendering — this function just produces the structured
// data with runtime-substituted context values.
func buildStartingPoints(scope ScopeSummary) []StartingPoint {
	points := []StartingPoint{
		{
			ID:      StartingPointByNamespace,
			Label:   "By namespace",
			Command: "gcx kg diagnose --namespace NS",
		},
		{
			ID:      StartingPointByNamePattern,
			Label:   "By service name pattern",
			Command: "gcx kg entities query \"MATCH (s:Service) WHERE s.name CONTAINS 'X' RETURN s LIMIT 20\"",
		},
		{
			ID:      StartingPointBySingleService,
			Label:   "By single service + neighbors",
			Command: "gcx kg diagnose service NAME",
		},
	}
	if n := len(scope.NamespacesKnown); n > 0 {
		points[0].Context = fmt.Sprintf("you have %d namespace(s) on this stack", n)
	}
	return points
}
