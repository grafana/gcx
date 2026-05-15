package vulnobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// gqlPath is the plugin-proxy GraphQL endpoint. The plugin slug is part of
// the plugin's public contract on the Grafana instance and is therefore a
// constant here (see ADR-001).
const gqlPath = "/api/plugin-proxy/grafana-vulnerabilityobs-app/api-proxy/graphql/query"

// Client posts GraphQL requests to the vulnerability-obs plugin proxy on
// the active Grafana instance, using the same Grafana token the rest of
// gcx uses.
type Client struct {
	httpClient *http.Client
	host       string
}

// NewClient creates a vulnerability-obs client from a NamespacedRESTConfig.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("vulnobs: failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

// gqlRequest is the on-the-wire shape of a GraphQL POST body.
type gqlRequest struct {
	OperationName string `json:"operationName"`
	Query         string `json:"query"`
	Variables     any    `json:"variables"`
}

// gqlResponse[T] is the on-the-wire shape of a GraphQL response.
type gqlResponse[T any] struct {
	Data   T            `json:"data"`
	Errors []gqlMessage `json:"errors,omitempty"`
}

// gqlMessage is one entry in the `errors[]` array of a GraphQL response.
// Decoded only; never sent on the wire, so no omitempty is needed.
type gqlMessage struct {
	Message    string         `json:"message"`
	Path       []string       `json:"path"`
	Extensions gqlMessageMeta `json:"extensions"`
}

// gqlMessageMeta is the `extensions` block on a GraphQL error.
type gqlMessageMeta struct {
	Code string `json:"code"`
}

