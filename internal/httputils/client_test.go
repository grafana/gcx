package httputils_test

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/host"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/retry"
)

func TestNewDefaultClient_HasRetryAndLoggingTransport(t *testing.T) {
	client := httputils.NewDefaultClient(context.Background())
	// Outermost layer is the sandbox access guard, then UserAgentTransport.
	guard, ok := client.Transport.(*host.GuardedTransport)
	if !ok {
		t.Fatalf("expected outermost Transport to be *host.GuardedTransport, got %T", client.Transport)
	}
	uaRT, ok := guard.Base.(*httputils.UserAgentTransport)
	if !ok {
		t.Fatalf("expected GuardedTransport.Base to be *httputils.UserAgentTransport, got %T", guard.Base)
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
	// Outermost layer is the sandbox access guard, then UserAgentTransport.
	guard, ok := client.Transport.(*host.GuardedTransport)
	if !ok {
		t.Fatalf("expected outermost Transport to be *host.GuardedTransport, got %T", client.Transport)
	}
	uaRT, ok := guard.Base.(*httputils.UserAgentTransport)
	if !ok {
		t.Fatalf("expected GuardedTransport.Base to be *httputils.UserAgentTransport, got %T", guard.Base)
	}
	// Next layer is retry.Transport.
	retryRT, ok := uaRT.Base.(*retry.Transport)
	if !ok {
		t.Fatalf("expected UserAgentTransport.Base to be *retry.Transport, got %T", uaRT.Base)
	}
	// Next layer is LoggingRoundTripper.
	logRT, ok := retryRT.Base.(*httputils.LoggingRoundTripper)
	if !ok {
		t.Fatalf("expected retry.Transport.Base to be *httputils.LoggingRoundTripper, got %T", retryRT.Base)
	}
	// Innermost layer is RequestResponseLoggingRoundTripper, so the dump shows
	// every header that an outer layer adds.
	if _, ok := logRT.Base.(*httputils.RequestResponseLoggingRoundTripper); !ok {
		t.Fatalf("expected LoggingRoundTripper.Base to be *httputils.RequestResponseLoggingRoundTripper, got %T", logRT.Base)
	}
}

func TestNewClient_CustomMiddleware(t *testing.T) {
	client := httputils.NewClient(httputils.ClientOpts{
		Middlewares: []httputils.Middleware{httputils.RequestResponseLoggingMiddleware},
	})
	// Outermost layer is the sandbox access guard, then UserAgentTransport.
	guard, ok := client.Transport.(*host.GuardedTransport)
	if !ok {
		t.Fatalf("expected outermost Transport to be *host.GuardedTransport, got %T", client.Transport)
	}
	uaRT, ok := guard.Base.(*httputils.UserAgentTransport)
	if !ok {
		t.Fatalf("expected GuardedTransport.Base to be *httputils.UserAgentTransport, got %T", guard.Base)
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
