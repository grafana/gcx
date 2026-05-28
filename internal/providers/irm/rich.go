package irm

// Rich types render the AlertGroup/Alert payloads in the spec/status shape
// that gcx exposes via `irm oncall alert-groups get|list|list-alerts`. They
// differ from the API-shaped AlertGroup/Alert types in this package (which
// mirror the OnCall internal API verbatim).
//
// Empty fields are routinely expected because:
//  - the AlertGroups list endpoint does not return last_alert.raw_request_data,
//    so all fields extracted from the Alertmanager-shaped payload stay empty,
//  - integrations like formatted_webhook/generic webhook do not provide labels
//    or annotations, so the promoted rule/dashboard/instance fields stay empty.
// `omitempty` keeps the YAML tidy in those cases.

// AlertGroupRichMetadata holds identity fields sourced from the raw API
// response that callers need to assemble the K8s envelope (name/PK and
// creation timestamp). They live outside Spec/Status because they describe the
// resource identity, not its observed state or desired configuration.
//
// StartedAt is kept as a string to preserve the wire format exactly — the
// OnCall API uses several RFC3339-ish layouts (with and without trailing Z) and
// re-formatting through time.Time would require error-prone multi-layout
// parsing. Callers that need a parsed time can use the formatRelativeAge helper.
//
// GetAlertGroupRich returns only *AlertGroupRich (not *alertGroupAPI). Callers
// that need Labels for metadata.labels assembly still receive the raw API struct
// via the list path (listAlertGroupRichFromBytes) and the envelope helpers.
type AlertGroupRichMetadata struct {
	PK        string `json:"pk,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
}

// AlertGroupRich is the K8s-envelope spec/status payload for an AlertGroup.
// It is marshalled to a top-level {"spec": ..., "status": ...} object so the
// command layer can lift those keys directly into the K8s envelope.
type AlertGroupRich struct {
	Metadata *AlertGroupRichMetadata `json:"metadata,omitempty"`
	Spec     AlertGroupSpec          `json:"spec"`
	Status   AlertGroupStatus        `json:"status"`
}

// AlertGroupSpec captures stable identity-ish metadata about the alert group.
type AlertGroupSpec struct {
	Integration IntegrationRef  `json:"integration"`
	Team        *TeamRef        `json:"team,omitempty"`
	Permalinks  AlertGroupLinks `json:"permalinks"`
}

// AlertGroupStatus captures the live state of the alert group, including
// fields promoted out of the Alertmanager-shaped payload.
type AlertGroupStatus struct {
	Title       string           `json:"title,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	Severity    string           `json:"severity,omitempty"`
	State       string           `json:"state,omitempty"`
	RunbookURL  string           `json:"runbookURL,omitempty"`
	Subject     *AlertSubject    `json:"subject,omitempty"`
	Timestamps  *AlertTimestamps `json:"timestamps,omitempty"`
	Links       *AlertLinks      `json:"links,omitempty"`
	AlertsCount int              `json:"alertsCount,omitempty"`
	Raw         *AlertGroupRaw   `json:"raw,omitempty"`
}

// AlertSubject groups the labels that describe the alert group's subject —
// the entities (clusters, services, namespaces, ...) the alert is about.
// Sourced from the filtered commonLabels (denylist applied — see
// labelDenylist) on the get path; on the list path, populated best-effort
// from render_for_web.message HTML scraping (subset of commonLabels).
type AlertSubject struct {
	Labels map[string]string `json:"labels,omitempty"`
}

// AlertDimensions groups the per-alert label diff against the parent group's
// commonLabels. A label key+value present in commonLabels is omitted; a key
// absent from commonLabels OR carrying a different value is kept.
type AlertDimensions struct {
	Labels map[string]string `json:"labels,omitempty"`
}

// AlertLinks groups the cross-provider pivot identifiers and URLs reachable
// from this alert: the rule that fired, this firing instance, the linked
// dashboard, and (when applicable) the backing SLO.
type AlertLinks struct {
	Alert     *AlertLinkAlert `json:"alert,omitempty"`
	Dashboard *AlertDashboard `json:"dashboard,omitempty"`
	SLO       *AlertLinkSLO   `json:"slo,omitempty"`
}

// AlertLinkAlert pairs the alert rule and its specific firing instance.
type AlertLinkAlert struct {
	Rule     *AlertRule     `json:"rule,omitempty"`
	Instance *AlertInstance `json:"instance,omitempty"`
}

// AlertLinkSLO identifies the Grafana SLO this alert measures, when present.
// Pivot via `gcx slo definitions get <uid>`.
type AlertLinkSLO struct {
	UID  string `json:"uid,omitempty"`
	Name string `json:"name,omitempty"`
}

// IntegrationRef identifies the OnCall integration that produced the group.
type IntegrationRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// TeamRef identifies the OnCall team owning the alert group.
type TeamRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// AlertGroupLinks holds the OnCall-rendered permalinks.
type AlertGroupLinks struct {
	Web      string `json:"web,omitempty"`
	Slack    string `json:"slack,omitempty"`
	SlackApp string `json:"slack_app,omitempty"`
	Telegram string `json:"telegram,omitempty"`
}

