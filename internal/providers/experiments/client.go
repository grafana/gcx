package experiments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

const (
	listPath               = "/api/plugins/grafana-odin-app/resources/v1/experiments"
	pluginPageSize         = 500
	maxListResponseBytes   = 32 << 20
	maxCreateResponseBytes = 4 << 20
)

// Experiment retains every field in Odin's resource object for JSON and YAML output.
type Experiment map[string]any

type Client struct {
	httpClient *http.Client
	host       string
}

func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("create Odin HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: strings.TrimRight(cfg.Host, "/")}, nil
}

// List reads the single page exposed by Odin's app plugin.
func (c *Client) List(ctx context.Context) ([]Experiment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+listPath, nil)
	if err != nil {
		return nil, fmt.Errorf("create Odin list request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list Odin experiments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusNotFound {
			return nil, errors.New("odin experiments endpoint returned 404; check that grafana-odin-app is enabled in this Grafana context")
		}
		return nil, fmt.Errorf("list Odin experiments: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Odin experiments response: %w", err)
	}
	if len(body) > maxListResponseBytes {
		return nil, fmt.Errorf("odin experiments response exceeds %d bytes", maxListResponseBytes)
	}

	var payload struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Odin experiments response: %w", err)
	}
	if len(payload.Items) == 0 || bytes.Equal(bytes.TrimSpace(payload.Items), []byte("null")) {
		return nil, errors.New("decode Odin experiments response: missing items array")
	}
	items := make([]Experiment, 0)
	if err := json.Unmarshal(payload.Items, &items); err != nil {
		return nil, fmt.Errorf("decode Odin experiments items: %w", err)
	}
	return items, nil
}

// Create submits one complete Experiment resource through Odin's app plugin.
func (c *Client) Create(ctx context.Context, manifest []byte) (Experiment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+listPath, bytes.NewReader(manifest))
	if err != nil {
		return nil, fmt.Errorf("create Odin request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create Odin experiment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := createErrorMessage(body)
		switch resp.StatusCode {
		case http.StatusNotFound:
			return nil, errors.New("odin experiments endpoint returned 404; check that grafana-odin-app is enabled in this Grafana context")
		case http.StatusConflict:
			return nil, fmt.Errorf("create Odin experiment: conflict (HTTP 409): %s", message)
		default:
			return nil, fmt.Errorf("create Odin experiment: HTTP %d: %s", resp.StatusCode, message)
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCreateResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read created Odin experiment: %w", err)
	}
	if len(body) > maxCreateResponseBytes {
		return nil, fmt.Errorf("created Odin experiment response exceeds %d bytes", maxCreateResponseBytes)
	}
	var created Experiment
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decode created Odin experiment: %w", err)
	}
	if created == nil {
		return nil, errors.New("decode created Odin experiment: empty resource")
	}
	return created, nil
}

func createErrorMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil && strings.TrimSpace(payload.Message) != "" {
		return strings.TrimSpace(payload.Message)
	}
	if message := strings.TrimSpace(string(body)); message != "" {
		return message
	}
	return "request failed"
}
