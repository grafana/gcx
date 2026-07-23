package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	// ErrRefreshTokenExpired is returned when the refresh token has expired and
	// the user must re-authenticate.
	ErrRefreshTokenExpired = errors.New("refresh token expired: re-authentication required")
	// ErrRefreshTokenMissing is returned when an access token needs renewal but
	// there is no refresh token with which to renew it.
	ErrRefreshTokenMissing = errors.New("OAuth refresh token is missing: re-authentication required")
	// ErrTokenGenerationChanged means the persisted refresh-token generation no
	// longer matches the token that produced a pending rotated generation.
	ErrTokenGenerationChanged = errors.New("OAuth token generation changed")
	// ErrInvalidRefreshResponse means a successful HTTP refresh response omitted
	// or malformed fields required to safely use the rotated token generation.
	ErrInvalidRefreshResponse = errors.New("OAuth refresh response is invalid")
)

// refreshThreshold is how far before token expiry we trigger a proactive refresh.
const refreshThreshold = 5 * time.Minute

// TokenRefresher is called after a successful refresh to persist the new
// tokens. previousRefreshToken is the exact generation presented to the
// refresh endpoint and lets persistence perform a compare-and-swap.
type TokenRefresher func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error

// TokenLocker acquires a cross-process lock around the refresh/persist cycle
// and returns a non-nil release function. Any error or nil release fails the
// refresh closed before the rotating refresh token is presented.
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

	mu       sync.Mutex
	inflight *refreshFlight
	pending  *pendingRefresh
	blocked  error
}

type refreshFlight struct {
	done chan struct{}
	err  error
}

// pendingRefresh is a successful rotating-token response that has not yet
// been durably persisted. It must be retried before the token can be exposed
// to any caller, and must never trigger a second network refresh in this
// process. This is deliberately not a durable journal: if the process exits
// before persistence succeeds, the rotated generation is lost and the user
// may need to re-authenticate.
type pendingRefresh struct {
	previousRefreshToken string
	token                string
	refreshToken         string
	expiresAt            string
	refreshExpiresAt     string
	persist              TokenRefresher
	afterPersistErr      error
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

// FreshToken returns a usable OAuth access token, refreshing and persisting it
// through the same fail-closed Lock/Reload/OnRefresh lifecycle used by
// RoundTrip when the current token is near expiry.
func (t *RefreshTransport) FreshToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/", nil)
	if err != nil {
		return "", err
	}
	if err := t.maybeRefresh(req); err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Token, nil
}

