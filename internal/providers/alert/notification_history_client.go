package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers"
)

// historianAPIPrefix is the App Platform group/version prefix for the alerting
// historian API. Notification history lives on custom subresource routes under
// this group, not on a standard CRUD resource.
const historianAPIPrefix = "/apis/historian.alerting.grafana.app/v0alpha1"

// ErrNotificationHistoryDisabled is returned when the historian API responds
// with 422, which it does when notification history is not enabled on the stack.
var ErrNotificationHistoryDisabled = errors.New(
	"notification history is not enabled on this stack: enable [unified_alerting.notification_history] " +
		"(with Loki configured) and the kubernetesAlertingHistorian feature",
)

// QueryNotifications lists notification delivery history entries matching req.
func (c *Client) QueryNotifications(ctx context.Context, req NotificationQueryRequest) ([]NotificationEntry, error) {
	path := c.historianPath("notification/query")
	var resp NotificationQueryResponse
	if err := c.postHistorian(ctx, path, req, &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

// QueryAlerts returns the individual alerts that were part of a single grouped
// notification, identified by its UUID.
func (c *Client) QueryAlerts(ctx context.Context, req NotificationAlertsRequest) ([]NotificationAlert, error) {
	path := c.historianPath("notifications/queryalerts")
	var resp NotificationAlertsResponse
	if err := c.postHistorian(ctx, path, req, &resp); err != nil {
		return nil, err
	}
	return resp.Alerts, nil
}

func (c *Client) historianPath(subresource string) string {
	return fmt.Sprintf("%s/namespaces/%s/%s", historianAPIPrefix, url.PathEscape(c.namespace), subresource)
}

// postHistorian performs a JSON POST against a historian route and decodes the
// response. It does not reuse doBody because the historian API's disabled state
// surfaces as a 422 that must be mapped to ErrNotificationHistoryDisabled; other
// non-2xx responses still go through providers.HandleErrorResponse.
func (c *Client) postHistorian(ctx context.Context, path string, in, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return ErrNotificationHistoryDisabled
	case resp.StatusCode >= 400:
		return providers.HandleErrorResponse(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}
