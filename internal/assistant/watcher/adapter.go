package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watchers"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"golang.org/x/sync/errgroup"
)

func init() { //nolint:gochecknoinits // Natural-key registration follows the typed-resource convention.
	adapter.RegisterNaturalKey(WatcherDescriptor().GroupVersionKind(), adapter.SpecFieldKey("title"))
}

func NewTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[Watcher], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("load Grafana config for Watchers: %w", err)
	}
	client, err := clientForConfig(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, err
	}
	return NewTypedCRUDForClient(client, cfg.Namespace), cfg, nil
}

func clientForConfig(cfg internalconfig.NamespacedRESTConfig) (*watchers.Client, error) {
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create Assistant HTTP client for Watchers: %w", err)
	}
	return watchers.NewClient(base), nil
}

func NewTypedCRUDForClient(client *watchers.Client, namespace string) *adapter.TypedCRUD[Watcher] {
	return NewTypedCRUDForClientArchived(client, namespace, false)
}

// NewTypedCRUDForClientArchived selects the list partition. Individual reads
// resolve across both visible partitions regardless of this selection.
func NewTypedCRUDForClientArchived(client *watchers.Client, namespace string, archived bool) *adapter.TypedCRUD[Watcher] {
	return &adapter.TypedCRUD[Watcher]{
		ListFn: adapter.LimitedListFn(func(ctx context.Context) ([]Watcher, error) {
			raw, err := client.ListAll(ctx, archived)
			if err != nil {
				return nil, fmt.Errorf("list Watchers: %w", err)
			}
			result := make([]Watcher, len(raw))
			group, readCtx := errgroup.WithContext(ctx)
			group.SetLimit(10)
			for i := range raw {
				group.Go(func() error {
					// Collection entries are summaries; detail and enrollment establish the
					// full modeled configuration before a successful manifest is emitted.
					detail, err := client.Get(readCtx, raw[i].ID)
					if err != nil {
						return fmt.Errorf("read Watcher %q: %w", raw[i].ID, err)
					}
					manifest, err := ReadManifest(readCtx, client, *detail)
					if err != nil {
						return err
					}
					result[i] = manifest
					return nil
				})
			}
			if err := group.Wait(); err != nil {
				return nil, err
			}
			return result, nil
		}),
		GetFn: func(ctx context.Context, ref string) (*Watcher, error) {
			raw, err := Resolve(ctx, client, ref)
			if err != nil {
				return nil, err
			}
			manifest, err := ReadManifest(ctx, client, *raw)
			if err != nil {
				return nil, err
			}
			return &manifest, nil
		},
		MetadataFn: func(w Watcher) map[string]any {
			if w.serverID == "" {
				return nil
			}
			return map[string]any{"annotations": map[string]any{WatcherIDAnnotation: w.serverID}}
		},
		Namespace: namespace, Descriptor: WatcherDescriptor(), Example: WatcherExample(),
	}
}

func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		crud, _, err := NewTypedCRUD(ctx, &loader)
		if err != nil {
			return nil, err
		}
		return &manifestAdapter{ResourceAdapter: crud.AsAdapter()}, nil
	}
}

// Candidate retains only caller-visible identity used for collision diagnostics.
type Candidate struct{ ID, Title, Name string }

// IdentityIndex enumerates both visible partitions without reading additional
// configuration. It is safe for diagnosing candidates outside a pull partition.
func IdentityIndex(ctx context.Context, client *watchers.Client) (map[string][]Candidate, error) {
	index := make(map[string][]Candidate)
	seen := make(map[string]bool)
	for _, archived := range []bool{false, true} {
		items, err := client.ListAll(ctx, archived)
		if err != nil {
			return nil, fmt.Errorf("enumerate Watcher identities: %w", err)
		}
		for _, item := range items {
			if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" {
				return nil, errors.New("Watcher identity collection contains an item without an ID or nonblank title")
			}
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			name := adapter.SlugifyName(item.Name)
			index[name] = append(index[name], Candidate{ID: item.ID, Title: item.Name, Name: name})
		}
	}
	return index, nil
}

func IdentityIndexForConfig(ctx context.Context, cfg internalconfig.NamespacedRESTConfig) (map[string][]Candidate, error) {
	client, err := clientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return IdentityIndex(ctx, client)
}

// CollisionError reports every visible candidate, in deterministic order.
func CollisionError(name string, candidates []Candidate) error {
	ordered := slices.Clone(candidates)
	slices.SortFunc(ordered, func(a, b Candidate) int { return strings.Compare(a.ID, b.ID) })
	details := make([]string, 0, len(ordered))
	for _, candidate := range ordered {
		details = append(details, fmt.Sprintf("id=%q title=%q", candidate.ID, candidate.Title))
	}
	return fmt.Errorf("Watcher resource name %q is ambiguous (%s); use a server ID to select one Watcher", name, strings.Join(details, "; "))
}

