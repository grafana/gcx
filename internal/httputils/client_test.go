package httputils_test

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/retry"
)

func TestNewDefaultClient_HasRetryAndLoggingTransport(t *testing.T) {
	client := httputils.NewDefaultClient(context.Background())
	// Outermost layer is UserAgentTransport.
	uaRT, ok := client.Transport.(*httputils.UserAgentTransport)
	if !ok {
		t.Fatalf("expected outermost Transport to be *httputils.UserAgentTransport, got %T", client.Transport)
	}
	// Next layer is retry.Transport.
	retryRT, ok := uaRT.Base.(*retry.Transport)
	if !ok {
		t.Fatalf("expected UserAgentTransport.Base to be *retry.Transport, got %T", uaRT.Base)
	}
	// Inner layer is LoggingRoundTripper.
	if _, ok := retryRT.Base.(*httputils.LoggingRoundTripper); !ok {
		t.Fatalf("expected retry.Transport.Base to be *httputils.LoggingRoundTripper, got %T", retryRT.Base)
	}
}

func TestNewDefaultClient_WithPayloadLogging(t *testing.T) {
	ctx := httputils.WithPayloadLogging(context.Background(), true)
	client := httputils.NewDefaultClient(ctx)
	// Outermost layer is UserAgentTransport.
	uaRT, ok := client.Transport.(*httputils.UserAgentTransport)
	if !ok {
		t.Fatalf("expected outermost Transport to be *httputils.UserAgentTransport, got %T", client.Transport)
	}
	// Next layer is retry.Transport.
	retryRT, ok := uaRT.Base.(*retry.Transport)
	if !ok {
		t.Fatalf("expected UserAgentTransport.Base to be *retry.Transport, got %T", uaRT.Base)
	}
	// Inner layer is RequestResponseLoggingRoundTripper (payload logging enabled).
	if _, ok := retryRT.Base.(*httputils.RequestResponseLoggingRoundTripper); !ok {
		t.Fatalf("expected retry.Transport.Base to be *httputils.RequestResponseLoggingRoundTripper, got %T", retryRT.Base)
	}
}

func TestNewClient_CustomMiddleware(t *testing.T) {
	client := httputils.NewClient(httputils.ClientOpts{
		Middlewares: []httputils.Middleware{httputils.RequestResponseLoggingMiddleware},
	})
	// Outermost layer is UserAgentTransport.
	uaRT, ok := client.Transport.(*httputils.UserAgentTransport)
	if !ok {
		t.Fatalf("expected outermost Transport to be *httputils.UserAgentTransport, got %T", client.Transport)
	}
	// Next layer is retry.Transport.
	retryRT, ok := uaRT.Base.(*retry.Transport)
	if !ok {
		t.Fatalf("expected UserAgentTransport.Base to be *retry.Transport, got %T", uaRT.Base)
	}
	// Inner layer is the custom middleware.
	if _, ok := retryRT.Base.(*httputils.RequestResponseLoggingRoundTripper); !ok {
		t.Fatalf("expected retry.Transport.Base to be *httputils.RequestResponseLoggingRoundTripper, got %T", retryRT.Base)
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
