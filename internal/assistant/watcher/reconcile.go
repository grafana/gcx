package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/shared"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// MutationOutcome records independent configuration writes and their observation.
type MutationOutcome struct {
	Changed       bool   `json:"changed" yaml:"changed"`
	Configuration string `json:"configuration" yaml:"configuration"`
	Enrollment    string `json:"enrollment" yaml:"enrollment"`
	Readback      string `json:"readback" yaml:"readback"`
}

func (w Watcher) MutationOutcome() MutationOutcome { return w.mutation }
func (w *Watcher) SetServerID(id string)           { w.serverID = id }

// PartialCompletionError retains saved identity when a later stage fails.
type PartialCompletionError struct {
	Watcher Watcher
	Cause   error
}

func (e *PartialCompletionError) Error() string {
	return fmt.Sprintf("Watcher %q (id=%q): configuration %s; enrollment %s; readback %s: %s", e.Watcher.GetResourceName(), e.Watcher.ServerID(), e.Watcher.mutation.Configuration, e.Watcher.mutation.Enrollment, e.Watcher.mutation.Readback, e.Cause)
}
func (e *PartialCompletionError) Unwrap() error          { return e.Cause }
func (e *PartialCompletionError) MutationWasSaved() bool { return e.Watcher.mutation.Changed }

func DecodeManifest(obj *unstructured.Unstructured) (*Watcher, error) {
	if obj == nil || obj.GetAPIVersion() != WatcherAPIVersion || obj.GetKind() != WatcherKind {
		return nil, errors.New("input requires a Watcher manifest with the supported apiVersion and kind")
	}

	for key := range obj.Object {
		if key != "apiVersion" && key != "kind" && key != "metadata" && key != "spec" {
			return nil, errors.New("Watcher manifest permits only apiVersion, kind, metadata and spec")
		}
	}
	if strings.TrimSpace(obj.GetName()) == "" {
		return nil, errors.New("metadata.name requires a nonblank string")
	}
	if _, _, err := unstructured.NestedStringMap(obj.Object, "metadata", "annotations"); err != nil {
		return nil, errors.New("metadata.annotations requires string values")
	}
	if _, _, err := unstructured.NestedString(obj.Object, "metadata", "namespace"); err != nil {
		return nil, errors.New("metadata.namespace requires a string")
	}
	raw, ok := obj.Object["spec"]
	if !ok {
		return nil, errors.New("Watcher manifest requires spec")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.New("invalid Watcher spec")
	}
	var w Watcher
	if err = json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	w.serverID = obj.GetAnnotations()[WatcherIDAnnotation]
	return &w, nil
}

// Warnings diagnose evolving values without constraining the API's vocabulary.
func (w Watcher) Warnings() []string {
	var warnings []string
	fields := []struct {
		path, value string
		known       []string
	}{
		{"spec.sensitivity", w.Sensitivity, []string{"sensitive", "balanced", "relaxed"}},
		{"spec.notifications.slack.severity", w.Notifications.Slack.Severity, []string{"warning-and-critical", "critical"}},
		{"spec.notifications.slack.targetType", w.Notifications.Slack.TargetType, []string{"channel", "dm"}},
		{"spec.notifications.alerting.severity", w.Notifications.Alerting.Severity, []string{"warning-and-critical", "critical"}},
		{"spec.notifications.teams.severity", w.Notifications.Teams.Severity, []string{"warning-and-critical", "critical"}},
		{"spec.notifications.webhook.severity", w.Notifications.Webhook.Severity, []string{"warning-and-critical", "critical"}},
	}
	for _, f := range fields {
		if !slices.Contains(f.known, f.value) {
			warnings = append(warnings, fmt.Sprintf("%s value %q is unrecognized; known values: %s; the API validates values on apply", f.path, f.value, strings.Join(f.known, ", ")))
		}
	}
	return warnings
}

