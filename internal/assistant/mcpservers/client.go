package mcpservers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

type baseClient interface {
	DoRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
	DoRequestWithHeaders(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error)
}

type Client struct {
	base baseClient
}

func NewClient(base *assistanthttp.Client) *Client {
	return &Client{base: base}
}

// defaultPageSize is the page size ListAll and ListBounded fall back to when
// the caller does not specify a positive ListOptions.Limit.
const defaultPageSize = 100

// fetchPage performs a single offset-paginated request against the
// underlying (unfiltered) integrations list. It returns the raw integrations
// for that page (before MCP filtering) and the total reported across ALL
// assistant integrations (0 if the response does not report one). The total
// is not MCP-specific -- MCP servers are narrowed client-side, not by the
// list request itself -- so callers MUST NOT present it as an MCP-server
// count (FR-016).
func (c *Client) fetchPage(ctx context.Context, opts ListOptions) ([]rawIntegration, int, error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", strconv.Itoa(opts.Offset))
	}
	path := "/api/v1/integrations"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list MCP servers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data struct {
			Integrations []rawIntegration `json:"integrations"`
			Total        int              `json:"total"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, 0, fmt.Errorf("failed to decode MCP servers: %w", err)
	}
	return envelope.Data.Integrations, envelope.Data.Total, nil
}

func filterMCPServers(raw []rawIntegration) []Server {
	servers := make([]Server, 0, len(raw))
	for _, item := range raw {
		if strings.EqualFold(item.Type, IntegrationTypeMCP) {
			servers = append(servers, item.server())
		}
	}
	return servers
}

// List returns the MCP servers on a single page of the underlying
// integration list, filtered client-side. It does not page beyond what opts
// requests -- used internally by Get/Find, which only need to search within
// one page.
func (c *Client) List(ctx context.Context, opts ListOptions) ([]Server, error) {
	raw, _, err := c.fetchPage(ctx, opts)
	if err != nil {
		return nil, err
	}
	return filterMCPServers(raw), nil
}

// ListAll exhausts every page of the underlying integration list and returns
// every MCP server across all pages. Exhaustion is driven by the raw
// (unfiltered) page size and the reported total, never by the MCP-filtered
// count on a given page -- a page can hold zero MCP servers while more
// integrations (and MCP servers) exist on later pages, so a filtered-count
// stop condition would truncate (FR-015). opts.Limit, when positive, sets
// the page size used for every request; it is not a cap on the number of
// servers returned. Used by the resources adapter (`pull`/`get`), which must
// never truncate a large stack.
func (c *Client) ListAll(ctx context.Context, opts ListOptions) ([]Server, error) {
	pageSize := opts.Limit
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	var servers []Server
	offset := opts.Offset
	for {
		raw, total, err := c.fetchPage(ctx, ListOptions{Limit: pageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		servers = append(servers, filterMCPServers(raw)...)
		offset += len(raw)
		if len(raw) < pageSize {
			break
		}
		if total > 0 && offset >= total {
			break
		}
	}
	return servers, nil
}

// BoundedList is a single bounded page of MCP servers plus whether more may
// exist on later pages of the underlying integration list.
type BoundedList struct {
	Servers []Server
	// Limit is the effective page size that was requested (after defaulting).
	Limit int
	// HasMore reports whether more integrations may exist beyond this page.
	// It is derived from the raw (unfiltered) page, never from the
	// MCP-filtered count, so it MAY be true even when this page contains no
	// MCP servers, and MAY under-represent MCP servers when a page is
	// dominated by non-MCP integrations (FR-016) -- acceptable for the human
	// path, since agents/GitOps use the exhausting ListAll instead.
	HasMore bool
}

// ListBounded fetches a single page of MCP servers without exhausting the
// underlying list, for the human `mcp-servers list` command (FR-016).
func (c *Client) ListBounded(ctx context.Context, opts ListOptions) (BoundedList, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}

	raw, total, err := c.fetchPage(ctx, ListOptions{Limit: limit, Offset: opts.Offset})
	if err != nil {
		return BoundedList{}, err
	}
	hasMore := len(raw) >= limit
	if total > 0 {
		hasMore = opts.Offset+len(raw) < total
	}
	return BoundedList{Servers: filterMCPServers(raw), Limit: limit, HasMore: hasMore}, nil
}

func (c *Client) Get(ctx context.Context, ref string) (*Server, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/api/v1/integrations/"+url.PathEscape(ref), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCP server %s: %w", ref, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		server, err := decodeServer(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to decode MCP server: %w", err)
		}
		if !strings.EqualFold(server.Type, IntegrationTypeMCP) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, ref)
		}
		return server, nil
	}
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	servers, err := c.List(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}
	nameMatches := []Server{}
	for _, server := range servers {
		if server.ID == ref {
			return &server, nil
		}
		if strings.EqualFold(server.Name, ref) {
			nameMatches = append(nameMatches, server)
		}
	}
	switch len(nameMatches) {
	case 0:
		return nil, fmt.Errorf("%w: %s", ErrNotFound, ref)
	case 1:
		return &nameMatches[0], nil
	default:
		return nil, AmbiguousReferenceError{Ref: ref, Matches: nameMatches}
	}
}

func (c *Client) Create(ctx context.Context, input ServerInput) (*MutationResult, error) {
	if err := input.Validate(true); err != nil {
		return nil, err
	}
	if scopeOrDefault(input.Scope) == "tenant" {
		if err := ValidateTenantAuthHeaders(input.Headers); err != nil {
			return nil, err
		}
	}
	body, err := json.Marshal(payloadFromInput(input))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP server create request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/api/v1/integrations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	result, err := decodeMutation(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode MCP server create response: %w", err)
	}
	result.Operation = "created"
	if result.Server == nil {
		server, getErr := c.Find(ctx, input)
		if getErr != nil {
			return nil, fmt.Errorf("failed to read back created MCP server: %w", getErr)
		}
		result.Server = server
	}
	return result, nil
}

// Find locates a server matching the input's name, URL, and scope. It matches
// on all three fields rather than name alone, so a user-scoped and
// tenant-scoped server sharing a name do not collide. Used to read back a
// just-created server when the create response omits the integration payload,
// and by --if-not-exists to decide whether the requested server already
// exists. Returns ErrNotFound when no server matches.
func (c *Client) Find(ctx context.Context, input ServerInput) (*Server, error) {
	servers, err := c.List(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}
	wantScope := scopeOrDefault(input.Scope)
	for i := range servers {
		s := servers[i]
		if strings.EqualFold(s.Name, input.Name) && s.URL == input.URL && scopeOrDefault(s.Scope) == wantScope {
			return &servers[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, input.Name)
}

func (c *Client) Update(ctx context.Context, ref string, input ServerInput) (*MutationResult, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("server id or name is required")
	}
	current, err := c.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if input.Name == "" {
		input.Name = current.Name
	}
	if input.Description == "" {
		input.Description = current.Description
	}
	if input.URL == "" {
		input.URL = current.URL
	}
	if len(input.Config) == 0 {
		input.Config = current.Configuration
	}
	if input.Scope == "" {
		input.Scope = current.Scope
	}
	if len(input.Applications) == 0 {
		input.Applications = current.Applications
	}
	if input.Enabled == nil {
		input.Enabled = &current.Enabled
	}
	if err := input.Validate(false); err != nil {
		return nil, err
	}
	if scopeOrDefault(current.Scope) != "tenant" && scopeOrDefault(input.Scope) == "tenant" {
		if err := ValidateTenantAuthHeaders(input.Headers); err != nil {
			return nil, err
		}
	}

	headerWrites := HeaderWritesForUpdate(input.Headers, current.CustomHeaders)
	body, err := json.Marshal(payloadFromInputForUpdate(input, headerWrites))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP server update request: %w", err)
	}
	resp, err := c.base.DoRequestWithHeaders(ctx, http.MethodPut, "/api/v1/integrations/"+url.PathEscape(current.ID), bytes.NewReader(body), scopeHeader(current.Scope))
	if err != nil {
		return nil, fmt.Errorf("failed to update MCP server %s: %w", ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	result, err := decodeMutation(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode MCP server update response: %w", err)
	}
	result.Operation = "updated"
	if result.Server == nil {
		if server, getErr := c.Get(ctx, current.ID); getErr == nil {
			result.Server = server
		}
	}
	return result, nil
}

func (c *Client) Delete(ctx context.Context, ref string) (*MutationResult, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("server id or name is required")
	}
	current, err := c.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	resp, err := c.base.DoRequestWithHeaders(ctx, http.MethodDelete, "/api/v1/integrations/"+url.PathEscape(current.ID), nil, scopeHeader(current.Scope))
	if err != nil {
		return nil, fmt.Errorf("failed to delete MCP server %s: %w", ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}
	return &MutationResult{Operation: "deleted", Server: current}, nil
}

func (c *Client) Validate(ctx context.Context, ref string) (*ValidationResult, error) {
	current, err := c.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	return c.ValidateByID(ctx, current.ID)
}

// ValidateByID validates a server without re-resolving the ref. Callers that
// already hold the resolved ID (e.g. right after create/update) use this to
// avoid an extra Get round trip.
func (c *Client) ValidateByID(ctx context.Context, id string) (*ValidationResult, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/api/v1/integrations/"+url.PathEscape(id)+"/validate", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to validate MCP server %s: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data struct {
			Result ValidationResult `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode MCP server validation response: %w", err)
	}
	return &envelope.Data.Result, nil
}

