// Package watcher defines configuration manifests and read adapters for Watchers.
package watcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/shared"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	WatcherAPIGroup     = "assistant.ext.grafana.app"
	WatcherVersion      = "v1alpha1"
	WatcherAPIVersion   = WatcherAPIGroup + "/" + WatcherVersion
	WatcherKind         = "Watcher"
	WatcherIDAnnotation = WatcherAPIGroup + "/watcher-id"
	legacyRoutingLabel  = "__grafana_managed_route__"
)

// Watcher is the configuration-only manifest spec. Runtime identity is carried
// privately and projected into metadata by the adapter.
//
//nolint:recvcheck // ResourceIdentity uses a pointer setter and a value getter.
type Watcher struct {
	Title                  string            `json:"title"`
	Description            string            `json:"description"`
	Prompt                 string            `json:"prompt"`
	DatasourceUIDs         []string          `json:"datasourceUids"`
	Interval               string            `json:"interval"`
	Sensitivity            string            `json:"sensitivity"`
	SkipReviewOnCleanRuns  bool              `json:"skipReviewOnCleanRuns"`
	AutoStop               AutoStop          `json:"autoStop"`
	Labels                 map[string]string `json:"labels"`
	AutomaticRecalibration EnabledSetting    `json:"automaticRecalibration"`
	Notifications          Notifications     `json:"notifications"`
	Investigation          Investigation     `json:"investigation"`
	mutation               MutationOutcome
	serverID               string
}

type AutoStop struct {
	Enabled bool   `json:"enabled"`
	At      string `json:"at,omitempty"`
	Archive bool   `json:"archive"`
}

type EnabledSetting struct {
	Enabled bool `json:"enabled"`
}

type Notifications struct {
	Slack    SlackNotification    `json:"slack"`
	Teams    ChannelNotification  `json:"teams"`
	Alerting AlertingNotification `json:"alerting"`
	Webhook  WebhookNotification  `json:"webhook"`
}

type SlackNotification struct {
	Enabled    bool   `json:"enabled"`
	TargetType string `json:"targetType"`
	ChannelID  string `json:"channelId,omitempty"`
	UserID     string `json:"userId,omitempty"`
	TeamID     string `json:"teamId,omitempty"`
	Severity   string `json:"severity"`
}

type ChannelNotification struct {
	Enabled   bool   `json:"enabled"`
	ChannelID string `json:"channelId,omitempty"`
	Severity  string `json:"severity"`
}

type AlertingNotification struct {
	Enabled     bool   `json:"enabled"`
	Severity    string `json:"severity"`
	RoutingTree string `json:"routingTree"`
}

type WebhookNotification struct {
	Enabled       bool         `json:"enabled"`
	Severity      string       `json:"severity"`
	URL           *SecretInput `json:"url,omitempty"`
	BearerToken   *SecretInput `json:"bearerToken,omitempty"`
	SigningSecret *SecretInput `json:"signingSecret,omitempty"`
}

type Investigation struct {
	Enabled    bool     `json:"enabled"`
	TeamAccess []string `json:"teamAccess"`
}

// SecretInput describes intent only. Validation never resolves its sources.
type SecretInput struct {
	FromEnv  string `json:"fromEnv,omitempty"`
	FromFile string `json:"fromFile,omitempty"`
	Preserve bool   `json:"preserve,omitempty"`
	Clear    bool   `json:"clear,omitempty"`
}

var _ adapter.ResourceIdentity = (*Watcher)(nil)

func (w Watcher) GetResourceName() string   { return adapter.SlugifyName(w.Title) }
func (w *Watcher) SetResourceName(_ string) {}
func (w Watcher) ServerID() string          { return w.serverID }

func WatcherDescriptor() resources.Descriptor {
	return resources.Descriptor{GroupVersion: schema.GroupVersion{Group: WatcherAPIGroup, Version: WatcherVersion}, Kind: WatcherKind, Singular: "watcher", Plural: "watchers"}
}

// UnmarshalJSON rejects unknown configuration fields and validates dependencies
// before any adapter I/O. Omitted blocks receive the manifest defaults.
func (w *Watcher) UnmarshalJSON(data []byte) error {
	type plain Watcher
	if err := validateJSONShape(data, "spec"); err != nil {
		return err
	}
	next := plain{
		Interval: (time.Duration(watchers.DefaultIntervalSeconds) * time.Second).String(), Sensitivity: watchers.DefaultSensitivity, SkipReviewOnCleanRuns: true,
		DatasourceUIDs: []string{}, Labels: map[string]string{},
		Notifications: Notifications{Slack: SlackNotification{TargetType: "channel", Severity: "warning-and-critical"}, Teams: ChannelNotification{Severity: "warning-and-critical"}, Alerting: AlertingNotification{Severity: "warning-and-critical"}, Webhook: WebhookNotification{Severity: "warning-and-critical"}},
		Investigation: Investigation{TeamAccess: []string{}},
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&next); err != nil {
		return fmt.Errorf("invalid Watcher spec: %w", err)
	}
	result := Watcher(next)
	if err := result.Validate(); err != nil {
		return err
	}
	*w = result
	return nil
}

