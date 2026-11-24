package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	adaptiveauth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/providers"
)

const (
	apiPrefix               = "/archive-replay/api/v1"
	cellDetailsPath         = apiPrefix + "/cell-details"
	destinationsPath        = apiPrefix + "/archives"
	destinationByIDFmt      = destinationsPath + "/%s"
	validateDestinationPath = destinationsPath + "/validate"
	streamsPath             = apiPrefix + "/streams"
	streamByIDFmt           = streamsPath + "/%s"
)

// List endpoints are unpaginated.
const maxResponseBodyBytes = 10 << 20

type Client struct {
	baseURL    string
	tenantID   int
	apiToken   string
	httpClient *http.Client
}

func NewClient(baseURL string, tenantID int, apiToken string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		tenantID:   tenantID,
		apiToken:   apiToken,
		httpClient: httpClient,
	}
}

// The archive API is served on the Hosted Logs host and needs the same scope
// as Adaptive Logs.
func newClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	signalAuth, err := adaptiveauth.ResolveSignalAuth(ctx, loader, "logs")
	if err != nil {
		return nil, err
	}
	return NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient), nil
}

func (c *Client) CellDetails(ctx context.Context) (*CellDetails, error) {
	var details CellDetails
	if err := c.do(ctx, http.MethodGet, cellDetailsPath, nil, &details, http.StatusOK); err != nil {
		return nil, fmt.Errorf("failed to get cell details: %w", err)
	}
	return &details, nil
}

func (c *Client) ListDestinations(ctx context.Context) ([]Destination, error) {
	var destinations []Destination
	if err := c.do(ctx, http.MethodGet, destinationsPath, nil, &destinations, http.StatusOK); err != nil {
		return nil, fmt.Errorf("failed to list archive destinations: %w", err)
	}
	if destinations == nil {
		return []Destination{}, nil
	}
	return destinations, nil
}

func (c *Client) GetDestination(ctx context.Context, id string) (*Destination, error) {
	var destination Destination
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf(destinationByIDFmt, url.PathEscape(id)), nil, &destination, http.StatusOK); err != nil {
		return nil, fmt.Errorf("failed to get archive destination %s: %w", id, err)
	}
	return &destination, nil
}

func (c *Client) CreateDestination(ctx context.Context, d *DestinationCreate) (*Destination, error) {
	var created Destination
	if err := c.do(ctx, http.MethodPost, destinationsPath, d, &created, http.StatusOK, http.StatusCreated); err != nil {
		return nil, fmt.Errorf("failed to create archive destination: %w", err)
	}
	return &created, nil
}

func (c *Client) ValidateDestination(ctx context.Context, d *DestinationCreate) error {
	if err := c.do(ctx, http.MethodPost, validateDestinationPath, d, nil, http.StatusOK); err != nil {
		return fmt.Errorf("failed to validate archive destination: %w", err)
	}
	return nil
}

func (c *Client) DeleteDestination(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf(destinationByIDFmt, url.PathEscape(id)), nil, nil, http.StatusOK, http.StatusNoContent); err != nil {
		return fmt.Errorf("failed to delete archive destination %s: %w", id, err)
	}
	return nil
}

func (c *Client) ListStreams(ctx context.Context) ([]Stream, error) {
	var streams []Stream
	if err := c.do(ctx, http.MethodGet, streamsPath, nil, &streams, http.StatusOK); err != nil {
		return nil, fmt.Errorf("failed to list archive streams: %w", err)
	}
	if streams == nil {
		return []Stream{}, nil
	}
	return streams, nil
}

func (c *Client) GetStream(ctx context.Context, id string) (*Stream, error) {
	var stream Stream
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf(streamByIDFmt, url.PathEscape(id)), nil, &stream, http.StatusOK); err != nil {
		return nil, fmt.Errorf("failed to get archive stream %s: %w", id, err)
	}
	return &stream, nil
}

func (c *Client) CreateStream(ctx context.Context, s *StreamCreate) (*Stream, error) {
	var created Stream
	if err := c.do(ctx, http.MethodPost, streamsPath, s, &created, http.StatusOK, http.StatusCreated); err != nil {
		return nil, fmt.Errorf("failed to create archive stream: %w", err)
	}
	return &created, nil
}

func (c *Client) UpdateStream(ctx context.Context, id string, s *StreamUpdate) (*Stream, error) {
	var updated Stream
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf(streamByIDFmt, url.PathEscape(id)), s, &updated, http.StatusOK); err != nil {
		return nil, fmt.Errorf("failed to update archive stream %s: %w", id, err)
	}
	return &updated, nil
}

func (c *Client) DeleteStream(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf(streamByIDFmt, url.PathEscape(id)), nil, nil, http.StatusOK, http.StatusNoContent); err != nil {
		return fmt.Errorf("failed to delete archive stream %s: %w", id, err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any, ok ...int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if !slices.Contains(ok, resp.StatusCode) {
		if err != nil {
			return &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("could not read response body: %v", err)}
		}
		return apiErrorFromResponseBody(resp.StatusCode, data)
	}
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(data)) > maxResponseBodyBytes {
		return fmt.Errorf("response body exceeds maximum size (%d bytes)", maxResponseBodyBytes)
	}
	if out == nil {
		return nil
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	if len(envelope.Result) == 0 {
		return errors.New("response missing result")
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}
