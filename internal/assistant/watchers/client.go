package watchers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/retry"
)

var (
	ErrCapabilityUnavailable = errors.New("assistant Watcher capability is unavailable")
	ErrPermissionDenied      = errors.New("assistant Watcher access denied")
	ErrNotFound              = errors.New("assistant Watcher not found")
	ErrInvalidConfiguration  = errors.New("assistant Watcher configuration rejected")
	ErrConflict              = errors.New("assistant Watcher edit rejected")
)

type APIError struct {
	StatusCode int
	Operation  string
	Reason     string
	kind       error
	cause      error
}

func (e *APIError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s: %s (HTTP %d)", e.Operation, e.Reason, e.StatusCode)
	}
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

// IsServerID recognizes the canonical UUID representation emitted for Watcher
// IDs. Other references must be matched against observed collection identities.
func IsServerID(ref string) bool {
	id, err := uuid.Parse(ref)
	return err == nil && id.String() == ref
}

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
	var item json.RawMessage
	if err := c.read(ctx, path+"/auto-calibration", "read Watcher automatic recalibration", false, &item); err != nil {
		return nil, err
	}
	var presence struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(item, &presence); err != nil {
		return nil, errors.New("read Watcher automatic recalibration: invalid response")
	}
	if presence.Enabled == nil {
		return nil, errors.New("read Watcher automatic recalibration: response omits enabled setting")
	}
	var enrollment Enrollment
	if err := json.Unmarshal(item, &enrollment); err != nil {
		return nil, errors.New("read Watcher automatic recalibration: invalid response")
	}
	return &enrollment, nil
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
		return fmt.Errorf("decode %s response: invalid response data", operation)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("decode %s response: missing data", operation)
	}
	if err := json.Unmarshal(envelope.Data, dst); err != nil {
		return fmt.Errorf("decode %s response: invalid response data", operation)
	}
	return nil
}

func readAPIError(resp *http.Response, operation string, collection bool) error {
	var wire struct {
		Name    string `json:"name"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(body, &wire)
	apiErr := &APIError{StatusCode: resp.StatusCode, Operation: operation, cause: readErr}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		apiErr.kind = ErrPermissionDenied
	case resp.StatusCode == http.StatusNotImplemented || wire.Name == "NOT_IMPLEMENTED":
		apiErr.kind = ErrCapabilityUnavailable
	case resp.StatusCode == http.StatusNotFound && collection:
		apiErr.kind = ErrCapabilityUnavailable
	case resp.StatusCode == http.StatusNotFound:
		apiErr.kind = ErrNotFound
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity:
		apiErr.kind = ErrInvalidConfiguration
	case resp.StatusCode == http.StatusConflict:
		apiErr.kind = ErrConflict
	}
	if errors.Is(apiErr.kind, ErrConflict) { // Only fixed, recognized reasons can leave the client.
		message := strings.ToLower(wire.Message + " " + wire.Error)
		switch {
		case strings.Contains(message, "restore the archived watcher first"):
			apiErr.Reason = "restore the archived Watcher before editing"
		case strings.Contains(message, "finish or cancel calibration before updating"):
			apiErr.Reason = "finish or cancel calibration before editing the Watcher"
		}
	}
	return apiErr
}

func (c *Client) Create(ctx context.Context, request *ConfigurationRequest) (*Watcher, error) {
	return c.save(ctx, http.MethodPost, collectionPath, "create Watcher", request)
}

func (c *Client) Update(ctx context.Context, id string, request *ConfigurationRequest) (*Watcher, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	return c.save(ctx, http.MethodPut, path, "update Watcher", request)
}

func (c *Client) save(ctx context.Context, method, path, operation string, request *ConfigurationRequest) (*Watcher, error) {
	if request == nil {
		return nil, errors.New("Watcher configuration is required")
	}
	var item Watcher
	if err := c.write(ctx, method, path, operation, request, &item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" {
		return nil, fmt.Errorf("%s: response has no resource identity", operation)
	}
	return &item, nil
}

func (c *Client) Delete(ctx context.Context, id string) error {
	path, err := watcherPath(id)
	if err != nil {
		return err
	}
	var result struct {
		Deleted *bool `json:"deleted"`
	}
	if err := c.write(ctx, http.MethodDelete, path, "delete Watcher", nil, &result); err != nil {
		return err
	}
	if result.Deleted == nil || !*result.Deleted {
		return errors.New("delete Watcher: response does not confirm deletion")
	}
	return nil
}

func (c *Client) SetEnrollment(ctx context.Context, id string, enabled bool) (*Enrollment, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	request := struct {
		Enabled bool `json:"enabled"`
	}{enabled}
	var data json.RawMessage
	if err := c.write(ctx, http.MethodPut, path+"/auto-calibration", "save Watcher automatic recalibration", request, &data); err != nil {
		return nil, err
	}
	var presence struct {
		Enabled *bool `json:"enabled"`
	}
	var result Enrollment
	if json.Unmarshal(data, &presence) != nil || presence.Enabled == nil || json.Unmarshal(data, &result) != nil {
		return nil, errors.New("save Watcher automatic recalibration: response omits enabled setting")
	}
	return &result, nil
}

func (c *Client) write(ctx context.Context, method, path, operation string, request, dst any) error {
	var body io.Reader
	if request != nil {
		data, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("%s: cannot encode configuration", operation)
		}
		body = bytes.NewReader(data)
	}
	resp, err := c.base.DoRequest(retry.WithoutRetries(ctx), method, path, body)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", operation, ctx.Err())
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return readAPIError(resp, operation, false)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&envelope) != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("%s: invalid response", operation)
	}
	if json.Unmarshal(envelope.Data, dst) != nil {
		return fmt.Errorf("%s: invalid response data", operation)
	}
	return nil
}
