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

//nolint:gochecknoglobals // static enum vocabularies, mirrored from the op model API
var (
	actorKindVocab               = []string{"agent", "human"}
	claimTypeVocab               = []string{"hypothesis", "impact"}
	impactAssessmentVocab        = []string{"unassessed", "confirmed", "ruled_out"}
	hypothesisAssessmentVocab    = []string{"unassessed", "accepted", "ruled_out"}
	recoveryVocab                = []string{"unknown", "ongoing", "mitigated", "ended"}
	actionStatusVocab            = []string{"proposed", "accepted", "in_progress", "blocked", "done", "failed", "abandoned"}
	stanceVocab                  = []string{"context", "challenges", "supports"}
	observationVerificationVocab = []string{"agent_corroborated", "disputed", "human_verified", "unverified"}
	evidenceVerificationVocab    = []string{"disputed", "human_verified", "unverified"}
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

type opModelEventDTO struct {
	Revision   string          `json:"revision"`
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurred_at,omitempty"`
	Actor      *opModelActor   `json:"actor,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type eventsBody struct {
	Events   []opModelEventDTO `json:"events"`
	Revision string            `json:"revision"`
}

type opModelEventsEnvelope struct {
	Type          string            `json:"type"`
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

type opModelTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type opModelMutation struct {
	Type          string            `json:"type"`
	SchemaVersion string            `json:"schema_version"`
	Action        string            `json:"action"`
	Container     string            `json:"container"`
	Target        opModelTarget     `json:"target"`
	Result        map[string]string `json:"result,omitempty"`
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
	_, err := fmt.Fprintf(w, "%s %s %s: ok%s (revision %s)\n", m.Action, m.Target.Kind, m.Target.ID, extra, m.Revision)
	return err
}

func (c *opModelMutationTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}
