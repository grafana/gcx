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
