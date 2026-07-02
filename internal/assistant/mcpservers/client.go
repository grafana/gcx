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

func (c *Client) List(ctx context.Context, opts ListOptions) ([]Server, error) {
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
		return nil, fmt.Errorf("failed to list MCP servers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data struct {
			Integrations []rawIntegration `json:"integrations"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode MCP servers: %w", err)
	}

	servers := make([]Server, 0, len(envelope.Data.Integrations))
	for _, item := range envelope.Data.Integrations {
		if strings.EqualFold(item.Type, IntegrationTypeMCP) {
			servers = append(servers, item.server())
		}
	}
	return servers, nil
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

	body, err := json.Marshal(payloadFromInput(input))
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
