package irm

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// alertGroupAPI is the partially-parsed API shape returned by
// GET /alertgroups/<id>/ and the items in /alertgroups/?... .
//
// Fields we don't need are left as json.RawMessage so we can pass them through
// (or skip parsing them entirely on the list path where they're absent).
type alertGroupAPI struct {
	PK             string `json:"pk"`
	AlertsCount    int    `json:"alerts_count"`
	Status         *int   `json:"status"`
	StartedAt      string `json:"started_at"`
	ResolvedAt     string `json:"resolved_at"`
	AcknowledgedAt string `json:"acknowledged_at"`
	SilencedAt     string `json:"silenced_at"`

	// Permalinks is `{web, slack, slack_app, telegram}` — sometimes null.
	Permalinks json.RawMessage `json:"permalinks"`

	// AlertReceiveChannel is an integration object on the get endpoint, often
	// just an ID or null on related endpoints. We only consume the object form.
	AlertReceiveChannel json.RawMessage `json:"alert_receive_channel"`

	// Team is an object {pk, name, ...} on get; on list it can be a string ID.
	Team json.RawMessage `json:"team"`

	// RenderForWeb has fields {title, message, image_url, source_link} — title is what we want.
	RenderForWeb json.RawMessage `json:"render_for_web"`

	// LastAlert is an object that, on the get endpoint, includes
	// `raw_request_data` (the original Alertmanager webhook payload).
	LastAlert json.RawMessage `json:"last_alert"`

	// Labels is the OnCall app's user-set labels[] array.
	Labels json.RawMessage `json:"labels"`
}

// alertAPI is the partially-parsed shape from GET /alerts/<id>/.
type alertAPI struct {
	ID             string          `json:"id"`
	AlertGroupID   string          `json:"alert_group"` // sometimes "alert_group_pk" / "alert_group" depending on path
	AlertGroupPK   string          `json:"alert_group_pk"`
	CreatedAt      string          `json:"created_at"`
	RawRequestData json.RawMessage `json:"raw_request_data"`
}

// decodeAlertGroupState converts the OnCall integer state enum into the lowercase
// string the rich type emits. The mapping comes from the AlertGroup model:
//
//	0 -> firing, 1 -> acknowledged, 2 -> resolved, 3 -> silenced.
func decodeAlertGroupState(state *int) string {
	if state == nil {
		return ""
	}
	switch *state {
	case 0:
		return "firing"
	case 1:
		return "acknowledged"
	case 2:
		return "resolved"
	case 3:
		return "silenced"
	default:
		return fmt.Sprintf("unknown(%d)", *state)
	}
}

// firstNonEmpty returns the first non-empty argument (or "" if all empty).
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseStringMap decodes the given JSON bytes into map[string]string.
// Returns nil when the input is empty or invalid (non-map / non-string values).
func parseStringMap(data json.RawMessage) map[string]string {
	if len(data) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		return m
	}
	// Some payloads have heterogeneous values; coerce to strings.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch t := v.(type) {
		case string:
			out[k] = t
		case bool:
			out[k] = strconv.FormatBool(t)
		case float64:
			out[k] = strconv.FormatFloat(t, 'g', -1, 64)
		case nil:
			// skip
		default:
			b, err := json.Marshal(t)
			if err == nil {
				out[k] = string(b)
			}
		}
	}
	return out
}

// rawRequestData represents the Alertmanager-shape webhook body that OnCall
// stores under last_alert.raw_request_data (or alert.raw_request_data).
type rawRequestData struct {
	Status            string          `json:"status"`
	GroupKey          string          `json:"groupKey"`
	ExternalURL       string          `json:"externalURL"`
	Receiver          string          `json:"receiver"`
	NumFiring         int             `json:"numFiring"`
	NumResolved       int             `json:"numResolved"`
	TruncatedAlerts   int             `json:"truncatedAlerts"`
	GroupLabels       json.RawMessage `json:"groupLabels"`
	CommonLabels      json.RawMessage `json:"commonLabels"`
	CommonAnnotations json.RawMessage `json:"commonAnnotations"`
	Alerts            []amAlertRaw    `json:"alerts"`
}

