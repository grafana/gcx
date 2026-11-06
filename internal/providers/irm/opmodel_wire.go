package irm

import (
	"net/url"
	"strconv"
	"time"
)

const opModelAPIPrefix = "/apis/irm.grafana.app/v1alpha1"

func containerBasePath(container string) string {
	return opModelAPIPrefix + "/containers/" + url.PathEscape(container)
}

type opModelActorInput struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type createObservationRequest struct {
	Container      string            `json:"-"`
	Actor          opModelActorInput `json:"actor"`
	Text           string            `json:"text"`
	SourceRef      string            `json:"source_ref,omitempty"`
	ObservedStart  *time.Time        `json:"observed_start,omitempty"`
	ObservedEnd    *time.Time        `json:"observed_end,omitempty"`
	Scope          *string           `json:"scope,omitempty"`
	OriginalAuthor *originalAuthor   `json:"original_author,omitempty"`
}
type observationContent struct {
	Text           string          `json:"text"`
	SourceRef      string          `json:"source_ref,omitempty"`
	ObservedStart  *time.Time      `json:"observed_start,omitempty"`
	ObservedEnd    *time.Time      `json:"observed_end,omitempty"`
	Scope          *string         `json:"scope,omitempty"`
	OriginalAuthor *originalAuthor `json:"original_author,omitempty"`
}
type actionContent struct {
	Text           string  `json:"text"`
	ExpectedResult *string `json:"expected_result,omitempty"`
}
type createClaimRequest struct {
	Container string            `json:"-"`
	Actor     opModelActorInput `json:"actor"`
	Type      string            `json:"type"`
	Text      string            `json:"text"`
}
type createActionRequest struct {
	Container      string             `json:"-"`
	Actor          opModelActorInput  `json:"actor"`
	Text           string             `json:"text"`
	ExpectedResult *string            `json:"expected_result,omitempty"`
	Assignee       *opModelActorInput `json:"assignee,omitempty"`
}
type promoteRequest struct {
	Container     string            `json:"-"`
	ObservationID string            `json:"-"`
	Actor         opModelActorInput `json:"actor"`
	Type          string            `json:"type"`
	Text          string            `json:"text,omitempty"`
}
type promoteToActionRequest struct {
	Container     string            `json:"-"`
	ObservationID string            `json:"-"`
	Actor         opModelActorInput `json:"actor"`
	Text          string            `json:"text,omitempty"`
}
type editTextRequest struct {
	Container string            `json:"-"`
	EntityID  string            `json:"-"`
	Actor     opModelActorInput `json:"actor"`
	Text      string            `json:"text"`
}
type editObservationRequest struct {
	Container     string             `json:"-"`
	ObservationID string             `json:"-"`
	Actor         opModelActorInput  `json:"actor"`
	Content       observationContent `json:"content"`
}
type editActionRequest struct {
	Container string            `json:"-"`
	ActionID  string            `json:"-"`
	Actor     opModelActorInput `json:"actor"`
	Content   actionContent     `json:"content"`
}
type assessClaimRequest struct {
	Container            string            `json:"-"`
	ClaimID              string            `json:"-"`
	Actor                opModelActorInput `json:"actor"`
	ImpactAssessment     string            `json:"impact_assessment,omitempty"`
	HypothesisAssessment string            `json:"hypothesis_assessment,omitempty"`
	Reason               string            `json:"reason,omitempty"`
}
type updateRecoveryRequest struct {
	Container string            `json:"-"`
	ClaimID   string            `json:"-"`
	Actor     opModelActorInput `json:"actor"`
	Recovery  string            `json:"recovery"`
	Reason    string            `json:"reason,omitempty"`
}
type coordinateActionRequest struct {
	Container   string             `json:"-"`
	ActionID    string             `json:"-"`
	Actor       opModelActorInput  `json:"actor"`
	Status      *string            `json:"status,omitempty"`
	Assignee    *opModelActorInput `json:"assignee,omitempty"`
	SetAssignee bool               `json:"set_assignee,omitempty"`
	Reason      string             `json:"reason,omitempty"`
}
type verifyObservationRequest struct {
	Container     string            `json:"-"`
	ObservationID string            `json:"-"`
	Actor         opModelActorInput `json:"actor"`
	Verification  string            `json:"verification"`
	Reason        string            `json:"reason,omitempty"`
}
type attachEvidenceRequest struct {
	Container string            `json:"-"`
	Actor     opModelActorInput `json:"actor"`
	SourceID  string            `json:"source_id"`
	TargetID  string            `json:"target_id"`
	Stance    string            `json:"stance"`
	Rationale string            `json:"rationale,omitempty"`
}
type setEvidenceVerificationRequest struct {
	Container    string            `json:"-"`
	EvidenceID   string            `json:"-"`
	Actor        opModelActorInput `json:"actor"`
	Verification string            `json:"verification"`
	Reason       string            `json:"reason,omitempty"`
}
type withdrawEvidenceRequest struct {
	Container  string            `json:"-"`
	EvidenceID string            `json:"-"`
	Actor      opModelActorInput `json:"actor"`
	Reason     string            `json:"reason,omitempty"`
}

type revisionResponse struct {
	Revision string `json:"revision"`
}

type createObservationResponse struct {
	ObservationID string `json:"observation_id"`
	Revision      string `json:"revision"`
}

type createClaimResponse struct {
	ClaimID  string `json:"claim_id"`
	Revision string `json:"revision"`
}

type createActionResponse struct {
	ActionID string `json:"action_id"`
	Status   string `json:"status"`
	Revision string `json:"revision"`
}

type promoteResponse struct {
	ClaimID  string `json:"claim_id"`
	Revision string `json:"revision"`
}

type promoteToActionResponse struct {
	ActionID string `json:"action_id"`
	Status   string `json:"status"`
	Revision string `json:"revision"`
}

type attachEvidenceResponse struct {
	EvidenceID string `json:"evidence_id"`
	Revision   string `json:"revision"`
}

type getContainerRequest struct {
	Container string
}

type listEventsRequest struct {
	Container     string
	SinceRevision string
	PageSize      int32
}

func (r listEventsRequest) query() url.Values {
	params := url.Values{}
	if r.SinceRevision != "" {
		params.Set("since_revision", r.SinceRevision)
	}
	if r.PageSize > 0 {
		params.Set("page_size", strconv.FormatInt(int64(r.PageSize), 10))
	}
	return params
}