func (t *RefreshTransport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

// adoptFreshStoredTokens reloads tokens from disk and, if they're both usable
// and different from what we hold in memory, adopts them and returns true.
// A different but near-expiry token is still adopted and returns false so the
// network refresh uses the latest persisted refresh token. A zero access-token
// expiry means the issuer did not provide one; that token is usable without a
// proactive refresh.
func (t *RefreshTransport) adoptFreshStoredTokens() (bool, error) {
	if t.Reload == nil {
		return false, nil
	}
	stored, ok, err := t.Reload()
	if err != nil {
		return false, fmt.Errorf("reload refreshed tokens: %w", err)
	}
	if !ok || stored.RefreshToken == "" {
		return false, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if stored.RefreshToken == t.RefreshToken && stored.Token == t.Token {
		return false, nil
	}
	t.Token = stored.Token
	t.RefreshToken = stored.RefreshToken
	t.ExpiresAt = stored.ExpiresAt
	t.RefreshExpiresAt = stored.RefreshExpiresAt
	return stored.Token != "" && (stored.ExpiresAt.IsZero() || time.Until(stored.ExpiresAt) > refreshThreshold), nil
}

func (t *RefreshTransport) maybeRefresh(req *http.Request) error {
	t.mu.Lock()
	if t.blocked != nil {
		err := t.blocked
		t.mu.Unlock()
		return err
	}

	if t.pending == nil {
		if t.Token != "" && (t.ExpiresAt.IsZero() || time.Until(t.ExpiresAt) > refreshThreshold) {
			t.mu.Unlock()
			return nil
		}
		if t.RefreshToken == "" {
			t.mu.Unlock()
			return ErrRefreshTokenMissing
		}
		if !t.RefreshExpiresAt.IsZero() && time.Now().After(t.RefreshExpiresAt) {
			t.mu.Unlock()
			return ErrRefreshTokenExpired
		}
		if err := req.Context().Err(); err != nil {
			t.mu.Unlock()
			return err
		}
	}

	// Every waiter observes the exact outcome of the flight it joined. In
	// particular, lock, reload, network, and persistence failures cannot be
	// hidden from goroutines that arrived behind the leader.
	if flight := t.inflight; flight != nil {
		t.mu.Unlock()
		select {
		case <-flight.done:
			return flight.err
		case <-req.Context().Done():
			return req.Context().Err()
		}
	}

	flight := &refreshFlight{done: make(chan struct{})}
	t.inflight = flight
	t.mu.Unlock()

	err := t.refreshAndPersist(req)
	t.mu.Lock()
	flight.err = err
	if t.inflight == flight {
		t.inflight = nil
	}
	close(flight.done)
	t.mu.Unlock()
	return err
}

func (t *RefreshTransport) refreshAndPersist(req *http.Request) error {
	t.mu.Lock()
	hasPending := t.pending != nil
	t.mu.Unlock()

	// Serialize with other gcx processes sharing this config file, so the
	// rotating refresh token is never consumed by two callers at once. A retry
	// of an already-rotated pending generation must outlive caller cancellation;
	// an initial refresh still honors cancellation while waiting for the lock.
	if t.Lock != nil {
		lockCtx := req.Context()
		if hasPending {
			lockCtx = context.WithoutCancel(lockCtx)
		}
		release, err := t.Lock(lockCtx)
		if err != nil {
			return fmt.Errorf("lock OAuth token persistence: %w", err)
		}
		if release == nil {
			return errors.New("lock OAuth token persistence: lock callback returned no release function")
		}
		defer release()
	}

	// A previous network response may already have rotated the refresh token
	// while its persistence callback failed. Retry only that callback under the
	// persistence lock; issuing another refresh would consume an unpersisted
	// generation and make crash recovery impossible.
	if hasPending {
		return t.persistPendingRefresh()
	}
	if err := req.Context().Err(); err != nil {
		return err
	}

	// If another process already refreshed while we waited for the lock,
	// adopt its tokens. A near-expiry replacement is adopted but then refreshed
	// with its latest refresh token; a fresh replacement skips the network call.
	adopted, err := t.adoptFreshStoredTokens()
	if err != nil {
		return err
	}
	if err := req.Context().Err(); err != nil {
		return err
	}
	if adopted {
		return nil
	}

	t.mu.Lock()
	refreshToken := t.RefreshToken
	refreshExpiresAt := t.RefreshExpiresAt
	t.mu.Unlock()
	if refreshToken == "" {
		return errors.New("OAuth refresh token is missing after persistence reload")
	}
	if !refreshExpiresAt.IsZero() && time.Now().After(refreshExpiresAt) {
		return ErrRefreshTokenExpired
	}
	if err := req.Context().Err(); err != nil {
		return err
	}

	// Detach from the caller's context: if the refresh is already in flight,
	// the server may have consumed and rotated the refresh token. Aborting
	// now would leave us with a stale token on disk and a locked-out user.
	// A bounded timeout still protects against a hung proxy.
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 30*time.Second)
	defer cancel()

	// Network call happens outside the in-process mutex.
	result, err := t.doProxyRefresh(refreshCtx, refreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidRefreshResponse) {
			t.blockConsumedGeneration(err)
		}
		return err
	}
	expiresAt, refreshExpiresAt, validationErr := validateRefreshResult(result)
	hasReplacementRefresh := strings.TrimSpace(result.RefreshToken) != ""
	if validationErr != nil && !hasReplacementRefresh {
		// A 200 response may mean the old refresh token was consumed even when
		// the response does not contain a replacement generation. Never retry or
		// persist that old generation in this process.
		t.blockConsumedGeneration(validationErr)
		return validationErr
	}

	// A successful refresh response must never be dropped. Rotation-capable
	// proxies return a new refresh generation; non-rotating proxies may return
	// the existing one. Retain the response and persist it with the same CAS
	// boundary in either case.
	t.mu.Lock()
	persistedToken := result.Token
	persistedExpiresAt := canonicalOptionalExpiry(result.ExpiresAt)
	persistedRefreshExpiresAt := canonicalOptionalExpiry(result.RefreshExpiresAt)
	if validationErr != nil {
		// Preserve the replacement refresh generation even if another required
		// field is invalid. A stale access expiry forces the next process to
		// recover by refreshing with that replacement instead of exposing a
		// malformed access generation.
		if strings.TrimSpace(persistedToken) == "" {
			persistedToken = t.Token
		}
		persistedExpiresAt = time.Unix(0, 0).UTC().Format(time.RFC3339)
		if refreshExpiresAt.IsZero() {
			persistedRefreshExpiresAt = ""
		}
		expiresAt = time.Unix(0, 0).UTC()
	}
	t.Token = persistedToken
	t.RefreshToken = result.RefreshToken
	t.ExpiresAt = expiresAt
	t.RefreshExpiresAt = refreshExpiresAt
	if t.OnRefresh != nil {
		t.pending = &pendingRefresh{
			previousRefreshToken: refreshToken,
			token:                persistedToken,
			refreshToken:         result.RefreshToken,
			expiresAt:            persistedExpiresAt,
			refreshExpiresAt:     persistedRefreshExpiresAt,
			persist:              t.OnRefresh,
			afterPersistErr:      validationErr,
		}
	} else if validationErr != nil {
		t.blocked = validationErr
	}
	hasPending = t.pending != nil
	t.mu.Unlock()

	if hasPending {
		return t.persistPendingRefresh()
	}
	return validationErr
}

func (t *RefreshTransport) blockConsumedGeneration(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Token = ""
	t.RefreshToken = ""
	t.ExpiresAt = time.Time{}
	t.RefreshExpiresAt = time.Time{}
	t.blocked = err
}

