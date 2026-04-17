package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// ErrRefreshTokenExpired is returned when the refresh token has expired and
// the user must re-authenticate.
var ErrRefreshTokenExpired = errors.New("refresh token expired: re-authentication required")

// refreshThreshold is how far before token expiry we trigger a proactive refresh.
const refreshThreshold = 5 * time.Minute

// TokenRefresher is called after a successful refresh to persist the new tokens.
type TokenRefresher func(token, refreshToken, expiresAt, refreshExpiresAt string) error

// TokenLocker acquires a cross-process lock around the refresh/persist cycle
// and returns a release function. Returning a nil release and an error causes
// the refresh to proceed without a lock (best-effort).
type TokenLocker func(ctx context.Context) (release func(), err error)

// StoredTokens describes tokens currently on disk.
type StoredTokens struct {
	Token            string
	RefreshToken     string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
}

// TokenReloader reads the latest tokens from disk. Returns false if no
// persisted tokens are available.
type TokenReloader func() (StoredTokens, bool, error)

// RefreshTransport wraps an http.RoundTripper and transparently refreshes
// the gat_ access token when it is close to expiry.
type RefreshTransport struct {
	Base             http.RoundTripper
	ProxyEndpoint    string
	Token            string
	RefreshToken     string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
	OnRefresh        TokenRefresher

	// Lock, if set, is called before a refresh to serialize concurrent gcx
	// invocations that share a config file. Without it, two processes race to
	// refresh the same rotating refresh token and one gets locked out.
	Lock TokenLocker
	// Reload, if set, is called inside the lock before issuing the network
	// refresh. If another process has already refreshed, its tokens are
	// adopted and the network refresh is skipped.
	Reload TokenReloader

	mu         sync.Mutex
	cond       *sync.Cond
	refreshing bool
}

func (t *RefreshTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Provider requests may carry their own BasicAuth credentials.
	if req.Header.Get("Authorization") != "" {
		return t.base().RoundTrip(req)
	}

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

func (t *RefreshTransport) initCond() {
	if t.cond == nil {
		t.cond = sync.NewCond(&t.mu)
	}
}

func (t *RefreshTransport) maybeRefresh(req *http.Request) error {
	t.mu.Lock()
	t.initCond()

	if t.RefreshToken == "" || time.Until(t.ExpiresAt) > refreshThreshold {
		t.mu.Unlock()
		return nil
	}

	if !t.RefreshExpiresAt.IsZero() && time.Now().After(t.RefreshExpiresAt) {
		t.mu.Unlock()
		return ErrRefreshTokenExpired
	}

	// Another goroutine is already refreshing — wait for it to finish.
	if t.refreshing {
		for t.refreshing {
			t.cond.Wait()
		}
		t.mu.Unlock()
		return nil
	}

	t.refreshing = true
	t.mu.Unlock()

	// Signal refresh-complete to in-process waiters no matter how we exit.
	defer func() {
		t.mu.Lock()
		t.refreshing = false
		t.cond.Broadcast()
		t.mu.Unlock()
	}()

	// Serialize with other gcx processes holding the same config file, so the
	// rotating refresh token is never consumed by two callers at once.
	var release func()
	if t.Lock != nil {
		r, lerr := t.Lock(req.Context())
		if lerr == nil && r != nil {
			release = r
			defer release()
		}
	}

	// After acquiring the cross-process lock, re-read disk state. If another
	// process already refreshed while we were waiting, adopt its tokens and
	// skip the network refresh entirely — presenting our now-stale refresh
	// token would get us a 401 and a real lockout. We only adopt when the
	// stored access token is actually fresh; otherwise the on-disk tokens
	// are just a stale snapshot and we should proceed with our own refresh.
	if t.Reload != nil {
		if stored, ok, _ := t.Reload(); ok && stored.RefreshToken != "" && time.Until(stored.ExpiresAt) > refreshThreshold {
			t.mu.Lock()
			if stored.RefreshToken != t.RefreshToken {
				t.Token = stored.Token
				t.RefreshToken = stored.RefreshToken
				t.ExpiresAt = stored.ExpiresAt
				t.RefreshExpiresAt = stored.RefreshExpiresAt
				t.mu.Unlock()
				return nil
			}
			t.mu.Unlock()
		}
	}

	t.mu.Lock()
	refreshToken := t.RefreshToken
	t.mu.Unlock()

	// Network call happens outside the lock.
	result, err := t.doRefresh(req.Context(), refreshToken)

	t.mu.Lock()
	if err != nil {
		t.mu.Unlock()
		return err
	}

	// A successful refresh response must never be dropped: the server has
	// already consumed the old refresh token and rotated it, so discarding
	// the response would leave the client with an invalid refresh token.
	t.Token = result.Data.Token
	if result.Data.RefreshToken != "" {
		t.RefreshToken = result.Data.RefreshToken
	}
	if parsed, err := time.Parse(time.RFC3339, result.Data.ExpiresAt); err == nil {
		t.ExpiresAt = parsed
	} else {
		// Unparseable expiry: fall back to zero time so the next request re-refreshes.
		t.ExpiresAt = time.Time{}
	}
	if rp, err := time.Parse(time.RFC3339, result.Data.RefreshExpiresAt); err == nil {
		t.RefreshExpiresAt = rp
	}
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

	if resp.StatusCode == http.StatusUnauthorized {
		respBody, _ := io.ReadAll(limitedBody)
		return nil, fmt.Errorf("refresh returned status %d: %s: %w", resp.StatusCode, string(respBody), ErrRefreshTokenExpired)
	}
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

// DoRefresh calls the proxy refresh endpoint and returns new token credentials.
// This is used by the assistant command's token refresher, which needs to refresh
// tokens outside of an HTTP round-trip context.
// RefreshResult holds the token credentials returned by a successful refresh.
type RefreshResult struct {
	Token            string
	RefreshToken     string
	ExpiresAt        string
	RefreshExpiresAt string
}

// DoRefresh calls the proxy refresh endpoint and returns new token credentials.
// This is used by the assistant command's token refresher, which needs to refresh
// tokens outside of an HTTP round-trip context.
func DoRefresh(ctx context.Context, proxyEndpoint, refreshTok string) (RefreshResult, error) {
	t := &RefreshTransport{ProxyEndpoint: proxyEndpoint}
	result, err := t.doRefresh(ctx, refreshTok)
	if err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{
		Token:            result.Data.Token,
		RefreshToken:     result.Data.RefreshToken,
		ExpiresAt:        result.Data.ExpiresAt,
		RefreshExpiresAt: result.Data.RefreshExpiresAt,
	}, nil
}