func candidatesFlat(index map[string][]Candidate) []Candidate {
	var all []Candidate
	for _, items := range index {
		all = append(all, items...)
	}
	return all
}
func resolveIdentity(index map[string][]Candidate, ref string) (Candidate, error) {
	for _, c := range candidatesFlat(index) {
		if c.ID == ref {
			return c, nil
		}
	}
	if watchers.IsServerID(ref) {
		return Candidate{}, fmt.Errorf("Watcher %q: %w", ref, adapter.ErrNotFound)
	}
	switch items := index[ref]; len(items) {
	case 0:
		return Candidate{}, fmt.Errorf("Watcher %q: %w", ref, adapter.ErrNotFound)
	case 1:
		return items[0], nil
	default:
		return Candidate{}, CollisionError(ref, items)
	}
}
func selectUpsert(index map[string][]Candidate, w Watcher, create bool) (Candidate, error) {
	var target Candidate
	var titles []Candidate
	for _, c := range candidatesFlat(index) {
		if c.ID == w.serverID {
			target = c
		}
		if c.Title == w.Title {
			titles = append(titles, c)
		}
	}
	if create {
		if len(titles) > 0 {
			return Candidate{}, fmt.Errorf("Watcher title already exists; use update: %w", CollisionError(w.Title, titles))
		}
		return Candidate{}, nil
	}
	if target.ID != "" {
		if target.Title == w.Title {
			return target, nil
		}
		var occupied []Candidate
		for _, c := range titles {
			if c.ID != target.ID {
				occupied = append(occupied, c)
			}
		}
		if len(occupied) > 0 {
			return Candidate{}, fmt.Errorf("cannot rename into an occupied title: %w", CollisionError(w.Title, occupied))
		}
		return target, nil
	}
	switch len(titles) {
	case 0:
		return Candidate{}, nil
	case 1:
		return titles[0], nil
	default:
		return Candidate{}, CollisionError(w.Title, titles)
	}
}

func createConfiguration(ctx context.Context, client *watchers.Client, w *Watcher) (*Watcher, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}
	index, err := mutationIndex(ctx, client)
	if err != nil {
		return nil, err
	}
	if _, err = selectUpsert(index, *w, true); err != nil {
		return nil, err
	}
	return reconcile(ctx, client, *w, "")
}
func updateConfiguration(ctx context.Context, client *watchers.Client, ref string, w *Watcher) (*Watcher, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}
	index, err := mutationIndex(ctx, client)
	if err != nil {
		return nil, err
	}
	target, err := resolveIdentity(index, ref)
	if err != nil {
		return nil, err
	}
	if w.serverID != "" && w.serverID != target.ID {
		return nil, errors.New("manifest Watcher ID conflicts with positional update target")
	}
	desired := *w
	desired.serverID = target.ID
	if _, err = selectUpsert(index, desired, false); err != nil {
		return nil, err
	}
	return reconcile(ctx, client, desired, target.ID)
}
func deleteConfiguration(ctx context.Context, client *watchers.Client, ref string) error {
	if watchers.IsServerID(ref) {
		return client.Delete(ctx, ref)
	}
	index, err := mutationIndex(ctx, client)
	if err != nil {
		return err
	}
	target, err := resolveIdentity(index, ref)
	if err != nil {
		return err
	}
	return client.Delete(ctx, target.ID)
}

