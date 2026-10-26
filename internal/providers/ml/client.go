package ml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
)

const basePath = "/api/plugins/grafana-ml-app/resources/manage/api/v1/"

// client uses the configured Grafana transport and the ML plugin proxy.
type client[T adapter.ResourceNamer] struct {
	deps adapter.ClientDeps
	path string
}

func validID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\?#%") {
		return errors.New("invalid ML resource ID: use a server ID without path separators")
	}
	return nil
}

func (c *client[T]) request(ctx context.Context, method, suffix string, input, output any) error {
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			return fmt.Errorf("encode ML request: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.deps.BaseURL, "/")+basePath+c.path+suffix, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.deps.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ml request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := providers.HandleErrorResponse(resp)
		if resp.StatusCode == http.StatusNotFound && method == http.MethodGet && suffix != "" {
			return fmt.Errorf("%w: %w", adapter.ErrNotFound, err)
		}
		return err
	}
	if output == nil {
		return nil
	}
	data, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return err
	}
	var result struct {
		Status   string          `json:"status"`
		Data     json.RawMessage `json:"data"`
		Error    string          `json:"error"`
		Warnings []string        `json:"warnings"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decode ML response: %w", err)
	}
	if result.Status != "success" || result.Error != "" {
		return fmt.Errorf("ml API failed: status %q: %s", result.Status, result.Error)
	}
	if len(result.Data) == 0 || string(result.Data) == "null" {
		return errors.New("ml response has no data")
	}
	for _, warning := range result.Warnings {
		slog.WarnContext(ctx, "ML API warning", "warning", warning)
	}
	if err := json.Unmarshal(result.Data, output); err != nil {
		return fmt.Errorf("decode ML data: %w", err)
	}
	return nil
}

func (c *client[T]) List(ctx context.Context, opts adapter.ListOptions) ([]T, error) {
	var items []T
	if err := c.request(ctx, http.MethodGet, "", nil, &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = make([]T, 0)
	}
	for _, item := range items {
		if item.GetResourceName() == "" {
			return nil, errors.New("ml response contains an item without an ID")
		}
	}
	return adapter.TruncateSlice(items, opts.Limit), nil
}

func (c *client[T]) Get(ctx context.Context, id string) (*T, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	var item T
	if err := c.request(ctx, http.MethodGet, "/"+id, nil, &item); err != nil {
		return nil, err
	}
	if item.GetResourceName() != id {
		return nil, errors.New("ml response ID does not match the requested ID")
	}
	return &item, nil
}

func (c *client[T]) write(ctx context.Context, id string, item *T) (*T, error) {
	data, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err = json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	// The API rejects IDs in update bodies and assigns IDs on create.
	delete(payload, "id")
	if _, ok := payload["grafanaUrl"]; ok {
		target := c.deps.GrafanaURL
		if target == "" {
			target = c.deps.BaseURL
		}
		payload["grafanaUrl"] = target
	}
	var result T
	suffix := ""
	if id != "" {
		suffix = "/" + id
	}
	if err = c.request(ctx, http.MethodPost, suffix, payload, &result); err != nil {
		return nil, err
	}
	if result.GetResourceName() == "" {
		return nil, errors.New("ml response has no resource ID")
	}
	return &result, nil
}

func (c *client[T]) Create(ctx context.Context, item *T) (*T, error) { return c.write(ctx, "", item) }
func (c *client[T]) Update(ctx context.Context, id string, item *T) (*T, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	return c.write(ctx, id, item)
}
func (c *client[T]) Delete(ctx context.Context, id string) error {
	if err := validID(id); err != nil {
		return err
	}
	return c.request(ctx, http.MethodDelete, "/"+id, nil, nil)
}
