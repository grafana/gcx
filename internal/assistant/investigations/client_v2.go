package investigations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

// v2-standard endpoint paths. Introduced by grafana-assistant-app#6645 as the
// official surface; keyed by {investigationId}.
const (
	v2InvestigationsBase = "/api/v2/investigations"
	v2ListPath           = v2InvestigationsBase
	v2ResolveFmt         = v2InvestigationsBase + "/%s"
	v2SnapshotFmt        = v2InvestigationsBase + "/%s/snapshot"
	v2PauseFmt           = v2InvestigationsBase + "/%s/pause"
	v2ResumeFmt          = v2InvestigationsBase + "/%s/resume"
	v2ModeFmt            = v2InvestigationsBase + "/%s/mode"
	v2ShareFmt           = v2InvestigationsBase + "/%s/share"
)

// CreateLodestone starts a new investigation against /api/v2/investigations.
func (c *Client) CreateLodestone(ctx context.Context, req CreateLodestoneRequest) (*CreateLodestoneResponse, error) {
	return assistanthttp.DoEnvelopeRequest[CreateLodestoneResponse](c.base, ctx, http.MethodPost, v2InvestigationsBase, req, "create investigation")
}

// ListLodestone returns v2 investigation summaries. The envelope is the same
// shape as the v1 summary endpoint, so the existing InvestigationSummary type
// and ListTableCodec are reused.
func (c *Client) ListLodestone(ctx context.Context, opts ListLodestoneOptions) ([]InvestigationSummary, error) {
	params := url.Values{}
	if opts.State != "" {
		params.Set("state", opts.State)
	}
	if opts.Q != "" {
		params.Set("q", opts.Q)
	}
	if opts.Scope != "" {
		params.Set("scope", opts.Scope)
	}
	if opts.TeamName != "" {
		params.Set("teamName", opts.TeamName)
	}
	if opts.From != "" {
		params.Set("from", opts.From)
	}
	if opts.To != "" {
		params.Set("to", opts.To)
	}
	if opts.Sort != "" {
		params.Set("sort", opts.Sort)
	}
	if opts.Order != "" {
		params.Set("order", opts.Order)
	}
	if opts.View != "" {
		params.Set("view", opts.View)
	}
	if opts.Label != "" {
		params.Set("label", opts.Label)
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", strconv.Itoa(opts.Offset))
	}
	if opts.IncludeLegacy {
		params.Set("includeLegacy", "true")
	}

	path := v2ListPath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list investigations: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}
	var envelope struct {
		Data struct {
			Investigations []InvestigationSummary `json:"investigations"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}
	if envelope.Data.Investigations == nil {
		return []InvestigationSummary{}, nil
	}
	return envelope.Data.Investigations, nil
}

// ResolveByID maps a user-supplied investigation identifier to both forms
// (investigationId and chatId). Returns the HTTP status so callers (e.g. get)
// can fall back to v1 on 404 without needing to inspect the wrapped error.
//
// Use the InvestigationID field for subsequent v2 endpoints; use the ChatID
// field for chat-thread reads on the v1 /chats/* surface.
func (c *Client) ResolveByID(ctx context.Context, id string) (ResolveByIDResponse, int, error) {
	path := fmt.Sprintf(v2ResolveFmt, url.PathEscape(id))
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return ResolveByIDResponse{}, 0, fmt.Errorf("resolve investigation %s: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ResolveByIDResponse{}, http.StatusNotFound, nil
	}
	if resp.StatusCode != http.StatusOK {
		return ResolveByIDResponse{}, resp.StatusCode, assistanthttp.HandleErrorResponse(resp)
	}
	var envelope struct {
		Data ResolveByIDResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return ResolveByIDResponse{}, resp.StatusCode, fmt.Errorf("decode resolve response: %w", err)
	}
	return envelope.Data, http.StatusOK, nil
}

// GetState returns the full session state for the given investigation id via
// the /api/v2 /snapshot endpoint.
func (c *Client) GetState(ctx context.Context, id string) (LodestoneState, error) {
	path := fmt.Sprintf(v2SnapshotFmt, url.PathEscape(id))
	state, err := assistanthttp.DoEnvelopeRequest[LodestoneState](c.base, ctx, http.MethodGet, path, nil, "get investigation state")
	if err != nil {
		return nil, err
	}
	return *state, nil
}

// Pause pauses a running investigation (non-terminal).
func (c *Client) Pause(ctx context.Context, id string) (*Message, error) {
	path := fmt.Sprintf(v2PauseFmt, url.PathEscape(id))
	return assistanthttp.DoEnvelopeRequest[Message](c.base, ctx, http.MethodPost, path, nil, "pause investigation")
}

// Resume resumes a paused (or terminal) investigation.
func (c *Client) Resume(ctx context.Context, id string) (*Message, error) {
	path := fmt.Sprintf(v2ResumeFmt, url.PathEscape(id))
	return assistanthttp.DoEnvelopeRequest[Message](c.base, ctx, http.MethodPost, path, nil, "resume investigation")
}

// SetMode changes the autonomy mode of a running investigation.
func (c *Client) SetMode(ctx context.Context, id, mode string) (*ModeResponse, error) {
	path := fmt.Sprintf(v2ModeFmt, url.PathEscape(id))
	return assistanthttp.DoEnvelopeRequest[ModeResponse](c.base, ctx, http.MethodPut, path, ModeRequest{Mode: mode}, "set investigation mode")
}

// Scope shares an investigation with additional teams (one-way, additive)
// via /api/v2 /share.
func (c *Client) Scope(ctx context.Context, id string, teamNames []string) (*ScopeResponse, error) {
	path := fmt.Sprintf(v2ShareFmt, url.PathEscape(id))
	return assistanthttp.DoEnvelopeRequest[ScopeResponse](c.base, ctx, http.MethodPost, path, ScopeRequest{TeamNames: teamNames}, "share investigation")
}