// amAlertRaw mirrors a single entry in raw_request_data.alerts[] before normalization.
type amAlertRaw struct {
	Status       string          `json:"status"`
	Labels       json.RawMessage `json:"labels"`
	Annotations  json.RawMessage `json:"annotations"`
	Fingerprint  string          `json:"fingerprint"`
	GeneratorURL string          `json:"generatorURL"`
	StartsAt     string          `json:"startsAt"`
	EndsAt       string          `json:"endsAt"`

	// grafana_alerting first-class fields.
	RuleUID      string `json:"ruleUID"`
	DashboardURL string `json:"dashboardURL"`
	PanelURL     string `json:"panelURL"`
	SilenceURL   string `json:"silenceURL"`
}

// dashboardUIDPattern parses the {uid} segment of a Grafana /d/{uid}/{slug}? URL.
var dashboardUIDPattern = regexp.MustCompile(`/d/([A-Za-z0-9_-]+)`)

// titleTargetSuffixPattern matches a trailing "(cluster, namespace[, ...])"
// suffix on render_for_web titles. OnCall concatenates target labels into the
// title server-side ("KubePodNotReady (grafana-apps)" /
// "CloudSQLSlowQueries (prod-eu-west-2, machine-learning)"). The list-table
// codec already renders cluster/service/namespace in their own columns; we
// strip the parenthetical here so the TITLE column shows just the alert name.
//
// Strips trailing parentheticals that contain 1–3 comma-separated identifiers
// (alphanumerics, dashes, underscores, dots) and nothing else. Single-token
// parens are included: "KubePodNotReady (grafana-apps)" →
// "KubePodNotReady". Free-form prose parens (spaces, punctuation, mixed case)
// are left alone because they won't match the identifier-only pattern.
var titleTargetSuffixPattern = regexp.MustCompile(`\s*\(([A-Za-z0-9_.\-]+(?:,\s*[A-Za-z0-9_.\-]+){0,2})\)\s*$`)

// stripTitleTargetSuffix removes the trailing "(target, ...)" parenthetical
// that OnCall server-side prepends to render_for_web titles. See pattern docs.
func stripTitleTargetSuffix(title string) string {
	return titleTargetSuffixPattern.ReplaceAllString(title, "")
}

// renderForWebSeverityPattern picks the value of a `severity:` line out of the
// rendered HTML message OnCall returns under render_for_web.message. The list
// endpoint omits last_alert.raw_request_data, so the structured severity
// extraction in buildAlertGroupRich is empty for list rows. The HTML message
// embeds the same labels in `<li>severity: <value></li>` form, which we can
// read as a fallback so the SEVERITY column on `alert-groups list` is not
// uniformly "-".
var renderForWebSeverityPattern = regexp.MustCompile(`<li>\s*severity:\s*([^<\s]+)`)

// extractSeverityFromRenderForWeb pulls the severity value out of the
// render_for_web HTML message body when present. Returns "" on no match.
func extractSeverityFromRenderForWeb(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var rfw struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &rfw); err != nil {
		return ""
	}
	if rfw.Message == "" {
		return ""
	}
	m := renderForWebSeverityPattern.FindStringSubmatch(rfw.Message)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// parseDashboardUIDFromURL extracts the dashboard UID from a /d/<uid>/<slug>... URL.
