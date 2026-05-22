package k6

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
)

const (
	// DefaultAPIDomain is the canonical k6 Cloud API endpoint.
	DefaultAPIDomain = "https://api.k6.io"

	authPath = "/v3/account/grafana-app/start"
)

// ReauthFunc refreshes the k6 auth credentials when a cached token is rejected.
// It must perform a fresh /start exchange, persist the new credentials, and
// return them so the caller can update the client's in-memory state.
type ReauthFunc func(ctx context.Context) (token string, orgID int, err error)

// DirectClient talks to api.k6.io directly using an SA-token-exchanged v3 token.
// It does NOT route through the grafana-k6-app plugin proxy — this path exists
// for stacks that cannot use OAuth (CI service accounts, headless automation).
type DirectClient struct {
	apiDomain string
	orgID     int
	stackID   int
	token     string
	http      *http.Client
	//nolint:unused // wired in Task 6 (SetReauth / doWithReauth)
	reauth ReauthFunc
}

// NewDirectClient creates a DirectClient. If apiDomain is empty, DefaultAPIDomain
// is used. If httpClient is nil, a default httputils client is created.
//
// The returned client is not authenticated until Authenticate or SetCachedAuth
// is called. Both Bearer + X-Stack-Id are injected on every subsequent request.
func NewDirectClient(ctx context.Context, apiDomain string, httpClient *http.Client) *DirectClient {
	if apiDomain == "" {
		apiDomain = DefaultAPIDomain
	}
	if httpClient == nil {
		httpClient = httputils.NewDefaultClient(ctx)
	}
	return &DirectClient{
		apiDomain: strings.TrimRight(apiDomain, "/"),
		http:      httpClient,
	}
}

// Authenticate exchanges a Grafana SA token (glsa_*) for a k6 v3 token by
// calling PUT /v3/account/grafana-app/start. The exchange uses
// X-Grafana-Service-Token; the CAP token does NOT work here (k6 backend
// rejects any header value longer than ~100 chars).
func (c *DirectClient) Authenticate(ctx context.Context, saToken string, stackID int) error {
	stackStr := strconv.Itoa(stackID)

	body, err := json.Marshal(struct{}{})
	if err != nil {
		return fmt.Errorf("k6: marshal auth request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.apiDomain+authPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("k6: create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Grafana-Service-Token", saToken)
	req.Header.Set("X-Stack-Id", stackStr)
	req.Header.Set("X-Grafana-User", "admin")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("k6: auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("k6: token exchange failed (PUT %s, status %d): %s", authPath, resp.StatusCode, string(respBody))
	}

	var ar authResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("k6: decode auth response: %w", err)
	}

	orgID, err := strconv.Atoi(ar.OrgID)
	if err != nil {
		return fmt.Errorf("k6: parse organization_id %q: %w", ar.OrgID, err)
	}

	c.orgID = orgID
	c.stackID = stackID
	c.token = ar.V3GrafanaToken
	return nil
}

// Token returns the v3 k6 token previously obtained via Authenticate or
// SetCachedAuth. The signature returns an error to satisfy the API interface
// (ProxyClient.Token is lazy and can fail); DirectClient.Token never errors
// in practice but returns errors.New(...) if the client has not been
// authenticated yet, to give callers a clear failure mode.
func (c *DirectClient) Token(_ context.Context) (string, error) {
	if c.token == "" {
		return "", errors.New("k6: DirectClient not authenticated (call Authenticate or SetCachedAuth first)")
	}
	return c.token, nil
}

// orgIDValue returns the cached organization ID, used internally by EnvVar methods.
// The plural form (orgID()) is reserved for ProxyClient's lazy variant.
//
//nolint:unused // called by resource methods added in Task 6
func (c *DirectClient) orgIDValue() int { return c.orgID }

// authResponse is the JSON body returned by PUT /v3/account/grafana-app/start.
type authResponse struct {
	OrgID          string `json:"organization_id"`
	V3GrafanaToken string `json:"v3_grafana_token"`
}