func reconcile(ctx context.Context, client *watchers.Client, desired Watcher, id string) (*Watcher, error) {
	if err := desired.Validate(); err != nil {
		return nil, err
	}
	var current Watcher
	var raw *watchers.Watcher
	enrollment := &watchers.Enrollment{}
	if id != "" {
		var err error
		raw, err = client.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		enrollment, err = client.Enrollment(ctx, id)
		if err != nil {
			return nil, err
		}
		unsupported := append(raw.UnsupportedConfiguration(), enrollment.UnsupportedConfiguration()...)
		if len(unsupported) > 0 {
			return nil, fmt.Errorf("Watcher has unrepresented configuration (%s); continue editing in Grafana", strings.Join(unsupported, ", "))
		}
		current = WatcherFromResponse(*raw, enrollment.Enabled)
	}
	// Keep unchanged legacy route names intact; only a changed selection uses
	// the API's trimming and default aliases. This also makes explicit reset
	// distinct from retaining a stored alias.
	if desired.Notifications.Alerting.RoutingTree != current.Notifications.Alerting.RoutingTree {
		desired.Notifications.Alerting.RoutingTree = canonicalRoutingTree(desired.Notifications.Alerting.RoutingTree)
	}
	configChanged := id == "" || !reflect.DeepEqual(normalized(desired), normalized(current))
	secretChanged := secretsChanged(desired, current)
	if configChanged || secretChanged {
		if err := checkSecretIntent(desired, current, id == ""); err != nil {
			return nil, err
		}
	}
	desired.serverID = id
	desired.mutation = MutationOutcome{Configuration: "unchanged", Enrollment: "unchanged", Readback: "not-required"}
	if configChanged || secretChanged {
		saved, err := saveConfiguration(ctx, client, desired, current, id)
		if err != nil {
			return nil, err
		}
		desired.serverID = saved.ID
		desired.mutation.Changed = true
		desired.mutation.Configuration = "saved"
	}

	if desired.AutomaticRecalibration.Enabled != enrollment.Enabled {
		if _, err := client.SetEnrollment(ctx, desired.serverID, desired.AutomaticRecalibration.Enabled); err != nil {
			desired.mutation.Enrollment = "failed"
			if desired.mutation.Configuration == "saved" {
				return nil, &PartialCompletionError{Watcher: desired, Cause: err}
			}
			return nil, err
		}
		desired.mutation.Enrollment = "saved"
		desired.mutation.Changed = true
	}
	if !desired.mutation.Changed {
		current.mutation = desired.mutation
		return &current, nil
	}
	saved, err := client.Get(ctx, desired.serverID)
	if err != nil {
		desired.mutation.Readback = "failed"
		return nil, &PartialCompletionError{Watcher: desired, Cause: err}
	}
	observed, err := ReadManifest(ctx, client, *saved)
	if err != nil {
		desired.mutation.Readback = "failed"
		return nil, &PartialCompletionError{Watcher: desired, Cause: err}
	}
	observed.mutation = desired.mutation
	observed.mutation.Readback = "observed"
	return &observed, nil
}

func normalized(w Watcher) Watcher {
	w.serverID = ""
	w.mutation = MutationOutcome{}
	w.AutomaticRecalibration = EnabledSetting{}
	w.Notifications.Webhook.URL = nil
	w.Notifications.Webhook.BearerToken = nil
	w.Notifications.Webhook.SigningSecret = nil
	w.Notifications.Slack.Severity = severityToRequest(w.Notifications.Slack.Severity)
	w.Notifications.Teams.Severity = severityToRequest(w.Notifications.Teams.Severity)
	w.Notifications.Alerting.Severity = severityToRequest(w.Notifications.Alerting.Severity)
	w.Notifications.Webhook.Severity = severityToRequest(w.Notifications.Webhook.Severity)
	duration, _ := shared.ParseDuration(w.Interval)
	w.Interval = strconv.Itoa(effectiveCadence(int(duration / time.Second)))
	if !w.AutoStop.Enabled {
		w.AutoStop = AutoStop{}
	} else if t, err := time.Parse(time.RFC3339Nano, w.AutoStop.At); err == nil {
		w.AutoStop.At = t.UTC().Format(time.RFC3339Nano)
	}
	w.DatasourceUIDs = sortedUnique(w.DatasourceUIDs)
	w.Investigation.TeamAccess = sortedUnique(w.Investigation.TeamAccess)
	w.Labels = configurationLabels(w.Labels)
	return w
}
func sortedUnique(values []string) []string {
	next := append([]string{}, values...)
	slices.Sort(next)
	return slices.Compact(next)
}