// AlertTimestamps groups the lifecycle timestamps of the AlertGroup.
type AlertTimestamps struct {
	Started      string `json:"started,omitempty"`
	Acknowledged string `json:"acknowledged,omitempty"`
	Resolved     string `json:"resolved,omitempty"`
	Silenced     string `json:"silenced,omitempty"`
}

// AlertRule identifies the upstream Grafana alert rule.
type AlertRule struct {
	UID string `json:"uid,omitempty"`
	URL string `json:"url,omitempty"`
}

// AlertInstance identifies a single alert instance within a group.
type AlertInstance struct {
	ID         string `json:"id,omitempty"`
	SilenceURL string `json:"silenceURL,omitempty"`
}

// AlertDashboard describes the Grafana dashboard / panel context for the alert.
type AlertDashboard struct {
	UID   string      `json:"uid,omitempty"`
	URL   string      `json:"url,omitempty"`
	Panel *AlertPanel `json:"panel,omitempty"`
}

// AlertPanel identifies a panel on the linked dashboard.
type AlertPanel struct {
	ID  int    `json:"id,omitempty"`
	URL string `json:"url,omitempty"`
}

// AlertGroupRaw is the subset of raw_request_data preserved on the AlertGroup
// for diagnostic / power-user use.
type AlertGroupRaw struct {
	CommonLabels      map[string]string `json:"commonLabels,omitempty"`
	CommonAnnotations map[string]string `json:"commonAnnotations,omitempty"`
	GroupLabels       map[string]string `json:"groupLabels,omitempty"`
}

// AlertRich is the K8s-envelope spec/status payload for a single Alert.
type AlertRich struct {
	Spec   AlertSpec   `json:"spec"`
	Status AlertStatus `json:"status"`
}

// AlertSpec captures back-pointer identity for the alert.
type AlertSpec struct {
	AlertGroupID string `json:"alertGroupID,omitempty"`
}

// AlertStatus captures the rendered state of an alert plus the full payload.
//
// Raw is the unprocessed Alertmanager-shape group webhook (= the API's
// raw_request_data). Hidden by default and gated behind `--include-raw` on
// the CLI; the extracted fields above (dimensions/links/...) are the curated
// promotion of the same data.
//
// Dimensions captures the per-alert label diff vs the parent group's
// commonLabels (set difference by VALUE — a label key+value matching
// commonLabels is omitted; a key absent from commonLabels or carrying a
// different value is included).
//
// Occurrences is the count of stored alerts (re-fires) sharing this alert's
// label set within the parent group. Under default collapse mode, an
// occurrences value > 1 means N AM re-fires were collapsed into this row.
// `--history` opts out of collapse and forces this to 1.
type AlertStatus struct {
	State       string           `json:"state,omitempty"`
	Severity    string           `json:"severity,omitempty"`
	Dimensions  *AlertDimensions `json:"dimensions,omitempty"`
	Links       *AlertLinks      `json:"links,omitempty"`
	Occurrences int              `json:"occurrences,omitempty"`
	Raw         *AlertPayload    `json:"raw,omitempty"`
}

// AlertPayload is the Alertmanager-shape group webhook (raw_request_data).
type AlertPayload struct {
	Status            string              `json:"status,omitempty"`
	GroupKey          string              `json:"groupKey,omitempty"`
	ExternalURL       string              `json:"externalURL,omitempty"`
	Receiver          string              `json:"receiver,omitempty"`
	NumFiring         int                 `json:"numFiring,omitempty"`
	NumResolved       int                 `json:"numResolved,omitempty"`
	TruncatedAlerts   int                 `json:"truncatedAlerts,omitempty"`
	GroupLabels       map[string]string   `json:"groupLabels,omitempty"`
	CommonLabels      map[string]string   `json:"commonLabels,omitempty"`
	CommonAnnotations map[string]string   `json:"commonAnnotations,omitempty"`
	Alerts            []AlertmanagerAlert `json:"alerts,omitempty"`
}

// AlertmanagerAlert mirrors the alerts[] entry of the Alertmanager webhook.
// We keep the optional first-class fields the OnCall backend adds for
// grafana_alerting integrations (ruleUID, dashboardURL, panelURL, silenceURL).
type AlertmanagerAlert struct {
	Status       string            `json:"status,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
	StartsAt     string            `json:"startsAt,omitempty"`
	EndsAt       string            `json:"endsAt,omitempty"`

	// grafana_alerting first-class fields (absent on alertmanager / webhook integrations).
	RuleUID      string `json:"ruleUID,omitempty"`
	DashboardURL string `json:"dashboardURL,omitempty"`
	PanelURL     string `json:"panelURL,omitempty"`
	SilenceURL   string `json:"silenceURL,omitempty"`
}
