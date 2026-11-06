// opmodel_types.go; output documents and flag vocabularies for the IRM
// operational-model commands (gcx irm opmodel / shares).
//
// The read bodies below mirror the HTTP API's own JSON exactly (see
// opmodel_wire.go), so what gcx prints is what the API documents; one
// vocabulary, no translation layer to drift. gcx adds only the envelope
// discriminators (`type`, `schema_version`) and the guarantee that the entity
// arrays are never null.
//
// The enum vocabularies are the single source of truth for flag validation and
// for help text. The server validates too; validating here means a typo costs
// no round trip and the error names the whole vocabulary.
package irm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/format"
	fail "github.com/grafana/gcx/internal/gcxerrors"
)

// ---------------------------------------------------------------------------
// Flag vocabularies, mirrored from the API's own enum tags.
// ---------------------------------------------------------------------------

//nolint:gochecknoglobals // static enum vocabularies, mirrored from the op model API
var (
	actorKindVocab            = []string{"agent", "human"}
	claimTypeVocab            = []string{"hypothesis", "impact"}
	impactAssessmentVocab     = []string{"unassessed", "confirmed", "ruled_out"}
	hypothesisAssessmentVocab = []string{"unassessed", "accepted", "ruled_out"}
	recoveryVocab             = []string{"unknown", "ongoing", "mitigated", "ended"}
	actionStatusVocab         = []string{"proposed", "accepted", "in_progress", "blocked", "done", "failed", "abandoned"}
	stanceVocab               = []string{"context", "challenges", "supports"}
	// observationVerificationVocab and evidenceVerificationVocab differ: an
	// observation can be corroborated by an agent, an evidence link cannot. Neither
	// includes "withdrawn": withdrawal is a separate judgment, reachable
	// exclusively through `evidence-links withdraw`.
	observationVerificationVocab = []string{"agent_corroborated", "disputed", "human_verified", "unverified"}
	evidenceVerificationVocab    = []string{"disputed", "human_verified", "unverified"}
)

// validateEnumFlag checks a closed-vocabulary flag value, returning an exit-2
// DetailedError that names the rejected value and the whole vocabulary.
func validateEnumFlag(flagName, value string, vocab []string) error {
	if slices.Contains(vocab, value) {
		return nil
	}
	exit2 := fail.ExitUsageError
	return &fail.DetailedError{
		Summary:  fmt.Sprintf("invalid --%s value %q", flagName, value),
		Details:  "Allowed values: " + strings.Join(vocab, ", ") + ".",
		ExitCode: &exit2,
		Suggestions: []string{
			"Re-run with --" + flagName + " " + vocab[0],
		},
	}
}

// ---------------------------------------------------------------------------
// Read bodies; decoded straight from the API, then wrapped in an envelope.
// ---------------------------------------------------------------------------

