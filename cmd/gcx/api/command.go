package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
)

type apiOpts struct {
	IO      cmdio.Options
	Method  string
	Data    string
	Headers []string
}

func (opts *apiOpts) setup(flags *pflag.FlagSet) {
	opts.IO.DefaultFormat("json")
	opts.IO.BindFlags(flags)

	if f := flags.Lookup("output"); f != nil {
		f.Usage = "Output format for JSON responses. One of: json, yaml"
	}

	flags.StringVarP(&opts.Method, "method", "X", "", "HTTP method (default: GET, or POST if -d is set)")
	flags.StringVarP(&opts.Data, "data", "d", "", "Request body (use @file for file, @- for stdin). Implies POST.")
	flags.StringArrayVarP(&opts.Headers, "header", "H", nil, "Custom headers (repeatable)")
}

func (opts *apiOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Method != "" {
		method := strings.ToUpper(opts.Method)
		valid := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true, "TRACE": true}
		if !valid[method] {
			return fmt.Errorf("invalid method %q", opts.Method)
		}
	}
	return nil
}

func (opts *apiOpts) effectiveMethod() string {
	if opts.Method != "" {
		return strings.ToUpper(opts.Method)
	}
	if opts.Data != "" {
		return "POST"
	}
	return "GET"
}

// Command returns the api command.
func Command() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &apiOpts{}

	cmd := &cobra.Command{
		Use:   "api PATH",
		Short: "Make direct HTTP requests to the Grafana API",
		Long:  "Send arbitrary HTTP requests to any Grafana API endpoint using the configured authentication. Supports GET, POST, PUT, PATCH, DELETE with custom headers and request bodies.",
		Example: `  # List all datasources
  gcx api /api/datasources

  # Get a specific datasource by UID
  gcx api /api/datasources/uid/my-prometheus

  # Get Grafana health status
  gcx api /api/health

  # Create a folder (POST implied by -d)
  gcx api /api/folders -d '{"title":"My Folder"}'

  # Create a dashboard from a file
  gcx api /api/dashboards/db -d @dashboard.json

  # Delete a dashboard
  gcx api /api/dashboards/uid/my-dashboard -X DELETE

  # Output as YAML
  gcx api /api/datasources -o yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			path := args[0]
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}

			cfg, err := configOpts.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}

			body, err := resolveBody(cmd, opts.Data)
			if err != nil {
				return err
			}

			resp, err := doRequest(cmd.Context(), cfg, opts.effectiveMethod(), path, body, opts.Headers)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			return outputResponse(cmd, opts, resp)
		},
	}

	configOpts.BindFlags(cmd.PersistentFlags())
	opts.setup(cmd.Flags())
	return cmd
}

func resolveBody(cmd *cobra.Command, data string) (io.Reader, error) {
	if data == "" {
		return http.NoBody, nil
	}
	if data == "@-" {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("failed to read stdin: %w", err)
		}
		return bytes.NewReader(b), nil
	}
	if strings.HasPrefix(data, "@") {
		b, err := os.ReadFile(data[1:])
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		return bytes.NewReader(b), nil
	}
	return strings.NewReader(data), nil
}

func doRequest(ctx context.Context, cfg config.NamespacedRESTConfig, method, path string, body io.Reader, headers []string) (*http.Response, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, cfg.Host+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != http.NoBody {
		req.Header.Set("Content-Type", "application/json")
	}

	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header format %q", h)
		}
		req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}

	return httpClient.Do(req)
}

func outputResponse(cmd *cobra.Command, opts *apiOpts, resp *http.Response) error {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		if len(respBody) == 0 {
			return fmt.Errorf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Try to parse as JSON for structured output
	var data any
	if err := json.Unmarshal(respBody, &data); err != nil {
		// Not JSON - output as-is
		_, err := cmd.OutOrStdout().Write(respBody)
		return err
	}

	return opts.IO.Encode(cmd.OutOrStdout(), data)
}
