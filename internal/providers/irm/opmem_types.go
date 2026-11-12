package irm

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
)

//nolint:gochecknoglobals // static enum vocabularies, mirrored from the op model API
var (
	actorKindVocab  = []string{"agent", "human"}
	claimTypeVocab  = []string{"hypothesis", "impact"}
	entityTypeVocab = []string{"observation", "hypothesis", "impact", "action"}
	stanceVocab     = []string{"context", "challenges", "supports"}
)

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

type opMemActor struct {
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
	By               opMemActor              `json:"by"`
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
	CreatedBy            opMemActor      `json:"created_by"`
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
	CreatedBy            opMemActor      `json:"created_by"`
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
	Assignee             *opMemActor     `json:"assignee,omitempty"`
	CoordinationRevision uint64          `json:"coordination_revision"`
	CreatedBy            opMemActor      `json:"created_by"`
	CreatedAt            string          `json:"created_at,omitempty"`
	AdvancedBy           *opMemActor     `json:"advanced_by,omitempty"`
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
	AttachedBy           opMemActor      `json:"attached_by"`
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

type containerEnvelope struct {
	containerBody

	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func newContainerEnvelope(body *containerBody) containerEnvelope {
	env := containerEnvelope{
		Type:          "gcx.irm.opmem.container",
		SchemaVersion: "2",
		containerBody: *body,
	}
	env.Observations = orEmpty(env.Observations)
	env.Claims = orEmpty(env.Claims)
	env.Actions = orEmpty(env.Actions)
	env.EvidenceLinks = orEmpty(env.EvidenceLinks)
	return env
}

type opMemEventDTO struct {
	Revision   string          `json:"revision"`
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurred_at,omitempty"`
	Actor      *opMemActor     `json:"actor,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type eventsBody struct {
	Events   []opMemEventDTO `json:"events"`
	Revision string          `json:"revision"`
}

type opMemEventsEnvelope struct {
	Type          string          `json:"type"`
	SchemaVersion string          `json:"schema_version"`
	Container     string          `json:"container"`
	Revision      string          `json:"revision"`
	Events        []opMemEventDTO `json:"events"`
}

func newOpMemEventsEnvelope(container string, body *eventsBody) opMemEventsEnvelope {
	return opMemEventsEnvelope{
		Type:          "gcx.irm.opmem.events",
		SchemaVersion: "1",
		Container:     container,
		Revision:      body.Revision,
		Events:        orEmpty(body.Events),
	}
}
