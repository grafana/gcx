package httputils_test

import (
	"context"
	"fmt"
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
	client := httputils.NewClient(t.Context(), httputils.ClientOpts{
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
	client := httputils.NewClient(t.Context(), httputils.ClientOpts{
		Timeout: 10 * time.Second,
	})
	if client.Timeout != 10*time.Second {
		t.Fatalf("expected 10s timeout, got %v", client.Timeout)
	}
}

func TestNewClient_DisableRetry(t *testing.T) {
	client := httputils.NewClient(t.Context(), httputils.ClientOpts{DisableRetry: true})
	uaRT, ok := client.Transport.(*httputils.UserAgentTransport)
	if !ok {
		t.Fatalf("expected outermost Transport to be *httputils.UserAgentTransport, got %T", client.Transport)
	}
	// With retry disabled, User-Agent wraps the middlewares directly.
	if _, ok := uaRT.Base.(*httputils.LoggingRoundTripper); !ok {
		t.Fatalf("expected UserAgentTransport.Base to be *httputils.LoggingRoundTripper, got %T", uaRT.Base)
	}
}

func TestNewClient_PayloadLoggingFromContext(t *testing.T) {
	tests := []struct {
		name        string
		middlewares []httputils.Middleware
		wantOuter   string // type of the layer directly under the User-Agent transport
	}{
		{name: "default middlewares", middlewares: nil, wantOuter: "*httputils.LoggingRoundTripper"},
		{name: "explicit middlewares", middlewares: []httputils.Middleware{httputils.LoggingMiddleware}, wantOuter: "*httputils.LoggingRoundTripper"},
		{name: "empty middlewares", middlewares: []httputils.Middleware{}, wantOuter: "*httputils.RequestResponseLoggingRoundTripper"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := httputils.WithPayloadLogging(t.Context(), true)
			client := httputils.NewClient(ctx, httputils.ClientOpts{Middlewares: tt.middlewares, DisableRetry: true})

			uaRT, ok := client.Transport.(*httputils.UserAgentTransport)
			if !ok {
				t.Fatalf("expected outermost Transport to be *httputils.UserAgentTransport, got %T", client.Transport)
			}
			if got := fmt.Sprintf("%T", uaRT.Base); got != tt.wantOuter {
				t.Fatalf("expected UserAgentTransport.Base to be %s, got %s", tt.wantOuter, got)
			}
			// The payload dump is always the innermost layer, so it shows every
			// header that reaches the wire.
			rt := uaRT.Base
			if logRT, ok := rt.(*httputils.LoggingRoundTripper); ok {
				rt = logRT.Base
			}
			if _, ok := rt.(*httputils.RequestResponseLoggingRoundTripper); !ok {
				t.Fatalf("expected innermost middleware to be *httputils.RequestResponseLoggingRoundTripper, got %T", rt)
			}
		})
	}
}

func TestNewClient_NoPayloadLoggingByDefault(t *testing.T) {
	client := httputils.NewClient(t.Context(), httputils.ClientOpts{Middlewares: []httputils.Middleware{}, DisableRetry: true})
	uaRT, ok := client.Transport.(*httputils.UserAgentTransport)
	if !ok {
		t.Fatalf("expected outermost Transport to be *httputils.UserAgentTransport, got %T", client.Transport)
	}
	if _, ok := uaRT.Base.(*httputils.RequestResponseLoggingRoundTripper); ok {
		t.Fatal("expected no RequestResponseLoggingRoundTripper without --insecure-log-http-payload")
	}
}
