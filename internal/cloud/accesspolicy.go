package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Cloud Access Policies + Tokens API paths (grafana.com/api/v1/...).
const (
	accessPoliciesPath = "/api/v1/accesspolicies"
	tokensPath         = "/api/v1/tokens" // #nosec G101 -- API resource path, not a credential.
)

// Realm scopes an access policy to a specific org or stack.
type Realm struct {
	// Type is "stack" or "org".
	Type string `json:"type"`
	// Identifier is the stack ID (for type "stack") or org ID (for type "org").
	Identifier string `json:"identifier"`
}

// AccessPolicy is a Grafana Cloud access policy as returned by the GCOM
// /v1/accesspolicies endpoints.
type AccessPolicy struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName,omitempty"`
	Scopes      []string `json:"scopes"`
	Realms      []Realm  `json:"realms"`
	Status      string   `json:"status,omitempty"`
}

// CreateAccessPolicyRequest is the body for POST /v1/accesspolicies.
type CreateAccessPolicyRequest struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName,omitempty"`
	Scopes      []string `json:"scopes"`
	Realms      []Realm  `json:"realms"`
}

// Token is a Grafana Cloud access-policy token. The secret Token field is only
// populated in the response to CreateToken.
type Token struct {
	ID             string `json:"id"`
	AccessPolicyID string `json:"accessPolicyId"`
	Name           string `json:"name"`
	DisplayName    string `json:"displayName,omitempty"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
	Token          string `json:"token,omitempty"`
}

// CreateTokenRequest is the body for POST /v1/tokens.
type CreateTokenRequest struct {
	AccessPolicyID string `json:"accessPolicyId"`
	Name           string `json:"name"`
	DisplayName    string `json:"displayName,omitempty"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
}

// CreateAccessPolicy creates an access policy in the given region. A 409
// conflict (name already exists) surfaces as a *GCOMHTTPError so callers can
// fall back to ListAccessPolicies.
func (c *GCOMClient) CreateAccessPolicy(ctx context.Context, region string, req CreateAccessPolicyRequest) (AccessPolicy, error) {
	var out AccessPolicy
	q := url.Values{"region": {region}}
	if err := c.doJSON(ctx, http.MethodPost, accessPoliciesPath, q, req, &out); err != nil {
		return AccessPolicy{}, err
	}
	return out, nil
}

// ListAccessPolicies returns the access policies in the given region (first
// page, up to the API default page size). Callers filter by name/realm
// client-side because the list endpoint does not support a name filter.
func (c *GCOMClient) ListAccessPolicies(ctx context.Context, region string) ([]AccessPolicy, error) {
	var out struct {
		Items []AccessPolicy `json:"items"`
	}
	q := url.Values{"region": {region}}
	if err := c.doJSON(ctx, http.MethodGet, accessPoliciesPath, q, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// CreateToken creates a token for an access policy in the given region. The
// returned Token.Token holds the secret and is only available here.
func (c *GCOMClient) CreateToken(ctx context.Context, region string, req CreateTokenRequest) (Token, error) {
	var out Token
	q := url.Values{"region": {region}}
	if err := c.doJSON(ctx, http.MethodPost, tokensPath, q, req, &out); err != nil {
		return Token{}, err
	}
	return out, nil
}

// ListTokens returns tokens in the given region, optionally filtered by access
// policy ID and token name (both server-side filters).
func (c *GCOMClient) ListTokens(ctx context.Context, region, accessPolicyID, name string) ([]Token, error) {
	var out struct {
		Items []Token `json:"items"`
	}
	q := url.Values{"region": {region}}
	if accessPolicyID != "" {
		q.Set("accessPolicyId", accessPolicyID)
	}
	if name != "" {
		q.Set("name", name)
	}
	if err := c.doJSON(ctx, http.MethodGet, tokensPath, q, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// DeleteToken deletes a token by ID in the given region.
func (c *GCOMClient) DeleteToken(ctx context.Context, region, tokenID string) error {
	q := url.Values{"region": {region}}
	return c.doJSON(ctx, http.MethodDelete, tokensPath+"/"+url.PathEscape(tokenID), q, nil, nil)
}

// doJSON performs a GCOM request with an optional JSON body and optional query
// params, decoding a 2xx response into out (when non-nil). Non-2xx responses
// are returned as *GCOMHTTPError.
func (c *GCOMClient) doJSON(ctx context.Context, method, rawPath string, q url.Values, body, out any) error {
	endpoint, err := c.buildURL(rawPath)
	if err != nil {
		return err
	}
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("gcom client: marshal request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gcom client: read response body: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return &GCOMHTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("gcom client: decode response: %w", err)
		}
	}
	return nil
}