func (c *Client) InitiateOAuth(ctx context.Context, ref string) (*OAuthResult, error) {
	current, err := c.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	return c.InitiateOAuthByID(ctx, current.ID, current.Scope)
}

// InitiateOAuthByID starts the OAuth flow without re-resolving the ref. Callers
// that already hold the resolved ID and scope use this to avoid an extra Get
// round trip.
func (c *Client) InitiateOAuthByID(ctx context.Context, id, scope string) (*OAuthResult, error) {
	if scope == "" {
		scope = defaultScope
	}
	body, err := json.Marshal(map[string]string{
		"integration_id": id,
		"scope":          scope,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP server OAuth request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/api/v1/integrations/oauth/initiate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to initiate MCP server OAuth for %s: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data struct {
			AuthURL string `json:"auth_url"`
			State   string `json:"state"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode MCP server OAuth response: %w", err)
	}
	return &OAuthResult{AuthURL: envelope.Data.AuthURL, State: envelope.Data.State}, nil
}

func payloadFromInput(input ServerInput) map[string]any {
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	scope := input.Scope
	if scope == "" {
		scope = defaultScope
	}
	applications := input.Applications
	if len(applications) == 0 {
		applications = []string{"assistant"}
	}

	cfg := map[string]any{}
	maps.Copy(cfg, input.Config)
	cfg["url"] = input.URL

	payload := map[string]any{
		"type":          IntegrationTypeMCP,
		"name":          input.Name,
		"description":   input.Description,
		"enabled":       enabled,
		"scope":         scope,
		"applications":  applications,
		"configuration": cfg,
	}
	if len(input.Headers) > 0 {
		payload["custom_headers"] = input.Headers
	}
	return payload
}

// HeaderIntent is the per-header write intent Update sends to the backend:
// whether a header's stored value is set, left untouched, or dropped
// entirely. Client callers still supply headers as a plain []Header on
// ServerInput -- Update derives HeaderWrite/HeaderIntent internally via
// HeaderWritesForUpdate. The exported primitives exist so other callers
// (notably the future MCPServer adapter, T6) can classify a header list the
// same way without re-deriving the overwrite/preserve/remove rules, and so
// the classification's shape is a documented, testable cross-task contract.
type HeaderIntent string

const (
	// HeaderIntentOverwrite sets (or creates) the header to Value.
	HeaderIntentOverwrite HeaderIntent = "overwrite"
	// HeaderIntentPreserve leaves an existing stored header value untouched.
	// Only meaningful when the header already exists on the server being
	// updated -- there is nothing to preserve on a create path.
	HeaderIntentPreserve HeaderIntent = "preserve"
	// HeaderIntentRemove drops the header entirely.
	HeaderIntentRemove HeaderIntent = "remove"
)

// HeaderWrite is one header's desired write intent at the client boundary.
// Value is only meaningful when Intent is HeaderIntentOverwrite.
type HeaderWrite struct {
	Name   string
	Value  string
	Intent HeaderIntent
}

// HeaderWritesForUpdate derives the full per-header write-intent list for an
// Update call from a caller's desired header list and the server's
// currently stored headers (name-only; values are redacted on read).
// Exported so callers other than Update -- notably the future MCPServer
// adapter (T6), which must classify a manifest's header list the same way
// before deciding whether a name-only header on a create path is an error
// (FR-019) -- can reuse the exact same classification instead of
// re-deriving it.
//
// Contract (FR-017/FR-018): desired == nil means the caller did not touch
// headers at all (e.g. no --header flags on the CLI) -- every current
// header is preserved untouched, which is what fixes the tenant-update
// header drop (PR #747 Major #1). A non-nil desired, even if empty, is
// treated as the full desired state: each entry with a non-empty Value
// overwrites (or creates) that header, each entry with an empty Value
// preserves the existing stored value for that name, and any current
// header whose name is absent from desired is removed.
func HeaderWritesForUpdate(desired []Header, current []ServerHeader) []HeaderWrite {
	if desired == nil {
		writes := make([]HeaderWrite, 0, len(current))
		for _, h := range current {
			writes = append(writes, HeaderWrite{Name: h.Name, Intent: HeaderIntentPreserve})
		}
		return writes
	}

	desiredNames := make(map[string]struct{}, len(desired))
	writes := make([]HeaderWrite, 0, len(desired))
	for _, h := range desired {
		desiredNames[strings.ToLower(h.Name)] = struct{}{}
		if h.Value == "" {
			writes = append(writes, HeaderWrite{Name: h.Name, Intent: HeaderIntentPreserve})
		} else {
			writes = append(writes, HeaderWrite{Name: h.Name, Value: h.Value, Intent: HeaderIntentOverwrite})
		}
	}
	for _, h := range current {
		if _, ok := desiredNames[strings.ToLower(h.Name)]; !ok {
			writes = append(writes, HeaderWrite{Name: h.Name, Intent: HeaderIntentRemove})
		}
	}
	return writes
}

// headerWire is the wire shape for one header entry in an Update payload.
// Value is omitted (rather than sent as an empty string) to signal
// HeaderIntentPreserve to the backend, per the API's per-header write-intent
// support (ADR-021 Decision 5) -- this encoding is an assumption against a
// closed-source backend and is centralized here so it can be revisited in
// one place if the wire contract differs.
type headerWire struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// headersWireDesired renders the full desired header list for the Update
// payload. HeaderIntentRemove entries are omitted entirely -- the backend's
// update replaces the whole header set with whatever is sent, so "absent"
// is how a header is removed.
func headersWireDesired(writes []HeaderWrite) []headerWire {
	out := make([]headerWire, 0, len(writes))
	for _, w := range writes {
		if w.Intent == HeaderIntentRemove {
			continue
		}
		out = append(out, headerWire{Name: w.Name, Value: w.Value})
	}
	return out
}

// payloadFromInputForUpdate builds the Update request body, overriding
// payloadFromInput's custom_headers with the full desired write-intent list
// so an update never silently drops headers the caller did not intend to
// touch (FR-017).
func payloadFromInputForUpdate(input ServerInput, headers []HeaderWrite) map[string]any {
	payload := payloadFromInput(input)
	wire := headersWireDesired(headers)
	if len(wire) > 0 {
		payload["custom_headers"] = wire
	} else {
		delete(payload, "custom_headers")
	}
	return payload
}

func scopeOrDefault(scope string) string {
	if scope == "" {
		return defaultScope
	}
	return scope
}

func scopeHeader(scope string) map[string]string {
	return map[string]string{"X-Resource-Scope": scopeOrDefault(scope)}
}

func decodeServer(r io.Reader) (*Server, error) {
	var envelope struct {
		Data rawIntegration `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&envelope); err != nil {
		return nil, err
	}
	server := envelope.Data.server()
	return &server, nil
}

func decodeMutation(r io.Reader) (*MutationResult, error) {
	var envelope struct {
		Data struct {
			Integration rawIntegration `json:"integration"`
			AuthURL     string         `json:"authUrl"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&envelope); err != nil {
		return nil, err
	}
	result := &MutationResult{AuthURL: envelope.Data.AuthURL}
	if raw := envelope.Data.Integration; raw.ID != "" || raw.Name != "" {
		server := raw.server()
		result.Server = &server
	}
	return result, nil
}

type rawIntegration struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Type          string         `json:"type"`
	Enabled       bool           `json:"enabled"`
	Scope         string         `json:"scope"`
	Applications  []string       `json:"applications"`
	Configuration map[string]any `json:"configuration"`
	CustomHeaders []rawHeader    `json:"custom_headers"`
	Created       time.Time      `json:"created"`
	Modified      time.Time      `json:"modified"`
	CreatedBy     string         `json:"createdBy"`
	UpdatedBy     string         `json:"updatedBy"`
	UserID        string         `json:"userId"`
}

func (r rawIntegration) server() Server {
	customHeaders := make([]ServerHeader, 0, len(r.CustomHeaders))
	for _, header := range r.CustomHeaders {
		customHeaders = append(customHeaders, ServerHeader{
			Name:            header.Name,
			ValueConfigured: header.Value != "",
		})
	}
	return Server{
		ID:            r.ID,
		Name:          r.Name,
		Description:   r.Description,
		Type:          r.Type,
		Enabled:       r.Enabled,
		Scope:         r.Scope,
		URL:           stringValue(r.Configuration, "url"),
		BuiltinID:     stringValue(r.Configuration, "builtinId"),
		Applications:  r.Applications,
		CustomHeaders: customHeaders,
		Created:       r.Created,
		Modified:      r.Modified,
		CreatedBy:     r.CreatedBy,
		UpdatedBy:     r.UpdatedBy,
		UserID:        r.UserID,
		Configuration: r.Configuration,
	}
}

type rawHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	v, ok := values[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