// effectiveCadence mirrors the API's stored scheduling cadence: minute intervals
// round to supported divisors of an hour, and longer intervals to whole hours
// (for example, 7m becomes 6m and 90m becomes 2h). Use it for both comparison and
// write payloads so readback of a rounded interval does not cause repeated writes.
func effectiveCadence(seconds int) int {
	minutes := max(1, seconds/60)
	if minutes >= 60 {
		return max(1, (minutes+30)/60) * 3600
	}
	best := 1
	for _, n := range []int{1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60} {
		if abs(n-minutes) < abs(best-minutes) {
			best = n
		}
	}
	return best * 60
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func severityToRequest(value string) string {
	if value == "warning-and-critical" {
		return "warning"
	}
	return value
}

func canonicalRoutingTree(value string) string {
	value = strings.TrimSpace(value)
	if value == "default" || value == "user-defined" {
		return ""
	}
	return value
}

// Routing has its own field. Retaining the legacy label would let an omitted
// routing field in a server response resurrect a selection explicitly cleared.
func configurationLabels(labels map[string]string) map[string]string {
	result := maps.Clone(labels)
	delete(result, legacyRoutingLabel)
	if result == nil {
		result = map[string]string{}
	}
	return result
}

func configurationRequest(w Watcher) *watchers.ConfigurationRequest {
	duration, _ := shared.ParseDuration(w.Interval)
	stop := &watchers.AutoStop{}
	if w.AutoStop.Enabled {
		t, _ := time.Parse(time.RFC3339Nano, w.AutoStop.At)
		stop.PauseAt = &t
		stop.Archive = w.AutoStop.Archive
	}
	channel := func(n ChannelNotification) *watchers.ChatAction {
		var target *watchers.ChatTarget
		if n.ChannelID != "" {
			target = &watchers.ChatTarget{Type: "channel", ChannelID: n.ChannelID}
		}
		return &watchers.ChatAction{Enabled: n.Enabled, Target: target, MinSeverity: severityToRequest(n.Severity)}
	}
	slack := w.Notifications.Slack
	var target *watchers.ChatTarget
	if slack.Enabled || slack.ChannelID != "" || slack.UserID != "" || slack.TeamID != "" || slack.TargetType != "channel" {
		target = &watchers.ChatTarget{Type: slack.TargetType, ChannelID: slack.ChannelID, UserID: slack.UserID, TeamID: slack.TeamID}
	}
	return &watchers.ConfigurationRequest{Name: new(w.Title), Description: new(w.Description), Prompt: new(w.Prompt), TriggerIntervalSeconds: new(effectiveCadence(int(duration / time.Second))), Sensitivity: new(w.Sensitivity), DisableDecisionSkip: new(!w.SkipReviewOnCleanRuns), DatasourceUIDs: new(append([]string{}, w.DatasourceUIDs...)), Labels: new(configurationLabels(w.Labels)), AutoStop: stop, Actions: &watchers.WriteActions{Slack: &watchers.ChatAction{Enabled: slack.Enabled, Target: target, MinSeverity: severityToRequest(slack.Severity)}, MSTeams: channel(w.Notifications.Teams), Alerting: &watchers.AlertingAction{Enabled: w.Notifications.Alerting.Enabled, MinSeverity: severityToRequest(w.Notifications.Alerting.Severity), RoutingTree: new(w.Notifications.Alerting.RoutingTree)}, Investigation: &watchers.InvestigationAction{Enabled: w.Investigation.Enabled, TeamNames: append([]string{}, w.Investigation.TeamAccess...)}, Webhook: &watchers.WebhookWriteAction{Enabled: new(w.Notifications.Webhook.Enabled), MinSeverity: new(severityToRequest(w.Notifications.Webhook.Severity))}}}
}
func secretPairs(desired, current Watcher) [][2]*SecretInput {
	return [][2]*SecretInput{{desired.Notifications.Webhook.URL, current.Notifications.Webhook.URL}, {desired.Notifications.Webhook.BearerToken, current.Notifications.Webhook.BearerToken}, {desired.Notifications.Webhook.SigningSecret, current.Notifications.Webhook.SigningSecret}}
}
func supplies(s *SecretInput) bool     { return s != nil && (s.FromEnv != "" || s.FromFile != "") }
func writesSecret(s *SecretInput) bool { return supplies(s) || (s != nil && s.Clear) }
func checkSecretIntent(desired, current Watcher, create bool) error {
	for _, pair := range secretPairs(desired, current) {
		if create && pair[0] != nil && pair[0].Preserve {
			return errors.New("cannot preserve webhook secrets when creating a Watcher; supply a source or omit the secret")
		}
	}
	if writesSecret(desired.Notifications.Webhook.URL) {
		for _, pair := range secretPairs(desired, current)[1:] {
			if pair[1] != nil && !writesSecret(pair[0]) {
				return errors.New("writing the webhook URL requires supply or clear for every saved bearer token and signing secret")
			}
		}
	}
	return nil
}
func secretsChanged(desired, current Watcher) bool {
	for _, p := range secretPairs(desired, current) {
		if supplies(p[0]) || (p[0] != nil && p[0].Clear && p[1] != nil) {
			return true
		}
	}
	return false
}
func resolveSecret(s *SecretInput) (*string, error) {
	if s == nil || s.Preserve {
		return nil, nil //nolint:nilnil // nil is the deliberate preserve/omit operation, not an invalid secret.
	}
	if s.Clear {
		return new(""), nil
	}
	var value string
	if s.FromEnv != "" {
		value = os.Getenv(s.FromEnv)
	} else {
		data, err := os.ReadFile(s.FromFile)
		if err != nil {
			return nil, errors.New("cannot read webhook secret file source")
		}
		value = strings.TrimRight(string(data), "\r\n")
	}
	if value == "" {
		return nil, errors.New("webhook secret source is missing or empty")
	}
	return &value, nil
}
func attachSecrets(request *watchers.ConfigurationRequest, desired, current Watcher) error {
	values := make([]*string, 3)
	for i, p := range secretPairs(desired, current) {
		if !writesSecret(p[0]) {
			continue
		}
		value, err := resolveSecret(p[0])
		if err != nil {
			return err
		}
		values[i] = value
	}
	if values[0] == nil && values[1] == nil && values[2] == nil {
		return nil
	}
	if request.Actions == nil {
		request.Actions = &watchers.WriteActions{}
	}
	if request.Actions.Webhook == nil {
		request.Actions.Webhook = &watchers.WebhookWriteAction{Enabled: new(current.Notifications.Webhook.Enabled), MinSeverity: new(severityToRequest(current.Notifications.Webhook.Severity))}
	}
	webhook := request.Actions.Webhook
	webhook.URL = values[0]
	if values[1] != nil || values[2] != nil {
		webhook.SecureSettings = &watchers.WebhookSecureSettings{AuthorizationCredentials: values[1], HMACSecret: values[2]}
		if values[1] != nil {
			webhook.AuthorizationScheme = new("Bearer")
		}
	}
	return nil
}

// ResolveDeleteIdentity needs only collection identities, not supplementary reads.
func ResolveDeleteIdentity(ctx context.Context, client *watchers.Client, ref string) (Candidate, error) {
	index, err := mutationIndex(ctx, client)
	if err != nil {
		return Candidate{}, err
	}
	return resolveIdentity(index, ref)
}

type identityContextKey struct{}

func mutationIndex(ctx context.Context, client *watchers.Client) (map[string][]Candidate, error) {
	if index, ok := ctx.Value(identityContextKey{}).(map[string][]Candidate); ok {
		return index, nil
	}
	return IdentityIndex(ctx, client)
}

func saveConfiguration(ctx context.Context, client *watchers.Client, desired, current Watcher, id string) (*watchers.Watcher, error) {
	request := configurationRequest(desired)
	if err := attachSecrets(request, desired, current); err != nil {
		return nil, err
	}
	if id == "" {
		return client.Create(ctx, request)
	}
	return client.Update(ctx, id, request)
}
