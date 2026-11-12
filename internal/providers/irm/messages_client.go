package irm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	fail "github.com/grafana/gcx/internal/gcxerrors"
)

// A share has no idempotency key, so a client timeout on a send the server
// still commits leaves the user unsure whether it went out. Give it room.
const defaultShareTimeout = 30 * time.Second

type MessageAPI interface {
	CreateMessage(ctx context.Context, req createMessageRequest) (*createMessageResponse, error)
}

type SharingClientLoader interface {
	LoadMessages(ctx context.Context, timeout time.Duration) (MessageAPI, error)
	RequireSharingOAuth(ctx context.Context) (messageDestination, error)
}

// messageDestination names the Grafana instance a share goes to, after
// --context and environment overrides are applied.
type messageDestination struct {
	Context       string
	Server        string
	ProxyEndpoint string
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
	return post[createMessageResponse](ctx, c, "/messages", req, mapMessageHTTPError)
}

func (l *configLoader) LoadMessages(ctx context.Context, timeout time.Duration) (MessageAPI, error) {
	return l.loadIRMResourceClient(ctx, timeout)
}

func (l *configLoader) RequireSharingOAuth(ctx context.Context) (messageDestination, error) {
	cfg, err := l.LoadConfig(ctx)
	if err != nil {
		return messageDestination{}, err
	}
	current := cfg.GetCurrentContext()
	method, err := current.EffectiveGrafanaAuthMethod()
	if err != nil {
		return messageDestination{}, err
	}
	if method == "oauth" {
		return messageDestination{Context: cfg.CurrentContext, Server: current.Grafana.Server, ProxyEndpoint: current.Grafana.ProxyEndpoint}, nil
	}
	exitCode := fail.ExitAuthFailure
	return messageDestination{}, &fail.DetailedError{
		Summary:     "sharing requires an OAuth user login",
		Details:     "Service account tokens, Basic authentication, and client certificates cannot be used to share findings.",
		ExitCode:    &exitCode,
		Suggestions: []string{"Sign in with gcx login using OAuth", "Unset GRAFANA_TOKEN if it overrides your OAuth context"},
	}
}

func mapMessageHTTPError(resp *http.Response, _ string) error {
	detail, isStatus := decodeOpMemProblem(resp)
	if resp.StatusCode == http.StatusNotFound && !isStatus {
		return irmEndpointNotAvailable(detail, "The IRM sharing API may not be enabled on this stack")
	}
	exitCode := fail.ExitGeneralError
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		exitCode = fail.ExitAuthFailure
	}
	return &fail.DetailedError{
		Summary: fmt.Sprintf("message sharing failed with HTTP %d", resp.StatusCode),
		Details: detail, ExitCode: &exitCode,
	}
}
