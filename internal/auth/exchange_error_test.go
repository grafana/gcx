package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/auth"
)

func TestExchangeCodeForToken_ErrorFormat(t *testing.T) {
	secretBody := `{"error":"bad_request","secret":"do-not-leak"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(secretBody))
	}))
	defer server.Close()

	_, err := auth.ExchangeCodeForToken(context.Background(), server.URL, "code", "verifier")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("error must not contain response body, got: %v", err)
	}
	if !strings.Contains(err.Error(), "oauth token exchange failed") {
		t.Fatalf("error should match oauth format, got: %v", err)
	}
}

// TestExchangeCodeForToken_StatusIsTyped lets the CLI give recovery steps for
// rate limits and service errors without parsing the message.
func TestExchangeCodeForToken_StatusIsTyped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := auth.ExchangeCodeForToken(context.Background(), server.URL, "code", "verifier")
	var statusErr *auth.ExchangeStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected *auth.ExchangeStatusError, got %T: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", statusErr.StatusCode)
	}
}
