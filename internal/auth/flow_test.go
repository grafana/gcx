package auth_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"reflect"
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

func TestBrowserCommand(t *testing.T) {
	// A representative OAuth URL: the query string carries & separators that
	// cmd.exe would treat as command separators (#814).
	const url = "https://mystack.grafana.net/a/grafana-assistant-app/cli/auth?callback_port=54321&state=abc&code_challenge=xyz&code_challenge_method=S256"

	tests := []struct {
		name     string
		goos     string
		wantArgs []string
	}{
		{"darwin", "darwin", []string{"open", url}},
		{"linux", "linux", []string{"xdg-open", url}},
		{"windows", "windows", []string{"explorer.exe", url}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := auth.BrowserCommand(context.Background(), tt.goos, url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(cmd.Args, tt.wantArgs) {
				t.Fatalf("args = %v, want %v", cmd.Args, tt.wantArgs)
			}
			// Regression for #814: the full URL must be passed as a single,
			// intact argument — never split or truncated at an & separator.
			if got := cmd.Args[len(cmd.Args)-1]; got != url {
				t.Fatalf("url argument = %q, want %q", got, url)
			}
		})
	}
}

func TestBrowserCommand_UnsupportedPlatform(t *testing.T) {
	if _, err := auth.BrowserCommand(context.Background(), "plan9", "https://mystack.grafana.net"); err == nil {
		t.Fatal("expected error for unsupported platform")
	}
}
