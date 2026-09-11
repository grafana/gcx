// Package slo is a typed Go client for Grafana Cloud's SLO definitions API.
// It takes a caller-supplied *http.Client and base URL: it builds no
// transport, discovers no tokens, and reads no config file. Auth is
// entirely the caller's responsibility.
package slo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrNotFound is returned when a requested SLO does not exist (HTTP 404).
var ErrNotFound = errors.New("SLO not found")

// ErrDeleteNotConfirmed is returned by Delete when confirmed is false. A
// library caller has no TTY to prompt against, so Delete never blocks on
// stdin: callers must explicitly pass confirmed=true once they've obtained
// consent by whatever means fits their own UI.
var ErrDeleteNotConfirmed = errors.New("delete not confirmed")

const (
	basePath     = "/api/plugins/grafana-slo-app/resources/v1/slo"
	sloByUUIDFmt = basePath + "/%s"
)

// Client is a typed HTTP client for the Grafana SLO definitions API.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new SLO definitions client using the given HTTP
// client and base URL. The caller owns auth: supply an *http.Client whose
// transport already attaches whatever credentials the target Grafana
// instance requires.
func NewClient(httpClient *http.Client, baseURL string) *Client {
	return &Client{httpClient: httpClient, baseURL: baseURL}
}

// List returns all SLO definitions.
func (c *Client) List(ctx context.Context) ([]Slo, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, basePath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list SLOs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var listResp listResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode SLO list response: %w", err)
	}

	if listResp.SLOs == nil {
		return []Slo{}, nil
	}

	return listResp.SLOs, nil
}

// Get returns a single SLO definition by UUID.
func (c *Client) Get(ctx context.Context, uuid string) (*Slo, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf(sloByUUIDFmt, uuid), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get SLO %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var slo Slo
	if err := json.NewDecoder(resp.Body).Decode(&slo); err != nil {
		return nil, fmt.Errorf("failed to decode SLO response: %w", err)
	}

	return &slo, nil
}

// Create creates a new SLO definition and returns the created object. The
// create endpoint's response only carries the server-assigned UUID, so
// Create fetches the full definition before returning.
func (c *Client) Create(ctx context.Context, slo *Slo) (*Slo, error) {
	body, err := json.Marshal(slo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SLO: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create SLO: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, handleErrorResponse(resp)
	}

	var createResp createResponse
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return nil, fmt.Errorf("failed to decode SLO create response: %w", err)
	}

	created, err := c.Get(ctx, createResp.UUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created SLO %q: %w", createResp.UUID, err)
	}

	return created, nil
}

// Update updates an existing SLO definition and returns the updated object.
func (c *Client) Update(ctx context.Context, uuid string, slo *Slo) (*Slo, error) {
	body, err := json.Marshal(slo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SLO: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf(sloByUUIDFmt, uuid), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update SLO %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return nil, handleErrorResponse(resp)
	}

	updated, err := c.Get(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated SLO %q: %w", uuid, err)
	}

	return updated, nil
}

// Delete deletes an SLO definition by UUID. confirmed must be true or
// Delete returns ErrDeleteNotConfirmed without making a request: unlike the
// gcx CLI command backed by this client, a library caller has no TTY to
// prompt against, so Delete never reads stdin — the caller must obtain
// consent itself and pass it through explicitly.
func (c *Client) Delete(ctx context.Context, uuid string, confirmed bool) error {
	if !confirmed {
		return ErrDeleteNotConfirmed
	}

	resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf(sloByUUIDFmt, uuid), nil)
	if err != nil {
		return fmt.Errorf("failed to delete SLO %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return handleErrorResponse(resp)
	}

	return nil
}

// doRequest builds and executes an HTTP request against the Grafana SLO API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}
