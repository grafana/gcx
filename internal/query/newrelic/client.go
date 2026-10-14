package newrelic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/queryerror"
)

// PluginID identifies the open-source New Relic Grafana datasource plugin.
const PluginID = "nrgrafanaplugin-newrelic-datasource"

// QueryRequest holds the NRQL text
// and optional plugin overrides.
type QueryRequest struct {
	QueryText      string
	Start          time.Time
	End            time.Time
	AccountID      int64
	TimeoutSeconds int
}

// Validate rejects empty NRQL and invalid plugin overrides.
func (r QueryRequest) Validate() error {
	if strings.TrimSpace(r.QueryText) == "" {
		return errors.New("NRQL expression must not be empty")
	}
	if r.AccountID < 0 {
		return errors.New("--account-id must be positive")
	}
	if r.TimeoutSeconds != 0 && (r.TimeoutSeconds < 5 || r.TimeoutSeconds > 120) {
		return errors.New("--timeout-seconds must be between 5 and 120")
	}
	return nil
}

// Model builds the plugin query without sending unset overrides.
func (r QueryRequest) Model(uid string) map[string]any {
	query := map[string]any{
		"refId":      "A",
		"datasource": map[string]string{"type": PluginID, "uid": uid},
		"queryText":  r.QueryText,
	}
	if r.AccountID != 0 {
		query["accountID"] = r.AccountID
	}
	if r.TimeoutSeconds != 0 {
		query["timeoutSeconds"] = r.TimeoutSeconds
	}
	return query
}

// QueryResponse keeps every frame returned by the plugin.
type QueryResponse struct {
	Frames []dataframe.Frame `json:"frames"`
}

// Client executes New Relic queries through Grafana's datasource query API.
type Client struct {
	queryClient *grafanaquery.Client
}

// NewClient uses Grafana's shared query transport.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	queryClient, err := grafanaquery.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{queryClient: queryClient}, nil
}

// Query runs NRQL and returns every result frame,
// including an empty frame list.
func (c *Client) Query(ctx context.Context, uid string, req QueryRequest) (*QueryResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	query := req.Model(uid)
	from, to := "now-1h", "now"
	if !req.Start.IsZero() && !req.End.IsZero() {
		from = strconv.FormatInt(req.Start.UnixMilli(), 10)
		to = strconv.FormatInt(req.End.UnixMilli(), 10)
	}
	body, err := json.Marshal(map[string]any{"queries": []any{query}, "from": from, "to": to})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}
	respBody, err := c.queryClient.Execute(ctx, body, "newrelic", "query")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results map[string]*dataframe.Result `json:"results"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse New Relic query response: %w", err)
	}
	result := resp.Results["A"]
	if result == nil {
		return nil, errors.New("newrelic query response is missing results.A")
	}
	if result.Error != "" || result.Status >= http.StatusBadRequest {
		status := result.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		return nil, queryerror.New("newrelic", "query", status, result.Error, result.ErrorSource)
	}
	if result.Frames == nil {
		result.Frames = []dataframe.Frame{}
	}
	return &QueryResponse{Frames: result.Frames}, nil
}
