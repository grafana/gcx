package changereview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/gcxerrors"
)

const (
	checksPath       = "/api/v1/github/review-checks"
	statsPath        = "/api/v1/github/review-analytics"
	pullRequestsPath = statsPath + "/pull-requests"

	readPermission  = "grafana-assistant-app.github:read"
	writePermission = "grafana-assistant-app.github:write"
)

// ErrConflict reports that the checks changed between the read and the save
// twice in a row.
var ErrConflict = errors.New("change review checks changed while saving")

// Client reads and edits a stack's change review checks and statistics.
type Client struct {
	base *assistanthttp.Client
}

func NewClient(base *assistanthttp.Client) *Client {
	return &Client{base: base}
}

// ListChecks returns every built-in check with the stack's settings for it,
// followed by the stack's custom checks.
func (c *Client) ListChecks(ctx context.Context) (*Checks, error) {
	checks, err := assistanthttp.DoEnvelopeRequest[Checks](c.base, ctx, http.MethodGet, checksPath, nil, "list change review checks")
	if err != nil {
		return nil, classify(err, readPermission)
	}
	return checks, nil
}

// EditChecks reads the checks, applies edit, and saves the result guarded by
// the revision it read. When another save lands in between, it reads and
// applies edit once more, so edit must derive its change from the checks it is
// given.
func (c *Client) EditChecks(ctx context.Context, edit func([]Check) ([]Check, error)) (*Checks, error) {
	for attempt := 0; ; attempt++ {
		current, err := c.ListChecks(ctx)
		if err != nil {
			return nil, err
		}
		next, err := edit(current.Checks)
		if err != nil {
			return nil, err
		}
		saved, err := c.saveChecks(ctx, current.Revision, next)
		if errors.Is(err, ErrConflict) && attempt == 0 {
			continue
		}
		return saved, err
	}
}

// saveRequest is the write shape: descriptions are read-only.
type saveRequest struct {
	ExpectedRevision string                       `json:"expectedRevision"`
	Checks           []map[string]json.RawMessage `json:"checks"`
}

type savedCheck struct {
	ID                  string                   `json:"id"`
	BuiltIn             bool                     `json:"builtIn"`
	Name                string                   `json:"name"`
	Instructions        string                   `json:"instructions"`
	Enabled             bool                     `json:"enabled"`
	Blocking            bool                     `json:"blocking"`
	Paths               []string                 `json:"paths"`
	RepositoryOverrides map[string]CheckOverride `json:"repositoryOverrides,omitempty"`
	AppliesTo           []string                 `json:"appliesTo,omitempty"`
}

func (c *Client) saveChecks(ctx context.Context, revision string, checks []Check) (*Checks, error) {
	body := saveRequest{ExpectedRevision: revision, Checks: make([]map[string]json.RawMessage, 0, len(checks))}
	for _, check := range checks {
		wire, err := savedFields(check)
		if err != nil {
			return nil, err
		}
		body.Checks = append(body.Checks, wire)
	}
	saved, err := assistanthttp.DoEnvelopeRequest[Checks](c.base, ctx, http.MethodPut, checksPath, body, "save change review checks")
	if err != nil {
		if status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err); ok && status.Status == http.StatusConflict {
			return nil, ErrConflict
		}
		return nil, classify(err, writePermission)
	}
	return saved, nil
}

// savedFields is the check as a save sends it: the modelled fields, then the
// fields the read returned that gcx does not model.
func savedFields(check Check) (map[string]json.RawMessage, error) {
	paths := check.Paths
	if paths == nil {
		paths = []string{}
	}
	data, err := json.Marshal(savedCheck{
		ID: check.ID, BuiltIn: check.BuiltIn, Name: check.Name, Instructions: check.Instructions,
		Enabled: check.Enabled, Blocking: check.Blocking, Paths: paths,
		RepositoryOverrides: check.RepositoryOverrides, AppliesTo: check.AppliesTo,
	})
	if err != nil {
		return nil, fmt.Errorf("encode change review check %q: %w", check.ID, err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("encode change review check %q: %w", check.ID, err)
	}
	maps.Copy(wire, check.extra)
	return wire, nil
}

// Stats summarizes the published reviews in a window.
func (c *Client) Stats(ctx context.Context, q StatsQuery) (*Stats, error) {
	stats, err := assistanthttp.DoEnvelopeRequest[Stats](c.base, ctx, http.MethodGet, statsPath+q.encode(nil), nil, "read change review statistics")
	if err != nil {
		return nil, classify(err, readPermission)
	}
	return stats, nil
}

// PullRequests lists the pull requests whose published reviews raised findings
// from check, most recently reviewed first.
func (c *Client) PullRequests(ctx context.Context, check string, q StatsQuery) (*PullRequests, error) {
	query := q.encode(url.Values{"check": {check}})
	pulls, err := assistanthttp.DoEnvelopeRequest[PullRequests](c.base, ctx, http.MethodGet, pullRequestsPath+query, nil, "list change review pull requests")
	if err != nil {
		return nil, classify(err, readPermission)
	}
	return pulls, nil
}

func (q StatsQuery) encode(params url.Values) string {
	if params == nil {
		params = url.Values{}
	}
	if !q.From.IsZero() {
		params.Set("from", q.From.UTC().Format(time.RFC3339Nano))
	}
	if !q.To.IsZero() {
		params.Set("to", q.To.UTC().Format(time.RFC3339Nano))
	}
	if q.Repository != "" {
		params.Set("repository", q.Repository)
	}
	if len(params) == 0 {
		return ""
	}
	return "?" + params.Encode()
}

// classify separates a stack without change review checks, which answers 403
// on checks and 404 on statistics, from a caller without permission. Only the
// server's own "not enabled" answers qualify: an unknown check ID returns an
// empty list, and any other 404 (such as a plugin without these routes) is
// reported as it is.
func classify(err error, permission string) error {
	status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
	if !ok {
		return err
	}
	notEnabled := (status.Status == http.StatusNotFound && strings.Contains(status.ServerMessage, "change review analytics not found")) ||
		(status.Status == http.StatusForbidden && strings.Contains(status.ServerMessage, "not enabled for this stack"))
	if notEnabled {
		return &gcxerrors.DetailedError{
			Summary: "Change review checks are not enabled on this stack",
			Details: "The stack's Grafana Assistant does not serve change review checks or statistics.",
			Parent:  err,
			Suggestions: []string{
				"Confirm GitHub is connected in Grafana Assistant settings and that change review checks are available for this stack",
			},
		}
	}
	if forbidden, ok := errors.AsType[*assistanthttp.ForbiddenError](err); ok {
		forbidden.Permission = permission
	}
	return err
}