// Resolve gives observed server IDs precedence, then matches resource names
// across both visible archive partitions and fetches the selected full detail.
func Resolve(ctx context.Context, client *watchers.Client, ref string) (*watchers.Watcher, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("Watcher reference requires a nonblank resource name or server ID")
	}
	index, err := IdentityIndex(ctx, client)
	if err != nil {
		return nil, err
	}
	for _, candidates := range index {
		for _, candidate := range candidates {
			if candidate.ID == ref {
				return client.Get(ctx, candidate.ID)
			}
		}
	}
	matches := index[ref]
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("Watcher %q: %w", ref, adapter.ErrNotFound)
	case 1:
		return client.Get(ctx, matches[0].ID)
	default:
		return nil, CollisionError(ref, matches)
	}
}

// ReadManifest requires observed enrollment rather than guessing a disabled
// setting when this additional configuration cannot be read.
func ReadManifest(ctx context.Context, client *watchers.Client, raw watchers.Watcher) (Watcher, error) {
	enrollment, err := client.Enrollment(ctx, raw.ID)
	if err != nil {
		return Watcher{}, fmt.Errorf("read automatic recalibration configuration for Watcher %q: %w", raw.ID, err)
	}
	if enrollment == nil {
		return Watcher{}, fmt.Errorf("automatic recalibration configuration for Watcher %q was not returned", raw.ID)
	}
	manifest := WatcherFromResponse(raw, enrollment.Enabled)
	if err := manifest.Validate(); err != nil {
		return Watcher{}, fmt.Errorf("Watcher %q configuration cannot be exported: %w", raw.ID, err)
	}
	return manifest, nil
}

// WatcherFromResponse projects modeled configuration only. Secret-presence
// indicators become preserve markers, including for disabled destinations.
func WatcherFromResponse(raw watchers.Watcher, automaticRecalibration bool) Watcher {
	result := Watcher{
		Title: raw.Name, Description: raw.Description, Prompt: raw.Prompt,
		DatasourceUIDs: append([]string{}, raw.DatasourceUIDs...), Interval: (time.Duration(raw.TriggerIntervalSeconds) * time.Second).String(),
		Sensitivity: raw.Sensitivity, SkipReviewOnCleanRuns: !raw.DisableDecisionSkip,
		Labels: maps.Clone(raw.Labels), AutomaticRecalibration: EnabledSetting{Enabled: automaticRecalibration},
		Notifications: Notifications{Slack: ChannelNotification{Severity: "warning-and-critical"}, Teams: ChannelNotification{Severity: "warning-and-critical"}, Webhook: WebhookNotification{Severity: "warning-and-critical"}},
		Investigation: Investigation{TeamAccess: []string{}}, serverID: raw.ID,
	}
	if result.Labels == nil {
		result.Labels = map[string]string{}
	}
	if raw.AutoStop != nil {
		result.AutoStop.Archive = raw.AutoStop.Archive
		if raw.AutoStop.PauseAt != nil {
			result.AutoStop.Enabled = true
			result.AutoStop.At = raw.AutoStop.PauseAt.Format(time.RFC3339Nano)
		}
	}
	if action := raw.Actions.Slack; action != nil {
		result.Notifications.Slack = channelFromResponse(action)
	}
	if action := raw.Actions.MSTeams; action != nil {
		result.Notifications.Teams = channelFromResponse(action)
	}
	if action := raw.Actions.Alerting; action != nil {
		result.Notifications.Alerting.Enabled = action.Enabled
	}
	if action := raw.Actions.Webhook; action != nil {
		result.Notifications.Webhook = webhookFromResponse(action)
	}
	if action := raw.Actions.Investigation; action != nil {
		result.Investigation.Enabled = action.Enabled
		result.Investigation.TeamAccess = append([]string{}, action.TeamNames...)
	}
	return result
}

func channelFromResponse(raw *watchers.ChatAction) ChannelNotification {
	result := ChannelNotification{Enabled: raw.Enabled, Severity: severityFromResponse(raw.MinSeverity)}
	if raw.Target != nil {
		result.ChannelID = raw.Target.ChannelID
	}
	return result
}

func severityFromResponse(raw string) string {
	switch raw {
	case "critical":
		return "critical"
	case "", "warning":
		return "warning-and-critical"
	default:
		return raw
	}
}

func UnsupportedMutation(operation string) error {
	return fmt.Errorf("Watcher %s is not supported yet; continue in Grafana: %w", operation, errors.ErrUnsupported)
}

type manifestAdapter struct{ adapter.ResourceAdapter }

func (a *manifestAdapter) Schema() json.RawMessage { return WatcherSchema() }

func webhookFromResponse(raw *watchers.WebhookAction) WebhookNotification {
	fields := raw.SecureFields
	if fields == nil {
		fields = &watchers.SecureFields{}
	}
	return WebhookNotification{
		Enabled: raw.Enabled, Severity: severityFromResponse(raw.MinSeverity),
		URL: preserveMarker(fields.URL), BearerToken: preserveMarker(fields.AuthorizationCredentials), SigningSecret: preserveMarker(fields.HMACSecret),
	}
}
func preserveMarker(configured bool) *SecretInput {
	if configured {
		return &SecretInput{Preserve: true}
	}
	return nil
}
