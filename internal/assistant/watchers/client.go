package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/gcxerrors"
)

var (
	ErrCapabilityUnavailable = errors.New("assistant Watcher capability is unavailable")
	ErrPermissionDenied      = errors.New("assistant Watcher access denied")
	ErrNotFound              = errors.New("assistant Watcher not found")
)

type APIError struct {
	StatusCode int
	Operation  string
	Code       string
	Message    string
	kind       error
	cause      error
}

func (e *APIError) Error() string {
	if e.kind != nil {
		return fmt.Sprintf("%s: %s (HTTP %d)", e.Operation, e.kind, e.StatusCode)
	}
	return fmt.Sprintf("%s failed (HTTP %d)", e.Operation, e.StatusCode)
}

func (e *APIError) Unwrap() error          { return errors.Join(e.kind, e.cause) }
func (e *APIError) HTTPStatusCode() int    { return e.StatusCode }
func (e *APIError) APIServiceName() string { return "Assistant Watchers" }
func (e *APIError) APIUserMessage() string { return e.Error() }

type Client struct {
	base *assistanthttp.Client
}

func NewClient(base *assistanthttp.Client) *Client {
	return &Client{base: base}
}

const collectionPath = "/api/v1/watcher-agents"

func (c *Client) ListAll(ctx context.Context, archived bool) ([]Watcher, error) {
	items := make([]Watcher, 0)
	cursor := ""
	seenCursors := make(map[string]struct{})
	seenIDs := make(map[string]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		params := url.Values{"archived": {strconv.FormatBool(archived)}, "page_size": {"100"}}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		var page struct {
			Agents     *[]Watcher `json:"agents"`
			NextCursor string     `json:"nextCursor"`
		}
		if err := c.read(ctx, collectionPath+"?"+params.Encode(), "list Watchers", true, &page); err != nil {
			return nil, err
		}
		if page.Agents == nil {
			return nil, errors.New("list Watchers: response omits collection items")
		}
		for _, item := range *page.Agents {
			if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" {
				return nil, errors.New("list Watchers: response contains an incomplete resource identity")
			}
			if _, duplicate := seenIDs[item.ID]; duplicate {
				continue
			}
			seenIDs[item.ID] = struct{}{}
			items = append(items, item)
		}
		if page.NextCursor == "" {
			return items, nil
		}
		if _, repeated := seenCursors[page.NextCursor]; repeated {
			return nil, errors.New("list Watchers: repeated pagination cursor; collection read is incomplete")
		}
		seenCursors[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
}

func (c *Client) Get(ctx context.Context, id string) (*Watcher, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	var item Watcher
	if err := c.read(ctx, path, "get Watcher", false, &item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" {
		return nil, errors.New("get Watcher: response has no resource identity")
	}
	return &item, nil
}

func (c *Client) Enrollment(ctx context.Context, id string) (*Enrollment, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	var item struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.read(ctx, path+"/auto-calibration", "read Watcher automatic recalibration", false, &item); err != nil {
		return nil, err
	}
	if item.Enabled == nil {
		return nil, errors.New("read Watcher automatic recalibration: response omits enabled setting")
	}
	return &Enrollment{Enabled: *item.Enabled}, nil
}

func (c *Client) Calibration(ctx context.Context, id string) (*Calibration, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	var item Calibration
	if err := c.read(ctx, path+"/initial-calibration", "read Watcher calibration", false, &item); err != nil {
		return nil, err
	}
	if item.Status == "" {
		return nil, errors.New("read Watcher calibration: response omits observed state")
	}
	return &item, nil
}

func watcherPath(id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", errors.New("Watcher ID is required")
	}
	return collectionPath + "/" + url.PathEscape(id), nil
}

func (c *Client) read(ctx context.Context, path, operation string, collection bool, dst any) error {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readAPIError(resp, operation, collection)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("decode %s response: missing data", operation)
	}
	if err := json.Unmarshal(envelope.Data, dst); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}
	return nil
}

func readAPIError(resp *http.Response, operation string, collection bool) error {
	var wire struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(body, &wire)
	apiErr := &APIError{StatusCode: resp.StatusCode, Operation: operation, Code: wire.Name, Message: wire.Message, cause: readErr}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		apiErr.kind = ErrPermissionDenied
	case resp.StatusCode == http.StatusNotImplemented || wire.Name == "NOT_IMPLEMENTED":
		apiErr.kind = ErrCapabilityUnavailable
	case resp.StatusCode == http.StatusNotFound && collection:
		apiErr.kind = ErrCapabilityUnavailable
	case resp.StatusCode == http.StatusNotFound:
		apiErr.kind = ErrNotFound
	}
	if errors.Is(apiErr, ErrCapabilityUnavailable) {
		return &gcxerrors.DetailedError{
			Parent: apiErr, Summary: "Endpoint not available", Details: apiErr.APIUserMessage(),
			Suggestions: []string{"Verify Assistant Watchers is available on the selected Grafana Cloud target"},
		}
	}
	return apiErr
}
