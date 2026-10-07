// Package agentping sends agent notifications to the current Grafana user's phone.
package agentping

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

// Message is the mobile notification payload accepted by the IRM plugin.
type Message struct {
	Host  string `json:"host"`
	Agent string `json:"agent"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Body  string `json:"body,omitempty"`
	Inbox string `json:"inbox"`
}

// Send reports API acceptance, not delivery to the device.
func Send(ctx context.Context, cfg config.NamespacedRESTConfig, message Message) error {
	client, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return fmt.Errorf("create notification client: %w", err)
	}
	client.Timeout = 30 * time.Second
	// A login redirect must not become a successful notification receipt.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode notification: %w", err)
	}
	url := strings.TrimRight(cfg.Host, "/") + "/api/plugins/grafana-irm-app/resources/mobile_notifications/self"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send notification (delivery unknown; check your phone before retrying): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		hint := "check your Grafana stack and mobile app pairing"
		switch resp.StatusCode {
		case http.StatusBadRequest:
			hint = "check the notification fields"
			validationBody, readErr := httputils.ReadResponseBody(resp.Body, 4096)
			if readErr == nil && json.Valid(validationBody) {
				hint += ": " + string(bytes.TrimSpace(validationBody))
			}
		case http.StatusUnauthorized, http.StatusForbidden:
			hint = "run gcx login with your user identity; gcx cloud login and service account tokens cannot identify your phone"
		case http.StatusNotFound:
			hint = "check that this stack supports mobile notifications and your user has paired the Grafana mobile app"
		case http.StatusTooManyRequests:
			hint = "notification rate limit reached; wait before trying again"
		}
		return &gcxerrors.HTTPStatusError{Status: resp.StatusCode, Message: fmt.Sprintf("notification request failed (HTTP %d); %s", resp.StatusCode, hint)}
	}
	responseBody, err := httputils.ReadResponseBody(resp.Body, 1<<20)
	if err != nil {
		return fmt.Errorf("read notification response (delivery unknown; check your phone before retrying): %w", err)
	}
	if len(bytes.TrimSpace(responseBody)) > 0 && !json.Valid(responseBody) {
		return errors.New("unexpected non-JSON notification response; check your Grafana stack and gcx login (delivery unknown)")
	}
	return nil
}
