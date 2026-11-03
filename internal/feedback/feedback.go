// Package feedback builds and sends the free-text feedback that
// `gcx feedback send` delivers to the gcx team.
//
// Feedback is a separate payload on a separate route from the anonymous usage
// event in internal/telemetry, and it must stay that way: the usage event
// never carries free text, while feedback is free text by definition. What
// the two share is the telemetry mode, which decides whether feedback carries
// metadata, and the device ID inside that metadata.
package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/telemetry"
	"github.com/grafana/gcx/internal/version"
)

// Env documents the environment variables that control feedback. The env
// tags are read by scripts/env-vars-reference (docs generation).
type Env struct {
	// Endpoint overrides the URL `gcx feedback send` posts feedback to.
	Endpoint string `env:"GCX_FEEDBACK_ENDPOINT"`
}

const (
	envEndpoint     = "GCX_FEEDBACK_ENDPOINT"
	defaultEndpoint = "https://stats.grafana.org/gcx-feedback"

	// ServiceName identifies gcx in the payload envelope.
	ServiceName = "gcx"

	// MaxBodyBytes is the largest request body the receiver accepts. gcx
	// rejects anything larger before sending.
	MaxBodyBytes = 64 * 1024

	sendTimeout = 10 * time.Second

	// maxResponseBytes bounds how much of the receiver's reply is read; a
	// confirmation is a few bytes.
	maxResponseBytes = 4 * 1024
)

// Endpoint returns the feedback receiver URL, honouring GCX_FEEDBACK_ENDPOINT.
func Endpoint() string {
	if override := os.Getenv(envEndpoint); override != "" {
		return override
	}
	return defaultEndpoint
}

// Payload is the request body. Its json encoding is the wire contract.
type Payload struct {
	Service    string            `json:"service"`
	FeedbackID string            `json:"feedback_id"`
	Data       map[string]string `json:"data"`
	// Metadata is null unless usage statistics are enabled.
	Metadata *Metadata `json:"metadata"`
}

// Metadata describes the gcx install that sent the feedback. It is attached
// only when the telemetry mode resolves to enabled.
type Metadata struct {
	Version           string `json:"version"`
	OS                string `json:"os"`
	Arch              string `json:"arch"`
	IsAgent           bool   `json:"is_agent"`
	Agent             string `json:"agent"`
	DeviceID          string `json:"device_id"`
	DeviceIDPersisted bool   `json:"device_id_persisted"`
}

// CollectMetadata returns the metadata for mode: nil unless usage statistics
// are enabled. Only the enabled path reads (and on first use creates) the
// device ID, so disabled and log modes leave no trace on disk.
func CollectMetadata(mode telemetry.Mode) *Metadata {
	if mode != telemetry.ModeEnabled {
		return nil
	}
	m := &Metadata{
		Version: version.Get(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		IsAgent: agent.IsAgentMode(),
		Agent:   agent.Name(),
	}
	// Same fallback as the usage event, so both report one label vocabulary.
	if m.Agent == "" && m.IsAgent {
		m.Agent = "generic"
	}
	m.DeviceID, m.DeviceIDPersisted = telemetry.DeviceID()
	return m
}

// ErrEmpty means nothing was left to send after empty values were dropped.
var ErrEmpty = errors.New("feedback is empty")

// TooLargeError means the encoded payload exceeds MaxBodyBytes.
type TooLargeError struct {
	Size int
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("feedback is %d bytes after encoding, the limit is %d bytes", e.Size, MaxBodyBytes)
}

// NewPayload scrubs every key and value of data, drops keys whose value is
// empty or whitespace, and assigns a fresh feedback ID. data is not modified.
func NewPayload(data map[string]string, metadata *Metadata) (Payload, error) {
	scrubbed := make(map[string]string, len(data))
	for k, v := range data {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		scrubbed[Scrub(k)] = Scrub(v)
	}
	if len(scrubbed) == 0 {
		return Payload{}, ErrEmpty
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return Payload{}, fmt.Errorf("generate feedback id: %w", err)
	}
	return Payload{
		Service:    ServiceName,
		FeedbackID: id.String(),
		Data:       scrubbed,
		Metadata:   metadata,
	}, nil
}

// Encode returns the exact request body, or a *TooLargeError when it would
// exceed MaxBodyBytes. HTML escaping is off so placeholders such as
// <redacted:url> read the same on the wire as in --dry-run.
func (p Payload) Encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	body := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	if len(body) > MaxBodyBytes {
		return nil, &TooLargeError{Size: len(body)}
	}
	return body, nil
}

// RejectedError means the receiver answered with a status other than 200.
// The feedback was not stored.
type RejectedError struct {
	Status int
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("feedback rejected with HTTP %d", e.Status)
}

// UnknownOutcomeError means gcx cannot tell whether the feedback was stored:
// the request failed in transit, or the receiver's reply was not a
// confirmation. The feedback may have been stored.
type UnknownOutcomeError struct {
	Cause error
}

func (e *UnknownOutcomeError) Error() string {
	return "feedback outcome unknown: " + e.Cause.Error()
}

func (e *UnknownOutcomeError) Unwrap() error { return e.Cause }

// Send posts body to endpoint in a single attempt: no retries and no
// redirects. It returns nil only when the receiver replied 200 with
// {"success":true}, which it sends after storing the feedback.
func Send(ctx context.Context, endpoint string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build feedback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := httputils.NewClient(httputils.ClientOpts{
		Timeout:      sendTimeout,
		DisableRetry: true,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
	resp, err := client.Do(req)
	if err != nil {
		return &UnknownOutcomeError{Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &RejectedError{Status: resp.StatusCode}
	}

	var reply struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&reply); err != nil {
		return &UnknownOutcomeError{Cause: fmt.Errorf("decode reply: %w", err)}
	}
	if !reply.Success {
		return &UnknownOutcomeError{Cause: errors.New("reply did not confirm storage")}
	}
	return nil
}