// Returns "" on no match or an empty input.
func parseDashboardUIDFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	m := dashboardUIDPattern.FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// parsePanelIDFromURL extracts the integer viewPanel query param from a Grafana panel URL.
// Returns 0 on missing/invalid input.
func parsePanelIDFromURL(rawURL string) int {
	if rawURL == "" {
		return 0
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	v := u.Query().Get("viewPanel")
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// extractRule applies the dual-shape fallback for the rule.uid / rule.url pair
// against the first alert in `alerts[]` and the commonLabels map.
//
// Order:
//  1. alerts[0].ruleUID (grafana_alerting's first-class field)
//  2. alerts[0].labels.__alert_rule_uid__ (alertmanager-via-AM shape)
//  3. commonLabels.__alert_rule_uid__
//
// For URL we use alerts[0].generatorURL. Returns nil when neither UID nor URL
// could be extracted, so the caller can omit the block entirely.
func extractRule(first *amAlertRaw, alertLabels, commonLabels map[string]string) *AlertRule {
	uid := ""
	if first != nil {
		uid = firstNonEmpty(first.RuleUID, alertLabels["__alert_rule_uid__"])
	}
	uid = firstNonEmpty(uid, commonLabels["__alert_rule_uid__"])

	url := ""
	if first != nil {
		url = first.GeneratorURL
	}
	if uid == "" && url == "" {
		return nil
	}
	return &AlertRule{UID: uid, URL: url}
}

// extractDashboard applies the dual-shape fallback for dashboard.uid / dashboard.url
// and the nested panel block. Returns nil when no dashboard link is available.
func extractDashboard(first *amAlertRaw, alertAnnotations, commonAnnotations map[string]string) *AlertDashboard {
	if first == nil {
		first = &amAlertRaw{}
	}

	url := firstNonEmpty(
		first.DashboardURL,
		alertAnnotations["dashboard_url"],
		alertAnnotations["dashboardURL"],
	)
	uid := firstNonEmpty(
		alertAnnotations["__dashboardUid__"],
		alertAnnotations["dashboard_uid"],
		alertAnnotations["dashboardUID"],
		commonAnnotations["__dashboardUid__"],
		parseDashboardUIDFromURL(url),
		parseDashboardUIDFromURL(first.PanelURL),
	)

	panelURL := firstNonEmpty(first.PanelURL, alertAnnotations["panel_url"])
	panelID := 0
	if v := alertAnnotations["__panelId__"]; v != "" {
		panelID, _ = strconv.Atoi(v)
	}
	if panelID == 0 {
		if v := commonAnnotations["__panelId__"]; v != "" {
			panelID, _ = strconv.Atoi(v)
		}
	}
	if panelID == 0 {
		panelID = parsePanelIDFromURL(panelURL)
	}

	if uid == "" && url == "" && panelID == 0 && panelURL == "" {
		return nil
	}

	dash := &AlertDashboard{
		UID: uid,
		URL: url,
	}
	if panelID != 0 || panelURL != "" {
		dash.Panel = &AlertPanel{
			ID:  panelID,
			URL: panelURL,
		}
	}
	return dash
}

// extractSLO extracts the Grafana SLO uid + name when the alert is backed by
// an SLO definition. Source: labels.grafana_slo_uuid + annotations.slo_name.
// Returns nil when no SLO link is present.
func extractSLO(labels, annotations map[string]string) *AlertLinkSLO {
	uid := firstNonEmpty(labels["grafana_slo_uuid"], labels["grafana_slo_uid"])
	name := annotations["slo_name"]
	if uid == "" && name == "" {
		return nil
	}
	return &AlertLinkSLO{UID: uid, Name: name}
}

// buildAlertLinks composes the rule / instance / dashboard / slo blocks into
// a single Links struct. Returns nil when none of the four are populated.
func buildAlertLinks(rule *AlertRule, instance *AlertInstance, dashboard *AlertDashboard, slo *AlertLinkSLO) *AlertLinks {
	var alertLink *AlertLinkAlert
	if rule != nil || instance != nil {
		alertLink = &AlertLinkAlert{Rule: rule, Instance: instance}
	}
	if alertLink == nil && dashboard == nil && slo == nil {
		return nil
	}
	return &AlertLinks{
		Alert:     alertLink,
		Dashboard: dashboard,
		SLO:       slo,
	}
}

// labelDenylistExact lists noise label keys that should NEVER appear in the
// promoted subject/dimensions views. Two groups:
//
//   - Prom / Grafana internals already promoted to dedicated status fields
//     (alertname, severity, grafana_folder).
//   - Annotation-style keys that the OnCall HTML scrape pulls in alongside
//     real labels on the list path (description, summary, runbook_url,
//     message, dashboard_url, documentation_url, playbook_url). These are
//     long free-form strings that bloat SUBJECT cells.
//
// `__`-prefixed keys (single or wrapped) are caught by the prefix rule in
// isDenylistedLabelKey, so they don't need explicit entries here.
var labelDenylistExact = map[string]bool{ //nolint:gochecknoglobals
	"alertname":         true,
	"severity":          true,
	"grafana_folder":    true,
	"description":       true,
	"summary":           true,
	"runbook_url":       true,
	"message":           true,
	"dashboard_url":     true,
	"documentation_url": true,
	"playbook_url":      true,
}

// isDenylistedLabelKey reports whether a label key should be filtered out of
// the subject/dimensions view. The denylist combines a prefix-match for ANY
// `__`-prefixed key (covers wrap-style `__name__` AND single-leading like
// `__enriched_by`, `__bypass_imported_global_am_allowlist`,
// `__grafana_origin`, `__ai_explanation`, `__enrich_logs_lines`) with the
// explicit annotation/promoted list above.
func isDenylistedLabelKey(k string) bool {
	if k == "" {
		return true
	}
	if labelDenylistExact[k] {
		return true
	}
	if strings.HasPrefix(k, "__") {
		return true
	}
	return false
}

// filterLabels returns a copy of `in` with denylisted keys removed. Returns
// nil when no labels remain so callers can decide whether to omit the field.
func filterLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if isDenylistedLabelKey(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// extractSubject builds an AlertSubject from the (already-extracted)
// commonLabels map by applying the denylist filter. Returns nil when no
// non-noise labels remain so callers can omit the block.
func extractSubject(commonLabels map[string]string) *AlertSubject {
	filtered := filterLabels(commonLabels)
	if len(filtered) == 0 {
		return nil
	}
	return &AlertSubject{Labels: filtered}
}

// extractDimensions returns the per-alert label diff vs commonLabels. Set
// difference by VALUE: a label key+value matching commonLabels exactly is
// omitted; a key absent from commonLabels OR carrying a different value is
// kept. The denylist is applied AFTER the diff so internal/promoted keys
// don't leak through. Returns nil when no diff remains.
func extractDimensions(alertLabels, commonLabels map[string]string) *AlertDimensions {
	if len(alertLabels) == 0 {
		return nil
	}
	diff := make(map[string]string, len(alertLabels))
	for k, v := range alertLabels {
		if cv, ok := commonLabels[k]; ok && cv == v {
			continue
		}
		diff[k] = v
	}
	filtered := filterLabels(diff)
	if len(filtered) == 0 {
		return nil
	}
	return &AlertDimensions{Labels: filtered}
}

// renderForWebLabelLinePattern matches a single `<li>key: value</li>` entry
// from the OnCall server-rendered HTML in render_for_web.message. The list
// endpoint omits last_alert.raw_request_data, so structured commonLabels
// extraction is empty for list rows; the HTML message embeds the same
// labels in `<li>key: value</li>` form, which we scrape here as a fallback
// so the SUBJECT column on `alert-groups list` is populated.
//
// Conservative: keys are alphanumerics + underscores + dots. Values run
// to end-of-line / `<` / `\n`, with optional trailing whitespace stripped.
var renderForWebLabelLinePattern = regexp.MustCompile(`<li>\s*([A-Za-z_][A-Za-z0-9_.]*?)\s*:\s*([^<\n]+?)\s*(?:<|$)`)

// extractCommonLabelsFromRenderForWeb scrapes commonLabels out of the
// render_for_web.message HTML body. Used on the list path where the API
// omits last_alert.raw_request_data. Returns an unfiltered map (caller
// applies the denylist via filterLabels / extractSubject).
func extractCommonLabelsFromRenderForWeb(data json.RawMessage) map[string]string {
	if len(data) == 0 {
		return nil
	}
	var rfw struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &rfw); err != nil {
		return nil
	}
	if rfw.Message == "" {
		return nil
	}
	matches := renderForWebLabelLinePattern.FindAllStringSubmatch(rfw.Message, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make(map[string]string, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		key := strings.TrimSpace(m[1])
		val := strings.TrimSpace(m[2])
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// canonicalLabelKey returns a stable string-identity for a labels map suitable
// for collapse keying: keys sorted ascending, each rendered as "k=v" and
// joined by '\x1f' (unit separator, won't appear in label values). The empty
// map returns "" so collapse-by-labels-equality works correctly for the
// "no per-alert dimensions" case.
func canonicalLabelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\x1f')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

// extractTitleFromRenderForWeb pulls the "title" field out of the render_for_web
// blob the OnCall API attaches to alert groups, stripping the trailing
// "(cluster, namespace)" target suffix OnCall concatenates server-side. The
// alert name (e.g. "KubePodNotReady") is what users want in the TITLE column;
// targets render in their own columns.
func extractTitleFromRenderForWeb(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var rfw struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &rfw); err != nil {
		return ""
	}
	return stripTitleTargetSuffix(rfw.Title)
}

// extractIntegrationRef parses the alert_receive_channel object from an alert
// group response. The internal API returns an object on the get endpoint and
// either an object or a bare ID string on list endpoints.
func extractIntegrationRef(data json.RawMessage) IntegrationRef {
	if len(data) == 0 || string(data) == "null" {
		return IntegrationRef{}
	}
	var asObj struct {
		ID         string `json:"id"`
		VerbalName string `json:"verbal_name"`
		Type       string `json:"integration"`
	}
	if err := json.Unmarshal(data, &asObj); err == nil && asObj.ID != "" {
		return IntegrationRef{
			ID:   asObj.ID,
			Name: asObj.VerbalName,
			Type: asObj.Type,
		}
	}
	// fallback: bare ID string.
	var asStr string
	if err := json.Unmarshal(data, &asStr); err == nil {
		return IntegrationRef{ID: asStr}
	}
	return IntegrationRef{}
}

// extractTeamID returns the team identifier (PK) from the API's team field,
// which can be a string ID, a {pk: ...} object, or null.
func extractTeamID(data json.RawMessage) string {
	if len(data) == 0 || string(data) == "null" {
		return ""
	}
	var asStr string
	if err := json.Unmarshal(data, &asStr); err == nil && asStr != "" {
		return asStr
	}
	var asObj struct {
		PK string `json:"pk"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &asObj); err == nil {
		return firstNonEmpty(asObj.PK, asObj.ID)
	}
	return ""
}

// extractAlertGroupLinks decodes the permalinks blob.
func extractAlertGroupLinks(data json.RawMessage) AlertGroupLinks {
	if len(data) == 0 || string(data) == "null" {
		return AlertGroupLinks{}
	}
	var links AlertGroupLinks
	_ = json.Unmarshal(data, &links)
	return links
}

// extractRawRequestDataFromLastAlert pulls raw_request_data out of the last_alert
// object embedded on an alert group response (only present on the get endpoint).
func extractRawRequestDataFromLastAlert(lastAlert json.RawMessage) (rawRequestData, bool) {
	var out rawRequestData
	if len(lastAlert) == 0 || string(lastAlert) == "null" {
		return out, false
	}
	var wrapper struct {
		Raw json.RawMessage `json:"raw_request_data"`
	}
	if err := json.Unmarshal(lastAlert, &wrapper); err != nil {
		return out, false
	}
	if len(wrapper.Raw) == 0 || string(wrapper.Raw) == "null" {
		return out, false
	}
	if err := json.Unmarshal(wrapper.Raw, &out); err != nil {
		return out, false
	}
	return out, true
}

// firstAlertOrNil returns &alerts[0] if the slice is non-empty, else nil.
func firstAlertOrNil(alerts []amAlertRaw) *amAlertRaw {
	if len(alerts) == 0 {
		return nil
	}
	return &alerts[0]
}

// firstAlertRuleUID returns the first non-empty status.links.alert.rule.uid
// across the alert envelopes. Used by the list-alerts post-result hint
// emission (D2 round 17) to render `gcx alert rules get <uid>` and
// `gcx alert instances list --rule <uid>` hints with a concrete UID. When
// alerts span multiple rules, the first occurrence wins (avoids hint noise).
// Returns "" when no alert carries a rule UID.
func firstAlertRuleUID(envs []alertEnvelope) string {
	for _, env := range envs {
		if env.Status.Links == nil || env.Status.Links.Alert == nil || env.Status.Links.Alert.Rule == nil {
			continue
		}
		if uid := env.Status.Links.Alert.Rule.UID; uid != "" {
			return uid
		}
	}
	return ""
}

// toAlertmanagerAlerts normalizes amAlertRaw entries into the public AlertmanagerAlert shape.
func toAlertmanagerAlerts(in []amAlertRaw) []AlertmanagerAlert {
	if len(in) == 0 {
		return nil
	}
	out := make([]AlertmanagerAlert, len(in))
	for i, a := range in {
		out[i] = AlertmanagerAlert{
			Status:       a.Status,
			Labels:       parseStringMap(a.Labels),
			Annotations:  parseStringMap(a.Annotations),
			Fingerprint:  a.Fingerprint,
			GeneratorURL: a.GeneratorURL,
			StartsAt:     a.StartsAt,
			EndsAt:       a.EndsAt,
			RuleUID:      a.RuleUID,
			DashboardURL: a.DashboardURL,
			PanelURL:     a.PanelURL,
			SilenceURL:   a.SilenceURL,
		}
	}
	return out
}