func (s *SecretInput) UnmarshalJSON(data []byte) error {
	if err := validateJSONShape(data, "secret input"); err != nil {
		return invalidSecretInput()
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 1 {
		return invalidSecretInput()
	}
	next := SecretInput{}
	for key, value := range fields {
		switch key {
		case "fromEnv":
			if err := json.Unmarshal(value, &next.FromEnv); err != nil {
				return invalidSecretInput()
			}
		case "fromFile":
			if err := json.Unmarshal(value, &next.FromFile); err != nil {
				return invalidSecretInput()
			}
		case "preserve":
			if err := json.Unmarshal(value, &next.Preserve); err != nil {
				return invalidSecretInput()
			}
		case "clear":
			if err := json.Unmarshal(value, &next.Clear); err != nil {
				return invalidSecretInput()
			}
		default:
			return invalidSecretInput()
		}
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*s = next
	return nil
}

func invalidSecretInput() error {
	return errors.New("secret input requires exactly one nonempty fromEnv, nonempty fromFile, preserve: true, or clear: true")
}

func (s *SecretInput) Validate() error {
	count := 0
	if strings.TrimSpace(s.FromEnv) != "" {
		count++
	}
	if strings.TrimSpace(s.FromFile) != "" {
		count++
	}
	if s.Preserve {
		count++
	}
	if s.Clear {
		count++
	}
	if count != 1 || (s.FromEnv != "" && strings.TrimSpace(s.FromEnv) == "") || (s.FromFile != "" && strings.TrimSpace(s.FromFile) == "") {
		return invalidSecretInput()
	}
	return nil
}

// Validate checks authored configuration without reading secrets or the target.
func (w Watcher) Validate() error {
	for _, field := range []struct{ name, value string }{{"title", w.Title}, {"prompt", w.Prompt}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("spec.%s requires a nonblank string", field.name)
		}
	}
	duration, err := shared.ParseDuration(w.Interval)
	if err != nil || duration <= 0 || duration%time.Second != 0 {
		return errors.New("spec.interval requires a positive whole-second duration such as 15m")
	}
	if err := watchers.ValidateIntervalSeconds(int64(duration / time.Second)); err != nil {
		return fmt.Errorf("spec.interval: %w", err)
	}
	if strings.TrimSpace(w.Sensitivity) == "" {
		return errors.New("spec.sensitivity requires a nonblank string")
	}
	if w.AutoStop.At != "" {
		if _, err := time.Parse(time.RFC3339, w.AutoStop.At); err != nil {
			return errors.New("spec.autoStop.at requires an RFC 3339 timestamp with explicit offset")
		}
	}
	if w.AutoStop.Enabled && w.AutoStop.At == "" {
		return errors.New("spec.autoStop.at is required when autoStop is enabled")
	}
	if w.AutoStop.Archive && !w.AutoStop.Enabled {
		return errors.New("spec.autoStop.archive requires an enabled autoStop")
	}
	slack := w.Notifications.Slack
	if err := validateSlackTarget(slack); err != nil {
		return err
	}
	if w.Notifications.Teams.Enabled && strings.TrimSpace(w.Notifications.Teams.ChannelID) == "" {
		return errors.New("spec.notifications.teams.channelId is required when enabled")
	}
	webhook := w.Notifications.Webhook
	for _, notification := range []struct{ name, severity string }{
		{"slack", slack.Severity}, {"teams", w.Notifications.Teams.Severity},
		{"alerting", w.Notifications.Alerting.Severity}, {"webhook", webhook.Severity},
	} {
		if err := validateSeverity("spec.notifications."+notification.name+".severity", notification.severity); err != nil {
			return err
		}
	}
	for _, input := range []struct {
		name  string
		value *SecretInput
	}{{"url", webhook.URL}, {"bearerToken", webhook.BearerToken}, {"signingSecret", webhook.SigningSecret}} {
		if input.value != nil {
			if err := input.value.Validate(); err != nil {
				return fmt.Errorf("spec.notifications.webhook.%s: %w", input.name, err)
			}
		}
	}
	if webhook.Enabled && (webhook.URL == nil || webhook.URL.Clear) {
		return errors.New("spec.notifications.webhook.url requires a source or preserve: true when enabled")
	}
	return nil
}

func validateSlackTarget(slack SlackNotification) error {
	if strings.TrimSpace(slack.TargetType) == "" {
		return errors.New("spec.notifications.slack.targetType requires a nonblank string")
	}
	switch slack.TargetType {
	case "channel":
		// Disabled legacy destinations can retain ancillary IDs before selection.
		if (slack.Enabled || slack.ChannelID != "") && strings.TrimSpace(slack.ChannelID) == "" {
			return errors.New("spec.notifications.slack.channelId is required for a channel target")
		}
	case "dm":
		if strings.TrimSpace(slack.UserID) == "" {
			return errors.New("spec.notifications.slack.userId is required for a dm target")
		}
	}
	return nil
}

func validateSeverity(path, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s requires a nonblank string", path)
	}
	return nil
}

// validateJSONShape rejects nulls and duplicate fields before Go's decoder
// can replace an authored value or silently accept null into a scalar.
func validateJSONShape(data []byte, path string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var visit func(string) error
	visit = func(path string) error {
		token, err := decoder.Token()
		if err != nil {
			return errors.New("invalid Watcher configuration JSON")
		}
		if token == nil {
			return fmt.Errorf("%s must not be null", path)
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]bool)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return errors.New("invalid Watcher configuration JSON")
				}
				key, ok := token.(string)
				if !ok {
					return errors.New("invalid Watcher configuration JSON")
				}
				if seen[key] {
					return fmt.Errorf("%s contains a duplicate field", path)
				}
				seen[key] = true
				if err := visit(path + "." + key); err != nil {
					return err
				}
			}
		case '[':
			for index := 0; decoder.More(); index++ {
				if err := visit(fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid Watcher configuration JSON")
		}
		if _, err := decoder.Token(); err != nil {
			return errors.New("invalid Watcher configuration JSON")
		}
		return nil
	}
	return visit(path)
}
