package irm

import (
	"context"
	"net/url"
)

//nolint:interfacebloat // mirrors the HTTP API surface 1:1; full CRUD parity is the contract
type OpModelAPI interface {
	CreateObservation(ctx context.Context, req createObservationRequest) (*createObservationResponse, error)
	CreateClaim(ctx context.Context, req createClaimRequest) (*createClaimResponse, error)
	CreateAction(ctx context.Context, req createActionRequest) (*createActionResponse, error)
	Promote(ctx context.Context, req promoteRequest) (*promoteResponse, error)
	PromoteToAction(ctx context.Context, req promoteToActionRequest) (*promoteToActionResponse, error)
	EditObservation(ctx context.Context, req editObservationRequest) (*revisionResponse, error)
	EditAction(ctx context.Context, req editActionRequest) (*revisionResponse, error)
	UpdateRecovery(ctx context.Context, req updateRecoveryRequest) (*revisionResponse, error)
	EditText(ctx context.Context, req editTextRequest) (*revisionResponse, error)
	AssessClaim(ctx context.Context, req assessClaimRequest) (*revisionResponse, error)
	CoordinateAction(ctx context.Context, req coordinateActionRequest) (*revisionResponse, error)
	VerifyObservation(ctx context.Context, req verifyObservationRequest) (*revisionResponse, error)
	AttachEvidence(ctx context.Context, req attachEvidenceRequest) (*attachEvidenceResponse, error)
	SetEvidenceVerification(ctx context.Context, req setEvidenceVerificationRequest) (*revisionResponse, error)
	WithdrawEvidence(ctx context.Context, req withdrawEvidenceRequest) (*revisionResponse, error)
	GetContainer(ctx context.Context, req getContainerRequest) (*containerBody, error)
	ListEvents(ctx context.Context, req listEventsRequest) (*eventsBody, error)
}

var _ OpModelAPI = (*irmResourceClient)(nil)

func (c *irmResourceClient) base(container string) string {
	return "/opmodel" + containerBasePath(container)
}

func (c *irmResourceClient) CreateObservation(ctx context.Context, req createObservationRequest) (*createObservationResponse, error) {
	return post[createObservationResponse](ctx, c, c.base(req.Container)+"/observations", req)
}

func (c *irmResourceClient) CreateClaim(ctx context.Context, req createClaimRequest) (*createClaimResponse, error) {
	return post[createClaimResponse](ctx, c, c.base(req.Container)+"/claims", req)
}

func (c *irmResourceClient) CreateAction(ctx context.Context, req createActionRequest) (*createActionResponse, error) {
	return post[createActionResponse](ctx, c, c.base(req.Container)+"/actions", req)
}

func (c *irmResourceClient) Promote(ctx context.Context, req promoteRequest) (*promoteResponse, error) {
	return post[promoteResponse](ctx, c, c.base(req.Container)+"/observations/"+url.PathEscape(req.ObservationID)+"/promote", req)
}

func (c *irmResourceClient) PromoteToAction(ctx context.Context, req promoteToActionRequest) (*promoteToActionResponse, error) {
	return post[promoteToActionResponse](ctx, c, c.base(req.Container)+"/observations/"+url.PathEscape(req.ObservationID)+"/promote-to-action", req)
}

func (c *irmResourceClient) EditText(ctx context.Context, req editTextRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/entities/"+url.PathEscape(req.EntityID)+"/edit", req)
}

func (c *irmResourceClient) AssessClaim(ctx context.Context, req assessClaimRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/claims/"+url.PathEscape(req.ClaimID)+"/assess", req)
}

func (c *irmResourceClient) CoordinateAction(ctx context.Context, req coordinateActionRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/actions/"+url.PathEscape(req.ActionID)+"/coordinate", req)
}

func (c *irmResourceClient) VerifyObservation(ctx context.Context, req verifyObservationRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/observations/"+url.PathEscape(req.ObservationID)+"/verify", req)
}

func (c *irmResourceClient) AttachEvidence(ctx context.Context, req attachEvidenceRequest) (*attachEvidenceResponse, error) {
	return post[attachEvidenceResponse](ctx, c, c.base(req.Container)+"/evidence-links", req)
}

func (c *irmResourceClient) SetEvidenceVerification(ctx context.Context, req setEvidenceVerificationRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/evidence-links/"+url.PathEscape(req.EvidenceID)+"/verify", req)
}

func (c *irmResourceClient) WithdrawEvidence(ctx context.Context, req withdrawEvidenceRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/evidence-links/"+url.PathEscape(req.EvidenceID)+"/withdraw", req)
}

func (c *irmResourceClient) GetContainer(ctx context.Context, req getContainerRequest) (*containerBody, error) {
	return get[containerBody](ctx, c, c.base(req.Container), nil)
}

func (c *irmResourceClient) ListEvents(ctx context.Context, req listEventsRequest) (*eventsBody, error) {
	return get[eventsBody](ctx, c, c.base(req.Container)+"/events", req.query())
}

func (c *irmResourceClient) EditObservation(ctx context.Context, req editObservationRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/observations/"+url.PathEscape(req.ObservationID)+"/edit", req)
}

func (c *irmResourceClient) EditAction(ctx context.Context, req editActionRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/actions/"+url.PathEscape(req.ActionID)+"/edit", req)
}

func (c *irmResourceClient) UpdateRecovery(ctx context.Context, req updateRecoveryRequest) (*revisionResponse, error) {
	return post[revisionResponse](ctx, c, c.base(req.Container)+"/claims/"+url.PathEscape(req.ClaimID)+"/recovery", req)
}
