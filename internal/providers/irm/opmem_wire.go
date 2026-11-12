package irm

import (
	"net/url"
	"strconv"
)

const opMemAPIPrefix = "/apis/irm.grafana.app/v1alpha1"

func containerBasePath(container string) string {
	return opMemAPIPrefix + "/containers/" + url.PathEscape(container)
}

type opMemActorInput struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type createObservationRequest struct {
	Container string          `json:"-"`
	Actor     opMemActorInput `json:"actor"`
	Text      string          `json:"text"`
	SourceRef string          `json:"source_ref,omitempty"`
}
type createClaimRequest struct {
	Container string          `json:"-"`
	Actor     opMemActorInput `json:"actor"`
	Type      string          `json:"type"`
	Text      string          `json:"text"`
}
type createActionRequest struct {
	Container string          `json:"-"`
	Actor     opMemActorInput `json:"actor"`
	Text      string          `json:"text"`
}
type promoteToActionRequest struct {
	Container     string          `json:"-"`
	ObservationID string          `json:"-"`
	Actor         opMemActorInput `json:"actor"`
	Text          string          `json:"text,omitempty"`
}
type attachEvidenceRequest struct {
	Container string          `json:"-"`
	Actor     opMemActorInput `json:"actor"`
	SourceID  string          `json:"source_id"`
	TargetID  string          `json:"target_id"`
	Stance    string          `json:"stance"`
	Rationale string          `json:"rationale,omitempty"`
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