func validateRefreshResult(result RefreshResult) (time.Time, time.Time, error) {
	invalid := make([]string, 0, 4)
	if strings.TrimSpace(result.Token) == "" {
		invalid = append(invalid, "token")
	}
	if strings.TrimSpace(result.RefreshToken) == "" {
		invalid = append(invalid, "refresh_token")
	}
	expiresAt, err := parseOptionalExpiry(result.ExpiresAt)
	if err != nil {
		invalid = append(invalid, "expires_at")
	}
	refreshExpiresAt, err := parseOptionalExpiry(result.RefreshExpiresAt)
	if err != nil {
		invalid = append(invalid, "refresh_expires_at")
	}
	if len(invalid) > 0 {
		return expiresAt, refreshExpiresAt, fmt.Errorf("%w: missing or malformed %s", ErrInvalidRefreshResponse, strings.Join(invalid, ", "))
	}
	return expiresAt, refreshExpiresAt, nil
}

// parseOptionalExpiry treats a blank issuer timestamp as unknown. OAuth token
// responses may omit expiry metadata; the credential is still usable when both
// token generations are present. A nonblank malformed timestamp remains an
// invalid response rather than being confused with an omitted value.
func parseOptionalExpiry(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func canonicalOptionalExpiry(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return value
}

func (t *RefreshTransport) persistPendingRefresh() error {
	t.mu.Lock()
	pending := t.pending
	t.mu.Unlock()
	if pending == nil {
		return nil
	}
	if err := pending.persist(
		pending.previousRefreshToken,
		pending.token,
		pending.refreshToken,
		pending.expiresAt,
		pending.refreshExpiresAt,
	); err != nil {
		if errors.Is(err, ErrTokenGenerationChanged) {
			// A deliberate re-login or a newer refresh won the compare-and-swap.
			// Drop only this obsolete pending generation and force the next call
			// through Reload so it can adopt the persisted winner. Clearing the
			// obsolete access token is the force-reload signal; a zero expiry alone
			// means a valid token whose issuer omitted expiry metadata.
			t.mu.Lock()
			if t.pending == pending {
				t.pending = nil
				t.Token = ""
				t.ExpiresAt = time.Time{}
			}
			t.mu.Unlock()
		}
		return fmt.Errorf("failed to persist refreshed tokens: %w", err)
	}

	t.mu.Lock()
	if t.pending == pending {
		t.pending = nil
		if pending.afterPersistErr != nil {
			t.blocked = pending.afterPersistErr
		}
	}
	t.mu.Unlock()
	return pending.afterPersistErr
}

type proxyRefreshResponse struct {
	Data struct {
		Token            string `json:"token"`
		ExpiresAt        string `json:"expires_at"`
		RefreshToken     string `json:"refresh_token"`
		RefreshExpiresAt string `json:"refresh_expires_at"`
	} `json:"data"`
}

func (t *RefreshTransport) doProxyRefresh(ctx context.Context, refreshToken string) (RefreshResult, error) {
	body, err := json.Marshal(map[string]string{
		"refresh_token": refreshToken,
	})
	if err != nil {
		return RefreshResult{}, err
	}

	refreshURL := t.ProxyEndpoint + "/api/cli/v1/auth/refresh"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(body))
	if err != nil {
		return RefreshResult{}, fmt.Errorf("failed to build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.base().RoundTrip(req)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("refresh request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	limitedBody := io.LimitReader(resp.Body, maxResponseBytes)

	// The response body is not included in errors: a refresh endpoint can echo
	// tokens or other credentials in its body, which must not leak into logs.
	if resp.StatusCode == http.StatusUnauthorized {
		return RefreshResult{}, fmt.Errorf("oauth refresh failed: status %d: %w", resp.StatusCode, ErrRefreshTokenExpired)
	}
	if resp.StatusCode != http.StatusOK {
		return RefreshResult{}, fmt.Errorf("oauth refresh failed: status %d", resp.StatusCode)
	}

	var result proxyRefreshResponse
	if err := json.NewDecoder(limitedBody).Decode(&result); err != nil {
		return RefreshResult{}, fmt.Errorf("%w: failed to parse response body", ErrInvalidRefreshResponse)
	}

	return RefreshResult{
		Token:            result.Data.Token,
		RefreshToken:     result.Data.RefreshToken,
		ExpiresAt:        result.Data.ExpiresAt,
		RefreshExpiresAt: result.Data.RefreshExpiresAt,
	}, nil
}

// RefreshResult holds the token credentials returned by a successful refresh.
type RefreshResult struct {
	Token            string
	RefreshToken     string
	ExpiresAt        string
	RefreshExpiresAt string
}

// ProxyRefresh calls the proxy refresh endpoint and returns new token credentials.
func ProxyRefresh(ctx context.Context, proxyEndpoint, refreshTok string) (RefreshResult, error) {
	t := &RefreshTransport{ProxyEndpoint: proxyEndpoint}
	return t.doProxyRefresh(ctx, refreshTok)
}
