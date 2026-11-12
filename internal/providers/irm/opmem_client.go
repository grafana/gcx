package irm

import (
	"context"
	"net/url"
)

type OpMemAPI interface {
	CreateObservation(ctx context.Context, req createObservationRequest) (*createObservationResponse, error)
	CreateClaim(ctx context.Context, req createClaimRequest) (*createClaimResponse, error)
	CreateAction(ctx context.Context, req createActionRequest) (*createActionResponse, error)
	PromoteToAction(ctx context.Context, req promoteToActionRequest) (*promoteToActionResponse, error)
	AttachEvidence(ctx context.Context, req attachEvidenceRequest) (*attachEvidenceResponse, error)
	GetContainer(ctx context.Context, req getContainerRequest) (*containerBody, error)
	ListEvents(ctx context.Context, req listEventsRequest) (*eventsBody, error)
}

var _ OpMemAPI = (*irmResourceClient)(nil)

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

func (c *irmResourceClient) PromoteToAction(ctx context.Context, req promoteToActionRequest) (*promoteToActionResponse, error) {
	return post[promoteToActionResponse](ctx, c, c.base(req.Container)+"/observations/"+url.PathEscape(req.ObservationID)+"/promote-to-action", req)
}

func (c *irmResourceClient) AttachEvidence(ctx context.Context, req attachEvidenceRequest) (*attachEvidenceResponse, error) {
	return post[attachEvidenceResponse](ctx, c, c.base(req.Container)+"/evidence-links", req)
}

func (c *irmResourceClient) GetContainer(ctx context.Context, req getContainerRequest) (*containerBody, error) {
	return get[containerBody](ctx, c, c.base(req.Container), nil)
}

func (c *irmResourceClient) ListEvents(ctx context.Context, req listEventsRequest) (*eventsBody, error) {
	return get[eventsBody](ctx, c, c.base(req.Container)+"/events", req.query())
}
