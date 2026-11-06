package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	fail "github.com/grafana/gcx/internal/gcxerrors"
)

type MessageAPI interface {
	CreateMessage(ctx context.Context, req createMessageRequest) (*createMessageResponse, error)
}

type SharingClientLoader interface {
	LoadMessages(ctx context.Context, timeout time.Duration) (MessageAPI, error)
	RequireSharingOAuth(ctx context.Context) error
}

type ShareClientLoader interface {
	OpModelClientLoader
	SharingClientLoader
}

type messageAgent struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type createMessageRequest struct {
	IncidentID string        `json:"incident_id"`
	DraftedBy  *messageAgent `json:"drafted_by,omitempty"`
	Body       string        `json:"body"`
	See        string        `json:"see,omitempty"`
}

type createMessageResponse struct {
	MessageID string `json:"message_id"`
}

func (c *irmResourceClient) CreateMessage(ctx context.Context, req createMessageRequest) (*createMessageResponse, error) {
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}
	return do[createMessageResponse](ctx, c, http.MethodPost, "/messages", nil, bytes.NewReader(encoded), mapMessageHTTPError)
}

func (l *configLoader) LoadMessages(ctx context.Context, timeout time.Duration) (MessageAPI, error) {
	return l.loadIRMResourceClient(ctx, timeout)
}

func (l *configLoader) RequireSharingOAuth(ctx context.Context) error {
	cfg, err := l.LoadConfigTolerant(ctx)
	if err != nil {
		return err
	}
	method, err := cfg.GetCurrentContext().EffectiveGrafanaAuthMethod()
	if err != nil {
		return err
	}
	if method == "oauth" {
		return nil
	}
	exitCode := fail.ExitAuthFailure
	return &fail.DetailedError{
		Summary:     "sharing requires an OAuth user login",
		Details:     "Service account tokens, Basic authentication, and client certificates cannot be used to share findings.",
		ExitCode:    &exitCode,
		Suggestions: []string{"Sign in with gcx login using OAuth", "Unset GRAFANA_TOKEN if it overrides your OAuth context"},
	}
}

func mapMessageHTTPError(resp *http.Response, _ string) error {
	exitCode := fail.ExitGeneralError
	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		exitCode = fail.ExitUsageError
	case http.StatusUnauthorized, http.StatusForbidden:
		exitCode = fail.ExitAuthFailure
	}
	return &fail.DetailedError{
		Summary: fmt.Sprintf("message sharing failed with HTTP %d", resp.StatusCode),
		Details: decodeOpModelProblem(resp), ExitCode: &exitCode,
	}
}
