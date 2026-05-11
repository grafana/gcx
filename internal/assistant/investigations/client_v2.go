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

// v2 (Lodestone) endpoint paths.
const (
	lodestoneCreatePath       = "/investigations/lodestone"
	lodestoneListPath         = "/investigations/lodestone"
	lodestoneByIDFmt          = "/investigations/lodestone/by-id/%s"
	lodestoneStateFmt         = "/investigations/lodestone/%s/state"
	lodestonePauseFmt         = "/investigations/lodestone/%s/pause"
	lodestoneResumeFmt        = "/investigations/lodestone/%s/resume"
	lodestoneModeFmt          = "/investigations/lodestone/%s/mode"
	lodestoneScopeFmt         = "/investigations/lodestone/%s/scope"
	lodestoneRegenReportFmt   = "/investigations/lodestone/%s/regenerate-report"
	lodestoneMermaidUpdateFmt = "/investigations/lodestone/%s/report/elements/%s/mermaid"
	lodestoneMermaidRepairFmt = "/investigations/lodestone/%s/report/elements/%s/mermaid/repair"
)

// CreateLodestone starts a new Lodestone investigation.
func (c *Client) CreateLodestone(ctx context.Context, req CreateLodestoneRequest) (*CreateLodestoneResponse, error) {
	return doEnvelopeRequest[CreateLodestoneResponse](c, ctx, http.MethodPost, lodestoneCreatePath, req, "create lodestone investigation")
}

// ListLodestone returns Lodestone investigation summaries. The envelope is
// the same shape as the v1 summary endpoint, so the existing
// InvestigationSummary type and ListTableCodec are reused.
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

	path := lodestoneListPath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list lodestone investigations: %w", err)
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
		return nil, fmt.Errorf("decode lodestone list response: %w", err)
	}
	if envelope.Data.Investigations == nil {
		return []InvestigationSummary{}, nil
	}
	return envelope.Data.Investigations, nil
}

// ResolveByID maps an investigation ID to its chat ID. Returns the HTTP
// status so callers (e.g. get) can fall back to v1 on 404 without needing
// to inspect the wrapped error. Doesn't use doEnvelopeRequest because it
// surfaces the status code as a return value.
func (c *Client) ResolveByID(ctx context.Context, investigationID string) (string, int, error) {
	path := fmt.Sprintf(lodestoneByIDFmt, url.PathEscape(investigationID))
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", 0, fmt.Errorf("resolve lodestone investigation %s: %w", investigationID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", http.StatusNotFound, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, assistanthttp.HandleErrorResponse(resp)
	}
	var envelope struct {
		Data ResolveByIDResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", resp.StatusCode, fmt.Errorf("decode lodestone by-id response: %w", err)
	}
	return envelope.Data.ChatID, http.StatusOK, nil
}

// GetState returns the full Lodestone session state for the given chat ID.
func (c *Client) GetState(ctx context.Context, chatID string) (LodestoneState, error) {
	path := fmt.Sprintf(lodestoneStateFmt, url.PathEscape(chatID))
	state, err := doEnvelopeRequest[LodestoneState](c, ctx, http.MethodGet, path, nil, "get lodestone state")
	if err != nil {
		return nil, err
	}
	return *state, nil
}

// Pause pauses a running Lodestone investigation (non-terminal).
func (c *Client) Pause(ctx context.Context, chatID string) (*Message, error) {
	path := fmt.Sprintf(lodestonePauseFmt, url.PathEscape(chatID))
	return doEnvelopeRequest[Message](c, ctx, http.MethodPost, path, nil, "pause lodestone investigation")
}

// Resume resumes a paused (or terminal) Lodestone investigation.
func (c *Client) Resume(ctx context.Context, chatID string) (*Message, error) {
	path := fmt.Sprintf(lodestoneResumeFmt, url.PathEscape(chatID))
	return doEnvelopeRequest[Message](c, ctx, http.MethodPost, path, nil, "resume lodestone investigation")
}

// RegenerateReport queues an asynchronous report regeneration.
func (c *Client) RegenerateReport(ctx context.Context, chatID string) (*Message, error) {
	path := fmt.Sprintf(lodestoneRegenReportFmt, url.PathEscape(chatID))
	return doEnvelopeRequest[Message](c, ctx, http.MethodPost, path, nil, "regenerate lodestone report")
}

// SetMode changes the autonomy mode of a running Lodestone investigation.
func (c *Client) SetMode(ctx context.Context, chatID, mode string) (*ModeResponse, error) {
	path := fmt.Sprintf(lodestoneModeFmt, url.PathEscape(chatID))
	return doEnvelopeRequest[ModeResponse](c, ctx, http.MethodPut, path, ModeRequest{Mode: mode}, "set lodestone mode")
}

// Scope shares an investigation with additional teams (one-way, additive).
func (c *Client) Scope(ctx context.Context, investigationID string, teamNames []string) (*ScopeResponse, error) {
	path := fmt.Sprintf(lodestoneScopeFmt, url.PathEscape(investigationID))
	return doEnvelopeRequest[ScopeResponse](c, ctx, http.MethodPost, path, ScopeRequest{TeamNames: teamNames}, "share lodestone investigation")
}

// UpdateMermaid persists repaired Mermaid source for a report element.
func (c *Client) UpdateMermaid(ctx context.Context, chatID, elementID, content string) (*UpdatedResponse, error) {
	path := fmt.Sprintf(lodestoneMermaidUpdateFmt, url.PathEscape(chatID), url.PathEscape(elementID))
	return doEnvelopeRequest[UpdatedResponse](c, ctx, http.MethodPut, path, MermaidUpdateRequest{Content: content}, "update mermaid element")
}

// RepairMermaid asks the server to LLM-repair a broken Mermaid diagram.
func (c *Client) RepairMermaid(ctx context.Context, chatID, elementID, errMsg string) (*RepairResponse, error) {
	path := fmt.Sprintf(lodestoneMermaidRepairFmt, url.PathEscape(chatID), url.PathEscape(elementID))
	return doEnvelopeRequest[RepairResponse](c, ctx, http.MethodPost, path, MermaidRepairRequest{ErrorMessage: errMsg}, "repair mermaid element")
}

// doEnvelopeRequest is a generic helper that runs an HTTP call against the
// Lodestone API and decodes the {"data": T} envelope. Pass nil for req when
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
