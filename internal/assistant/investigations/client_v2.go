package investigations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	v2ReportRegenFmt     = v2InvestigationsBase + "/%s/report/regenerate"
	v2MermaidUpdateFmt   = v2InvestigationsBase + "/%s/report/elements/%s/mermaid"
	v2MermaidRepairFmt   = v2InvestigationsBase + "/%s/report/elements/%s/mermaid/repair"
)

// pathFor returns the URL path for a v2 operation. `id` may be empty for
// collection-level paths (create, list).
func (c *Client) pathFor(op string, id string) string {
	switch op {
	case "create", "list":
		return v2InvestigationsBase
	case "resolve":
		return fmt.Sprintf(v2ResolveFmt, url.PathEscape(id))
	case "state":
		return fmt.Sprintf(v2SnapshotFmt, url.PathEscape(id))
	case "pause":
		return fmt.Sprintf(v2PauseFmt, url.PathEscape(id))
	case "resume":
		return fmt.Sprintf(v2ResumeFmt, url.PathEscape(id))
	case "mode":
		return fmt.Sprintf(v2ModeFmt, url.PathEscape(id))
	case "share":
		return fmt.Sprintf(v2ShareFmt, url.PathEscape(id))
	case "regen-report":
		return fmt.Sprintf(v2ReportRegenFmt, url.PathEscape(id))
	}
	panic(fmt.Sprintf("investigations: unknown v2 op %q", op))
}

// mermaidPath returns the URL for a mermaid update/repair endpoint. `verb` is
// "" for update (PUT) or "repair" for the LLM-repair route (POST).
func (c *Client) mermaidPath(investigationID, elementID, verb string) string {
	fmtStr := v2MermaidUpdateFmt
	if verb == "repair" {
		fmtStr = v2MermaidRepairFmt
	}
	return fmt.Sprintf(fmtStr, url.PathEscape(investigationID), url.PathEscape(elementID))
}

// CreateLodestone starts a new investigation against /api/v2/investigations.
func (c *Client) CreateLodestone(ctx context.Context, req CreateLodestoneRequest) (*CreateLodestoneResponse, error) {
	return doEnvelopeRequest[CreateLodestoneResponse](c, ctx, http.MethodPost, c.pathFor("create", ""), req, "create investigation")
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

	path := c.pathFor("list", "")
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
// Doesn't use doEnvelopeRequest because it surfaces the status code as a
// return value.
//
// Use the InvestigationID field for subsequent v2 endpoints; use the ChatID
// field for chat-thread reads on the v1 /chats/* surface.
func (c *Client) ResolveByID(ctx context.Context, id string) (ResolveByIDResponse, int, error) {
	path := c.pathFor("resolve", id)
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
	state, err := doEnvelopeRequest[LodestoneState](c, ctx, http.MethodGet, c.pathFor("state", id), nil, "get investigation state")
	if err != nil {
		return nil, err
	}
	return *state, nil
}

// Pause pauses a running investigation (non-terminal).
func (c *Client) Pause(ctx context.Context, id string) (*Message, error) {
	return doEnvelopeRequest[Message](c, ctx, http.MethodPost, c.pathFor("pause", id), nil, "pause investigation")
}

// Resume resumes a paused (or terminal) investigation.
func (c *Client) Resume(ctx context.Context, id string) (*Message, error) {
	return doEnvelopeRequest[Message](c, ctx, http.MethodPost, c.pathFor("resume", id), nil, "resume investigation")
}

// RegenerateReport queues an asynchronous report regeneration.
func (c *Client) RegenerateReport(ctx context.Context, id string) (*Message, error) {
	return doEnvelopeRequest[Message](c, ctx, http.MethodPost, c.pathFor("regen-report", id), nil, "regenerate investigation report")
}

// SetMode changes the autonomy mode of a running investigation.
func (c *Client) SetMode(ctx context.Context, id, mode string) (*ModeResponse, error) {
	return doEnvelopeRequest[ModeResponse](c, ctx, http.MethodPut, c.pathFor("mode", id), ModeRequest{Mode: mode}, "set investigation mode")
}

// Scope shares an investigation with additional teams (one-way, additive)
// via /api/v2 /share.
func (c *Client) Scope(ctx context.Context, id string, teamNames []string) (*ScopeResponse, error) {
	return doEnvelopeRequest[ScopeResponse](c, ctx, http.MethodPost, c.pathFor("share", id), ScopeRequest{TeamNames: teamNames}, "share investigation")
}

// UpdateMermaid persists repaired Mermaid source for a report element.
func (c *Client) UpdateMermaid(ctx context.Context, id, elementID, content string) (*UpdatedResponse, error) {
	return doEnvelopeRequest[UpdatedResponse](c, ctx, http.MethodPut, c.mermaidPath(id, elementID, ""), MermaidUpdateRequest{Content: content}, "update mermaid element")
}

// RepairMermaid asks the server to LLM-repair a broken Mermaid diagram.
func (c *Client) RepairMermaid(ctx context.Context, id, elementID, errMsg string) (*RepairResponse, error) {
	return doEnvelopeRequest[RepairResponse](c, ctx, http.MethodPost, c.mermaidPath(id, elementID, "repair"), MermaidRepairRequest{ErrorMessage: errMsg}, "repair mermaid element")
}

// doEnvelopeRequest is a generic helper that runs an HTTP call against the
// /api/v2 investigations surface and decodes the {"data": T} envelope. Pass nil for req when
// the request has no body. Accepts 200 OK or 201 Created. Callers that need
// to inspect the status code (e.g. ResolveByID) bypass this helper.
func doEnvelopeRequest[T any](c *Client, ctx context.Context, method, path string, req any, op string) (*T, error) {
	var body io.Reader
	if req != nil {
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal %s request: %w", op, err)
		}
		body = bytes.NewReader(data)
	}
	resp, err := c.base.DoRequest(ctx, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", op, err)
	}
	return &envelope.Data, nil
}