// do posts a GraphQL document and decodes `data` into out.
func do[T any](ctx context.Context, c *Client, op, query string, vars any) (T, error) {
	var zero T

	body, err := json.Marshal(gqlRequest{OperationName: op, Query: query, Variables: vars})
	if err != nil {
		return zero, fmt.Errorf("vulnobs: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+gqlPath, bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("vulnobs: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("vulnobs: execute request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("vulnobs: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return zero, fmt.Errorf("vulnobs: %s returned HTTP %d: %s", op, resp.StatusCode, snippet(raw))
	}

	var out gqlResponse[T]
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, fmt.Errorf("vulnobs: decode %s response: %w", op, err)
	}
	if len(out.Errors) > 0 {
		msgs := make([]string, len(out.Errors))
		for i, e := range out.Errors {
			msgs[i] = e.Message
		}
		return zero, fmt.Errorf("vulnobs: %s graphql errors: %s", op, strings.Join(msgs, "; "))
	}
	return out.Data, nil
}

func snippet(b []byte) string {
	const maxLen = 200
	if len(b) > maxLen {
		return string(b[:maxLen]) + "...(truncated)"
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

const groupsQuery = `query Groups { groups { id name } }`

const projectsQuery = `query Projects($filters: SourceFilters!, $first: Int, $after: Int) {
  sources(filters: $filters, first: $first, after: $after) {
    metadata { totalCount }
    response {
      id name type origin visibility
      integration { id name type }
      groups { id name }
      versions {
        id tag publishDate lowestSloRemaining
        totalCveCounts { critical high medium low }
      }
    }
  }
}`

const issuesQuery = `query Issues($filters: IssueFilters!) {
  issues(filters: $filters) {
    response {
      id package installedVersion fixedVersion target sloRemaining
      tool { name }
      cve { cve severity cvssScore title }
    }
  }
}`

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// Groups returns every vulnerability-obs group (team/tag namespace).
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	type resp struct {
		Groups []Group `json:"groups"`
	}
	d, err := do[resp](ctx, c, "Groups", groupsQuery, struct{}{})
	if err != nil {
		return nil, err
	}
	return d.Groups, nil
}

// ResolveGroupID accepts either a numeric group ID or a group name (case
// insensitive) and returns the canonical numeric ID as a string.
func (c *Client) ResolveGroupID(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	if _, err := strconv.Atoi(ref); err == nil {
		return ref, nil
	}
	groups, err := c.Groups(ctx)
	if err != nil {
		return "", err
	}
	low := strings.ToLower(ref)
	for _, g := range groups {
		if strings.ToLower(g.Name) == low {
			return strconv.Itoa(g.ID), nil
		}
	}
	return "", fmt.Errorf("vulnobs: unknown group %q (try `gcx vulnobs groups list`)", ref)
}

// ProjectsOptions controls a `Projects` query.
type ProjectsOptions struct {
	GroupID      string // already a numeric string; use ResolveGroupID first
	Name         string // substring match on Source.name (server-side)
	SortBy       string // e.g. CRITICALS_DESC; empty for server default
	First        int    // page size; defaults to 30 when 0
	After        int    // cursor; defaults to 0
	HideK8s      bool   // exclude k8s-scan versions
	ShowArchived bool   // include archived sources
}

// Projects returns sources matching the given options.
func (c *Client) Projects(ctx context.Context, opts ProjectsOptions) ([]Source, int, error) {
	first := opts.First
	if first == 0 {
		first = 30
	}
	if opts.SortBy == "" {
		opts.SortBy = "CRITICALS_DESC"
	}
	filters := SourceFilters{
		GroupID:     opts.GroupID,
		Name:        opts.Name,
		SortBy:      opts.SortBy,
		EnabledOnly: true,
		VersionFilters: &VersionFilters{
			HideK8s:      opts.HideK8s,
			ShowArchived: opts.ShowArchived,
		},
	}
	vars := map[string]any{
		"filters": filters,
		"first":   first,
		"after":   opts.After,
	}
	type resp struct {
		Sources struct {
			Metadata struct {
				TotalCount int `json:"totalCount"`
			} `json:"metadata"`
			Response []Source `json:"response"`
		} `json:"sources"`
	}
	d, err := do[resp](ctx, c, "Projects", projectsQuery, vars)
	if err != nil {
		return nil, 0, err
	}
	return d.Sources.Response, d.Sources.Metadata.TotalCount, nil
}

// Issues returns CVE findings for a single Version.
func (c *Client) Issues(ctx context.Context, versionID string) ([]Issue, error) {
	if versionID == "" {
		return nil, errors.New("vulnobs: versionID is required")
	}
	vars := map[string]any{"filters": IssueFilters{VersionID: versionID}}
	type resp struct {
		Issues struct {
			Response []Issue `json:"response"`
		} `json:"issues"`
	}
	d, err := do[resp](ctx, c, "Issues", issuesQuery, vars)
	if err != nil {
		return nil, err
	}
	return d.Issues.Response, nil
}

// ResolveVersion resolves "owner/repo" + tag to a versionId. Tag defaults to
// "main" when empty. Falls back to the most recently published version if the
// requested tag is not found.
func (c *Client) ResolveVersion(ctx context.Context, repo, tag string) (string, error) {
	if repo == "" {
		return "", errors.New("vulnobs: repo is required")
	}
	if tag == "" {
		tag = "main"
	}
	sources, _, err := c.Projects(ctx, ProjectsOptions{
		Name:         repo,
		ShowArchived: true,
		First:        50,
	})
	if err != nil {
		return "", err
	}
	var match *Source
	for i := range sources {
		if sources[i].Name == repo {
			match = &sources[i]
			break
		}
	}
	if match == nil {
		// Fall back to suffix match for shorthand like "faro-web-sdk".
		low := strings.ToLower(repo)
		for i := range sources {
			n := strings.ToLower(sources[i].Name)
			if n == low || strings.HasSuffix(n, "/"+low) {
				match = &sources[i]
				break
			}
		}
	}
	if match == nil {
		return "", fmt.Errorf("vulnobs: repo %q not found", repo)
	}
	for _, v := range match.Versions {
		if v.Tag == tag {
			return strconv.Itoa(v.ID), nil
		}
	}
	// Fallback: most-recent by publish date.
	if len(match.Versions) == 0 {
		return "", fmt.Errorf("vulnobs: repo %q has no versions", repo)
	}
	latest := match.Versions[0]
	for _, v := range match.Versions[1:] {
		if v.PublishDate > latest.PublishDate {
			latest = v
		}
	}
	return strconv.Itoa(latest.ID), nil
}
