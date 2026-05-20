package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CloudRefreshTransportConfig holds the parameters for building a RefreshTransport
// that refreshes tokens via the GCOM OAuth2 token endpoint.
type CloudRefreshTransportConfig struct {
	Base             http.RoundTripper
	GCOMURL          string
	ClientID         string
	Token            string
	RefreshToken     string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
}

// NewCloudRefreshTransport returns a RefreshTransport that refreshes tokens
// via the GCOM OAuth2 endpoint instead of the proxy refresh endpoint.
func NewCloudRefreshTransport(cfg CloudRefreshTransportConfig) *RefreshTransport {
	rt := &RefreshTransport{
		Base:             cfg.Base,
		Token:            cfg.Token,
		RefreshToken:     cfg.RefreshToken,
		ExpiresAt:        cfg.ExpiresAt,
		RefreshExpiresAt: cfg.RefreshExpiresAt,
	}

	rt.DoRefresh = func(ctx context.Context, refreshToken string) (RefreshResult, error) {
		return doGCOMRefresh(ctx, rt.base(), cfg.GCOMURL, cfg.ClientID, refreshToken)
	}

	return rt
}

// gcomRefreshResponse holds the parsed response from a GCOM token refresh.
type gcomRefreshResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresAt string `json:"refresh_expires_at"`
	ExpiresIn        int    `json:"expires_in"`
}

func doGCOMRefresh(ctx context.Context, base http.RoundTripper, gcomURL, clientID, refreshToken string) (RefreshResult, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": refreshToken,
	})
	if err != nil {
		return RefreshResult{}, err
	}

	refreshURL := gcomURL + "/api/oauth2/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(body))
	if err != nil {
		return RefreshResult{}, fmt.Errorf("failed to build cloud refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := base.RoundTrip(req)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("cloud refresh request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	limitedBody := io.LimitReader(resp.Body, maxResponseBytes)

	if resp.StatusCode == http.StatusUnauthorized {
		respBody, _ := io.ReadAll(limitedBody)
		return RefreshResult{}, fmt.Errorf("cloud refresh returned status %d: %s: %w", resp.StatusCode, string(respBody), ErrRefreshTokenExpired)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(limitedBody)
		return RefreshResult{}, fmt.Errorf("cloud refresh returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result gcomRefreshResponse
	if err := json.NewDecoder(limitedBody).Decode(&result); err != nil {
		return RefreshResult{}, fmt.Errorf("failed to parse cloud refresh response: %w", err)
	}

	return RefreshResult{
		Token:            result.AccessToken,
		RefreshToken:     result.RefreshToken,
		ExpiresAt:        time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339),
		RefreshExpiresAt: result.RefreshExpiresAt,
	}, nil
}
