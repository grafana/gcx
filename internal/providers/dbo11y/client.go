package dbo11y

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/prometheus/common/model"
	"k8s.io/client-go/rest"
)

// apiBasePath is the base path for all routes.
const apiBasePath = "/api/plugins/grafana-dbo11y-app/resources/api/v1"

// apiClient is the base HTTP client for the Database Observability API.
type apiClient struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

func newAPIClient(cfg config.NamespacedRESTConfig) (*apiClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &apiClient{restConfig: cfg, httpClient: httpClient}, nil
}

// doRequest issues a GET request against path (relative to the API's
// versioned root, e.g. "/instances") with the given query
// parameters, and returns the raw response.
func (c *apiClient) doRequest(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	full := c.restConfig.Host + apiBasePath + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	return resp, nil
}

// envelope is the API's success-response wrapper: {"status":"success","data":...}.
type envelope[T any] struct {
	Data T `json:"data"`
}

// apiGet issues a GET request against path with the given query parameters and
// decodes the response envelope's "data" field into Resp. On a non-2xx
// status it returns a *apiError.
func apiGet[Resp any](ctx context.Context, c *apiClient, path string, query url.Values) (Resp, error) {
	var zero Resp
	resp, err := c.doRequest(ctx, path, query)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, decodeError(resp.StatusCode, body)
	}

	var env envelope[Resp]
	if err := json.Unmarshal(body, &env); err != nil {
		return zero, fmt.Errorf("failed to decode response: %w", err)
	}
	return env.Data, nil
}

// codeInvalidServerID is the error code the API answers with when an
// argument falls outside the charset it accepts for a server_id.
const codeInvalidServerID = "invalid_server_id"

// apiError is a structured error returned by the Database Observability
// API's {"status":"error","error":{"code","message"}} envelope.
type apiError struct {
	StatusCode int
	Code       string
	message    string
	rawBody    string
}

func (e *apiError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("dbo11y: request failed with status %d: %s", e.StatusCode, e.message)
	}
	if e.rawBody != "" {
		return fmt.Sprintf("dbo11y: request failed with status %d: %s", e.StatusCode, e.rawBody)
	}
	return fmt.Sprintf("dbo11y: request failed with status %d", e.StatusCode)
}

// HTTPStatusCode implements the interface used by cmd/gcx/fail for rich errors.
func (e *apiError) HTTPStatusCode() int { return e.StatusCode }

// APIServiceName implements the interface used by cmd/gcx/fail for rich errors.
func (e *apiError) APIServiceName() string { return "Database Observability" }

// APIUserMessage implements the interface used by cmd/gcx/fail for rich errors.
func (e *apiError) APIUserMessage() string {
	if e.message != "" {
		return e.message
	}
	return e.rawBody
}

// decodeError parses an already-read non-2xx response body as the APIs
// error envelope, falling back to the raw body text if it doesn't match.
func decodeError(statusCode int, body []byte) *apiError {
	apiErr := &apiError{StatusCode: statusCode}
	if len(body) == 0 {
		return apiErr
	}
	var envErr struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envErr); err == nil && envErr.Error.Message != "" {
		apiErr.message = envErr.Error.Message
		apiErr.Code = envErr.Error.Code
	} else {
		apiErr.rawBody = strings.TrimSpace(string(body))
	}
	return apiErr
}

const instancesPath = "/instances"

// maxTopQueries is the Database Observability API's own cap on
// /instances/{id}/queries?limit=; passing more is rejected by the API, so
// callers clamp to it rather than forwarding a larger value.
const maxTopQueries = 100

// backendQueryWindow is the width the backend evaluates each query's own
// figures over, regardless of the range a caller asks for: every arm of the
// top-queries lookup is an instant query with its lookback baked into the
// expression. Reported as the `window` of an `instances get` result so the
// figures are labelled with the width they actually cover.
const backendQueryWindow = "5m"

func instancePath(serverID string) string {
	return instancesPath + "/" + url.PathEscape(serverID)
}

func queriesPath(serverID string) string {
	return instancePath(serverID) + "/queries"
}

// fetchInstances calls GET /instances, scoped to datasourceUID, with filters
// applied as exact-match label matchers on the inventory metric. A nil
// response slice is normalised to an empty one so the filtered and unfiltered
// paths cannot diverge in `-o json` between `null` and `[]`.
func fetchInstances(ctx context.Context, client *apiClient, datasourceUID string, filters url.Values) ([]Instance, error) {
	query := url.Values{"datasource": {datasourceUID}}
	for label, values := range filters {
		if label == "datasource" {
			continue
		}
		for _, v := range values {
			query.Add(label, v)
		}
	}
	raw, err := apiGet[[]apiInstance](ctx, client, instancesPath, query)
	if err != nil {
		return nil, err
	}
	out := make([]Instance, 0, len(raw))
	for _, a := range raw {
		out = append(out, a.toInstance())
	}
	return out, nil
}

// fetchInstanceDetail calls GET /instances/{serverID}.
func fetchInstanceDetail(ctx context.Context, client *apiClient, datasourceUID, serverID string) (Instance, error) {
	query := url.Values{"datasource": {datasourceUID}}
	raw, err := apiGet[apiInstance](ctx, client, instancePath(serverID), query)
	if err != nil {
		return Instance{}, err
	}
	return raw.toInstance(), nil
}

// queriesResponse is the raw shape GET /instances/{id}/queries returns.
type queriesResponse struct {
	Queries []apiQuery `json:"queries"`
}

// fetchTopQueries calls GET /instances/{serverID}/queries, scoping the
// request to [now-since, now]. limit <= 0 or > maxTopQueries is clamped to
// maxTopQueries, the API's own cap.
func fetchTopQueries(ctx context.Context, client *apiClient, datasourceUID, lokiDatasourceUID, serverID, since string, limit int) ([]TopQuery, error) {
	dur, err := model.ParseDuration(since)
	if err != nil {
		return nil, fmt.Errorf("invalid --since %q: %w", since, err)
	}
	now := time.Now().UTC()
	from := now.Add(-time.Duration(dur))

	if limit <= 0 || limit > maxTopQueries {
		limit = maxTopQueries
	}

	query := url.Values{
		"datasource": {datasourceUID},
		"from":       {from.Format(time.RFC3339)},
		"to":         {now.Format(time.RFC3339)},
		"limit":      {strconv.Itoa(limit)},
	}
	if lokiDatasourceUID != "" {
		query.Set("loki", lokiDatasourceUID)
	}
	resp, err := apiGet[queriesResponse](ctx, client, queriesPath(serverID), query)
	if err != nil {
		return nil, err
	}
	out := make([]TopQuery, 0, len(resp.Queries))
	for _, a := range resp.Queries {
		out = append(out, a.toTopQuery())
	}
	return out, nil
}
