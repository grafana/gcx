package mcpservers

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	IntegrationTypeMCP = "mcp"
	defaultScope       = "user"

	ValidationStatusOAuthRequired = "oauth_required"
)

var ErrNotFound = errors.New("MCP server not found")

type AmbiguousReferenceError struct {
	Ref     string
	Matches []Server
}

func (e AmbiguousReferenceError) Error() string {
	return fmt.Sprintf("ambiguous MCP server name %q matches %d servers; use the server ID", e.Ref, len(e.Matches))
}

type ListOptions struct {
	Limit  int
	Offset int
}

type Server struct {
	ID            string         `json:"id" yaml:"id"`
	Name          string         `json:"name" yaml:"name"`
	Description   string         `json:"description,omitempty" yaml:"description,omitempty"`
	Type          string         `json:"type" yaml:"type"`
	Enabled       bool           `json:"enabled" yaml:"enabled"`
	Scope         string         `json:"scope,omitempty" yaml:"scope,omitempty"`
	URL           string         `json:"url,omitempty" yaml:"url,omitempty"`
	BuiltinID     string         `json:"builtinId,omitempty" yaml:"builtinId,omitempty"`
	Applications  []string       `json:"applications,omitempty" yaml:"applications,omitempty"`
	CustomHeaders []ServerHeader `json:"customHeaders,omitempty" yaml:"customHeaders,omitempty"`
	Created       time.Time      `json:"created,omitzero" yaml:"created,omitempty"`
	Modified      time.Time      `json:"modified,omitzero" yaml:"modified,omitempty"`
	CreatedBy     string         `json:"createdBy,omitempty" yaml:"createdBy,omitempty"`
	UpdatedBy     string         `json:"updatedBy,omitempty" yaml:"updatedBy,omitempty"`
	UserID        string         `json:"userId,omitempty" yaml:"userId,omitempty"`
	Configuration map[string]any `json:"configuration,omitempty" yaml:"configuration,omitempty"`
}

type ServerHeader struct {
	Name            string `json:"name" yaml:"name"`
	ValueConfigured bool   `json:"valueConfigured" yaml:"valueConfigured"`
}

type Header struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type ServerInput struct {
	Name         string         `json:"name" yaml:"name"`
	Description  string         `json:"description,omitempty" yaml:"description,omitempty"`
	URL          string         `json:"url" yaml:"url"`
	Enabled      *bool          `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Scope        string         `json:"scope,omitempty" yaml:"scope,omitempty"`
	Headers      []Header       `json:"headers,omitempty" yaml:"headers,omitempty"`
	Applications []string       `json:"applications,omitempty" yaml:"applications,omitempty"`
	Config       map[string]any `json:"configuration,omitempty" yaml:"configuration,omitempty"`
}

func (in ServerInput) Validate(requireName bool) error {
	if requireName && strings.TrimSpace(in.Name) == "" {
		return errors.New("--name is required")
	}
	if strings.TrimSpace(in.URL) == "" {
		return errors.New("--url is required")
	}
	parsed, err := url.Parse(in.URL)
	if err != nil {
		return fmt.Errorf("--url is invalid: %w", err)
	}
	if parsed.Scheme == "" {
		return errors.New("--url must include a URL scheme")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("--url scheme must be http or https")
	}
	if parsed.Host == "" {
		return errors.New("--url must include a host")
	}
	if in.Scope != "" && in.Scope != "user" && in.Scope != "tenant" {
		return errors.New("--scope must be one of: user, tenant")
	}
	for i, header := range in.Headers {
		if strings.TrimSpace(header.Name) == "" {
			return fmt.Errorf("header %d name is required", i+1)
		}
	}
	return nil
}

func ValidateTenantAuthHeaders(headers []Header) error {
	for _, header := range headers {
		name := strings.ToLower(strings.TrimSpace(header.Name))
		if isAuthenticationHeader(name) && strings.TrimSpace(header.Value) != "" {
			return nil
		}
	}
	return errors.New("--scope tenant requires at least one authentication --header with a value")
}

func isAuthenticationHeader(name string) bool {
	switch name {
	case "api-key", "authorization", "x-api-key", "x-auth-token",
		"x-ch-auth-api-token", "x-grafana-api-key":
		return true
	default:
		return false
	}
}

type MutationResult struct {
	Operation string  `json:"operation" yaml:"operation"`
	Server    *Server `json:"server,omitempty" yaml:"server,omitempty"`
	AuthURL   string  `json:"authUrl,omitempty" yaml:"authUrl,omitempty"`
	// Error carries the in-band failure summary when the mutation itself
	// persisted but a follow-up step failed (e.g. the post-mutation OAuth
	// requirement check) and the command exits non-zero: a machine consumer
	// reading only stdout must see why the exit code is not 0. Empty on full
	// success.
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
}

type ValidationResult struct {
	Status  string `json:"status" yaml:"status"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

type OAuthResult struct {
	AuthURL string `json:"authUrl" yaml:"authUrl"`
	State   string `json:"state,omitempty" yaml:"state,omitempty"`
}

func ParseHeader(value string) (Header, error) {
	name, headerValue, ok := strings.Cut(value, "=")
	if !ok {
		return Header{}, fmt.Errorf("--header %q must be NAME=VALUE", value)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Header{}, fmt.Errorf("--header %q name is required", value)
	}
	return Header{Name: name, Value: headerValue}, nil
}
