package auth_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
)

func TestValidateEndpointURL_AcceptsTrustedDomains(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"grafana.net", "https://mystack.grafana.net"},
		{"grafana-dev.net", "https://mystack.grafana-dev.net"},
		{"grafana-ops.net", "https://mystack.grafana-ops.net"},
		{"localhost", "http://127.0.0.1:3000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := auth.ValidateEndpointURL(tt.endpoint); err != nil {
				t.Fatalf("expected %q to be accepted, got error: %v", tt.endpoint, err)
			}
		})
	}
}

func TestValidateEndpointURL_RejectsUntrustedDomains(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"random domain", "https://evil.example.com"},
		{"http non-local", "http://mystack.grafana.net"},
		{"subdomain bypass", "https://evil.grafana.net.attacker.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := auth.ValidateEndpointURL(tt.endpoint); err == nil {
				t.Fatalf("expected %q to be rejected", tt.endpoint)
			}
		})
	}
}

func TestGCOMFlowRun_RejectsUntrustedURL(t *testing.T) {
	tests := []struct {
		name    string
		gcomURL string
	}{
		{"random domain", "https://evil.example.com"},
		{"http non-local", "http://grafana.com"},
		{"stack endpoint", "https://mystack.grafana.net"},
		{"subdomain bypass", "https://grafana.com.attacker.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var writer bytes.Buffer
			flow := auth.NewGCOMFlow(auth.GCOMOptions{
				GCOMURL: tt.gcomURL,
				Writer:  &writer,
			})

			_, err := flow.Run(context.Background())
			if err == nil {
				t.Fatalf("expected %q to be rejected", tt.gcomURL)
			}
			if !strings.Contains(err.Error(), "invalid GCOM URL") {
				t.Fatalf("expected invalid GCOM URL error, got %v", err)
			}
			if writer.Len() != 0 {
				t.Fatalf("expected no browser instructions before validation failure, got %q", writer.String())
			}
		})
	}
}

func TestIsGCOMHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"prod portal", "grafana.com", true},
		{"dev portal", "grafana-dev.com", true},
		{"ops portal", "grafana-ops.com", true},
		{"uppercase portal", "GRAFANA.COM", true},
		{"portal subdomain", "help.grafana.com", false},
		{"stack host", "mystack.grafana.net", false},
		{"lookalike", "grafana.com.attacker.com", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := auth.IsGCOMHost(tt.host); got != tt.want {
				t.Fatalf("IsGCOMHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// A Cloud portal root has no grafana-assistant-app route. The flow must refuse
// it instead of opening a browser on a page that does not exist.
func TestFlowRun_RejectsCloudPortalEndpointBeforeBrowserOutput(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"prod portal", "https://grafana.com"},
		{"dev portal", "https://grafana-dev.com"},
		{"ops portal", "https://grafana-ops.com"},
		{"portal with a trailing slash", "https://grafana.com/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var writer bytes.Buffer
			flow := auth.NewFlow(tt.endpoint, auth.Options{Writer: &writer})

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			if _, err := flow.Run(ctx); err == nil {
				t.Fatalf("expected %q to be rejected", tt.endpoint)
			} else if !strings.Contains(err.Error(), "is a Grafana Cloud portal, not a Grafana stack") {
				t.Fatalf("expected a portal error, got %v", err)
			}
			if writer.Len() != 0 {
				t.Fatalf("expected no browser instructions before the portal check, got %q", writer.String())
			}
		})
	}
}

func TestFlowRun_FailsBeforeBrowserOutputWhenFixedPortUnavailable(t *testing.T) {
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve callback port: %v", err)
	}
	defer func() { _ = listener.Close() }()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("expected *net.TCPAddr from listener")
	}
	port := tcpAddr.Port
	var writer bytes.Buffer
	flow := auth.NewFlow("https://mystack.grafana.net", auth.Options{
		Port:   port,
		Writer: &writer,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err = flow.Run(ctx)
	if err == nil {
		t.Fatal("expected fixed callback port conflict to fail")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("callback port %d unavailable", port)) {
		t.Fatalf("expected unavailable port error for %d, got %v", port, err)
	}
	if writer.Len() != 0 {
		t.Fatalf("expected no browser instructions before bind failure, got %q", writer.String())
	}
}
