package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// refreshThreshold is how far before token expiry we trigger a proactive refresh.
const refreshThreshold = 5 * time.Minute

// TokenRefresher is called after a successful refresh to persist the new tokens.
type TokenRefresher func(token, refreshToken, expiresAt, refreshExpiresAt string) error

// RefreshTransport wraps an http.RoundTripper and transparently refreshes
// the gat_ access token when it is close to expiry.
type RefreshTransport struct {
	Base          http.RoundTripper
	ProxyEndpoint string
	Token         string
	RefreshToken  string
	ExpiresAt     time.Time
	OnRefresh     TokenRefresher

	mu         sync.Mutex
	refreshing bool
}

func (t *RefreshTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.maybeRefresh(req); err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	t.mu.Lock()
	token := t.Token
	t.mu.Unlock()

	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.base().RoundTrip(clone)
}

func (t *RefreshTransport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func (t *RefreshTransport) maybeRefresh(req *http.Request) error {
	t.mu.Lock()
	if t.RefreshToken == "" || time.Until(t.ExpiresAt) > refreshThreshold || t.refreshing {
		t.mu.Unlock()
		return nil
	}
	// Mark refreshing so concurrent callers skip the refresh.
	t.refreshing = true
	refreshToken := t.RefreshToken
	t.mu.Unlock()

	// Network call happens outside the lock.
	result, err := t.doRefresh(req.Context(), refreshToken)

	t.mu.Lock()
	t.refreshing = false
	if err != nil {
		t.mu.Unlock()
		return err
	}
	t.Token = result.Data.Token
	if result.Data.RefreshToken != "" {
		t.RefreshToken = result.Data.RefreshToken
	}
	parsed, parseErr := time.Parse(time.RFC3339, result.Data.ExpiresAt)
	if parseErr != nil {
		t.mu.Unlock()
		return fmt.Errorf("server returned unparseable expires_at %q: %w", result.Data.ExpiresAt, parseErr)
	}
	t.ExpiresAt = parsed
	storedRefresh := t.RefreshToken
	onRefresh := t.OnRefresh
	t.mu.Unlock()

	if onRefresh != nil {
		if err := onRefresh(
			result.Data.Token,
			storedRefresh,
			result.Data.ExpiresAt,
			result.Data.RefreshExpiresAt,
		); err != nil {
			return fmt.Errorf("failed to persist refreshed tokens: %w", err)
		}
	}

	return nil
}

type refreshResponse struct {
	Data struct {
		Token            string `json:"token"`
		ExpiresAt        string `json:"expires_at"`
		RefreshToken     string `json:"refresh_token"`
		RefreshExpiresAt string `json:"refresh_expires_at"`
	} `json:"data"`
}

func (t *RefreshTransport) doRefresh(ctx context.Context, refreshToken string) (*refreshResponse, error) {
	body, err := json.Marshal(map[string]string{
		"refresh_token": refreshToken,
	})
	if err != nil {
		return nil, err
	}

	refreshURL := t.ProxyEndpoint + "/api/cli/v1/auth/refresh"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.base().RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	limitedBody := io.LimitReader(resp.Body, maxResponseBytes)

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(limitedBody)
		return nil, fmt.Errorf("refresh returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result refreshResponse
	if err := json.NewDecoder(limitedBody).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	return &result, nil
}
