package httputils_test

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/httputils"
)

func TestNewDefaultClient_HasLoggingTransport(t *testing.T) {
	client := httputils.NewDefaultClient(context.Background())
	if _, ok := client.Transport.(*httputils.LoggingRoundTripper); !ok {
		t.Fatalf("expected Transport to be *httputils.LoggingRoundTripper, got %T", client.Transport)
	}
}

func TestNewDefaultClient_WithPayloadLogging(t *testing.T) {
	ctx := httputils.WithPayloadLogging(context.Background(), true)
	client := httputils.NewDefaultClient(ctx)
	// Outermost middleware is RequestResponseLoggingRoundTripper
	if _, ok := client.Transport.(*httputils.RequestResponseLoggingRoundTripper); !ok {
		t.Fatalf("expected outermost Transport to be *httputils.RequestResponseLoggingRoundTripper, got %T", client.Transport)
	}
}

func TestNewClient_CustomMiddleware(t *testing.T) {
	client := httputils.NewClient(httputils.ClientOpts{
		Middlewares: []httputils.Middleware{httputils.RequestResponseLoggingMiddleware},
	})
	if _, ok := client.Transport.(*httputils.RequestResponseLoggingRoundTripper); !ok {
		t.Fatalf("expected Transport to be *httputils.RequestResponseLoggingRoundTripper, got %T", client.Transport)
	}
}

func TestNewClient_DefaultTimeout(t *testing.T) {
	client := httputils.NewDefaultClient(context.Background())
	if client.Timeout != 60*time.Second {
		t.Fatalf("expected 60s timeout, got %v", client.Timeout)
	}
}

func TestNewClient_CustomTimeout(t *testing.T) {
	client := httputils.NewClient(httputils.ClientOpts{
		Timeout: 10 * time.Second,
	})
	if client.Timeout != 10*time.Second {
		t.Fatalf("expected 10s timeout, got %v", client.Timeout)
	}
}