type opModelActor struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type originalAuthor struct {
	DisplayName string `json:"display_name,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
}
type originOutput struct {
	ObservationID  string `json:"observation_id"`
	ContentVersion uint64 `json:"content_version"`
}
type subjectFreshnessOutput struct {
	SubjectNeedsReassessment bool   `json:"subject_needs_reassessment"`
	Reason                   string `json:"reason,omitempty"`
	JudgedVersion            uint64 `json:"judged_version"`
	CurrentVersion           uint64 `json:"current_version"`
}
type judgmentOutput struct {
	By               opModelActor            `json:"by"`
	At               string                  `json:"at,omitempty"`
	Reason           string                  `json:"reason,omitempty"`
	SubjectVersion   uint64                  `json:"subject_version"`
	SubjectFreshness *subjectFreshnessOutput `json:"subject_freshness,omitempty"`
}

type observationDTO struct {
	ID                   string          `json:"id"`
	Text                 string          `json:"text"`
	SourceRef            string          `json:"source_ref,omitempty"`
	ObservedStart        string          `json:"observed_start,omitempty"`
	ObservedEnd          string          `json:"observed_end,omitempty"`
	Scope                *string         `json:"scope,omitempty"`
	OriginalAuthor       *originalAuthor `json:"original_author,omitempty"`
	ContentVersion       uint64          `json:"content_version"`
	CreatedBy            opModelActor    `json:"created_by"`
	CreatedAt            string          `json:"created_at,omitempty"`
	Verification         string          `json:"verification"`
	VerificationRevision uint64          `json:"verification_revision"`
	VerificationJudgment *judgmentOutput `json:"verification_judgment,omitempty"`
}

type claimDTO struct {
	ID                   string          `json:"id"`
	Type                 string          `json:"type"`
	Text                 string          `json:"text"`
	ContentVersion       uint64          `json:"content_version"`
	Origin               *originOutput   `json:"origin,omitempty"`
	SourceRef            string          `json:"source_ref,omitempty"`
	OriginalAuthor       *originalAuthor `json:"original_author,omitempty"`
	ImpactAssessment     string          `json:"impact_assessment,omitempty"`
	HypothesisAssessment string          `json:"hypothesis_assessment,omitempty"`
	Recovery             string          `json:"recovery,omitempty"`
	AssessmentRevision   uint64          `json:"assessment_revision"`
	RecoveryRevision     uint64          `json:"recovery_revision"`
	AssessmentJudgment   *judgmentOutput `json:"assessment_judgment,omitempty"`
	RecoveryJudgment     *judgmentOutput `json:"recovery_judgment,omitempty"`
	CreatedBy            opModelActor    `json:"created_by"`
	CreatedAt            string          `json:"created_at,omitempty"`
}

type actionDTO struct {
	ID                   string          `json:"id"`
	Text                 string          `json:"text"`
	Status               string          `json:"status"`
	ContentVersion       uint64          `json:"content_version"`
	Origin               *originOutput   `json:"origin,omitempty"`
	SourceRef            string          `json:"source_ref,omitempty"`
	OriginalAuthor       *originalAuthor `json:"original_author,omitempty"`
	ExpectedResult       *string         `json:"expected_result,omitempty"`
	Assignee             *opModelActor   `json:"assignee,omitempty"`
	CoordinationRevision uint64          `json:"coordination_revision"`
	CreatedBy            opModelActor    `json:"created_by"`
	CreatedAt            string          `json:"created_at,omitempty"`
	AdvancedBy           *opModelActor   `json:"advanced_by,omitempty"`
	AdvancedAt           string          `json:"advanced_at,omitempty"`
}

type evidenceDTO struct {
	ID                   string          `json:"id"`
	SourceID             string          `json:"source_id"`
	TargetID             string          `json:"target_id"`
	SourceVersion        uint64          `json:"source_version"`
	TargetVersion        uint64          `json:"target_version"`
	ContentVersion       uint64          `json:"content_version"`
	Stance               string          `json:"stance"`
	Rationale            string          `json:"rationale,omitempty"`
	AttachedBy           opModelActor    `json:"attached_by"`
	AttachedAt           string          `json:"attached_at,omitempty"`
	Verification         string          `json:"verification"`
	VerificationRevision uint64          `json:"verification_revision"`
	VerificationJudgment *judgmentOutput `json:"verification_judgment,omitempty"`
	Withdrawal           *judgmentOutput `json:"withdrawal,omitempty"`
}
type containerBody struct {
	Container     string           `json:"container"`
	Revision      string           `json:"revision"`
	Observations  []observationDTO `json:"observations"`
	Claims        []claimDTO       `json:"claims"`
	Actions       []actionDTO      `json:"actions"`
	EvidenceLinks []evidenceDTO    `json:"evidence_links"`
}

// containerEnvelope is the output document of `containers get`. The four
// entity arrays are always present and never null.
type containerEnvelope struct {
	containerBody

	Type          string `json:"type"` // "gcx.irm.opmodel.container"
	SchemaVersion string `json:"schema_version"`
}

// orEmpty guarantees a slice serializes as [] rather than null; the output
// contract every read envelope in this tree promises.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func newContainerEnvelope(body *containerBody) containerEnvelope {
	env := containerEnvelope{
		Type:          "gcx.irm.opmodel.container",
		SchemaVersion: "2",
		containerBody: *body,
	}
	env.Observations = orEmpty(env.Observations)
	env.Claims = orEmpty(env.Claims)
	env.Actions = orEmpty(env.Actions)
	env.EvidenceLinks = orEmpty(env.EvidenceLinks)
	return env
}

// opModelEventDTO is one appended event read back from the stream. payload is
// carried through verbatim; the API already emits it as JSON.
type opModelEventDTO struct {
	Revision   string          `json:"revision"`
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurred_at,omitempty"`
	Actor      *opModelActor   `json:"actor,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// eventsBody is one page of the event stream as the API returns it. Revision
// is the container's head, independent of the page, so a caller can tell
// whether it has caught up.
type eventsBody struct {
	Events   []opModelEventDTO `json:"events"`
	Revision string            `json:"revision"`
}

// opModelEventsEnvelope is the output document of `containers list-events`.
type opModelEventsEnvelope struct {
	Type          string            `json:"type"` // "gcx.irm.opmodel.events"
	SchemaVersion string            `json:"schema_version"`
	Container     string            `json:"container"`
	Revision      string            `json:"revision"`
	Events        []opModelEventDTO `json:"events"`
}

func newOpModelEventsEnvelope(container string, body *eventsBody) opModelEventsEnvelope {
	return opModelEventsEnvelope{
		Type:          "gcx.irm.opmodel.events",
		SchemaVersion: "1",
		Container:     container,
		Revision:      body.Revision,
		Events:        orEmpty(body.Events),
	}
}

// ---------------------------------------------------------------------------
// Mutation envelope; shared by every op model write verb.
// ---------------------------------------------------------------------------

// opModelTarget names the entity a mutation acted on (or minted).
type opModelTarget struct {
	Kind string `json:"kind"` // observation | claim | action | evidence_link | entity
	ID   string `json:"id"`
}

// opModelMutation is the single result document every op model write verb
// emits on stdout. It does not reuse cmdio.NewSingleMutation: that envelope
// has no extension point for the container revision or the minted-id results,
// and its own header says not to force-fit new shapes into it. The
// instrumentation provider's MutationResult is the precedent for a
// domain-shaped mutation document.
type opModelMutation struct {
	Type          string            `json:"type"` // "gcx.irm.opmodel.mutation"
	SchemaVersion string            `json:"schema_version"`
	Action        string            `json:"action"`
	Container     string            `json:"container"`
	Target        opModelTarget     `json:"target"`
	Result        map[string]string `json:"result,omitempty"`
	Warnings      []string          `json:"warnings,omitempty"`
	Revision      string            `json:"revision"`
}

func newOpModelMutation(action, container string, target opModelTarget, result map[string]string, revision string) opModelMutation {
	return opModelMutation{
		Type:          "gcx.irm.opmodel.mutation",
		SchemaVersion: "2",
		Action:        action,
		Container:     container,
		Target:        target,
		Result:        result,
		Revision:      revision,
	}
}

// opModelMutationTextCodec renders the mutation envelope as one
// human-readable line. Registered as `-o text` (the TTY default); JSON /
// YAML / agents codecs carry the structured shape. Output-only.
type opModelMutationTextCodec struct{}

func (c *opModelMutationTextCodec) Format() format.Format { return format.Format("text") }

func (c *opModelMutationTextCodec) Encode(w io.Writer, v any) error {
	m, ok := v.(opModelMutation)
	if !ok {
		return fmt.Errorf("text codec: unsupported value type %T (expected opModelMutation)", v)
	}
	extra := ""
	if len(m.Result) > 0 {
		pairs := make([]string, 0, len(m.Result))
		for _, k := range slices.Sorted(maps.Keys(m.Result)) {
			pairs = append(pairs, k+"="+m.Result[k])
		}
		extra = "; " + strings.Join(pairs, " ")
	}
	warning := ""
	if len(m.Warnings) > 0 {
		warning = "; warning: " + strings.Join(m.Warnings, "; ")
	}
	_, err := fmt.Fprintf(w, "%s %s %s: ok%s (revision %s)%s\n", m.Action, m.Target.Kind, m.Target.ID, extra, m.Revision, warning)
	return err
}

func (c *opModelMutationTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}
