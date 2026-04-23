// Package gcxlib provides a public API for embedding gcx in other Go programs.
//
// Instead of shelling out to the gcx binary, callers can import this package
// and call [Execute] with pre-configured auth. The injected [Config] bypasses
// all file-based config loading — auth is provided via a custom HTTP transport.
package gcxlib

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/version"
	"k8s.io/client-go/rest"
)

// Config holds the parameters for an embedded gcx execution.
type Config struct {
	// GrafanaURL is the user-facing Grafana server URL (e.g., "https://mystack.grafana.net").
	GrafanaURL string

	// Namespace is the Kubernetes-style namespace (e.g., "stacks-12345").
	Namespace string

	// WrapTransport wraps the base HTTP transport with authentication.
	// The returned RoundTripper should apply auth headers to outgoing requests.
	WrapTransport func(http.RoundTripper) http.RoundTripper
}

// Result contains the captured output of an embedded gcx execution.
type Result struct {
	Stdout []byte
	Stderr []byte
}

// Execute runs a gcx command with pre-injected configuration.
//
// Args should be the command arguments without the "gcx" prefix
// (e.g., []string{"alert", "rules", "list"}).
//
// The injected Config bypasses all file-based config loading. Auth is provided
// via WrapTransport, which is applied to every HTTP request gcx makes.
// Output is always JSON (agent mode is forced).
func Execute(ctx context.Context, args []string, cfg Config) (*Result, error) {
	agent.SetFlag(true)

	host := strings.TrimSuffix(cfg.GrafanaURL, "/")
	nrc := config.NamespacedRESTConfig{
		Config: rest.Config{
			UserAgent: version.UserAgent(),
			Host:      host,
			APIPath:   "/apis",
			QPS:       50,
			Burst:     100,
		},
		Namespace:  cfg.Namespace,
		GrafanaURL: host,
	}
	if cfg.WrapTransport != nil {
		nrc.Config.WrapTransport = cfg.WrapTransport
	}

	ctx = config.WithNamespacedRESTConfig(ctx, nrc)

	cmd := root.Command("embedded")

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(ctx)

	return &Result{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}, err
}
