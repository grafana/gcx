package irm

import (
	"time"
)

// FlexTime is a time.Time that accepts empty strings from JSON (treating them as zero time).
// The IRM API sometimes returns empty strings for optional time fields.
type FlexTime time.Time

func (ft *FlexTime) UnmarshalJSON(data []byte) error {
	if string(data) == `""` || string(data) == "null" {
		return nil
	}
	var t time.Time
	if err := t.UnmarshalJSON(data); err != nil {
		return err
	}
	*ft = FlexTime(t)
	return nil
}

func (ft FlexTime) MarshalJSON() ([]byte, error) {
	t := time.Time(ft)
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return t.MarshalJSON()
}

// GetResourceName returns the incident ID.
func (i Incident) GetResourceName() string { return i.IncidentID }

// SetResourceName restores the incident ID.
func (i *Incident) SetResourceName(name string) { i.IncidentID = name }

// Incident represents an incident from the IRM API.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Incident struct {
	IncidentID string `json:"incidentID,omitempty"`
	Title      string `json:"title"`
	Slug       string `json:"slug,omitempty"`
	Prefix     string `json:"prefix,omitempty"`
	Status     string `json:"status"`
	StatusID   string `json:"statusID,omitempty"`
	State      string `json:"state,omitempty"`
	// Severity is the display label, for example "Critical". gcx ignores it
	// when SeverityID is not empty.
	Severity string `json:"severity,omitempty"`
	// SeverityID is the severity identifier of the organization. A
	// hand-written manifest can carry it, and gcx then resolves the label from
	// the organization severity list. A pulled manifest carries the label
	// alone, because the pull removes this field.
	SeverityID              string               `json:"severityID,omitempty"`
	IsDrill                 bool                 `json:"isDrill"`
	IncidentType            string               `json:"incidentType,omitempty"`
	Description             string               `json:"description,omitempty"`
	Summary                 string               `json:"summary,omitempty"`
	OverviewURL             string               `json:"overviewURL,omitempty"`
	FieldGroupUUID          string               `json:"fieldGroupUUID,omitempty"`
	DurationSeconds         int                  `json:"durationSeconds,omitempty"`
	Version                 int                  `json:"version,omitempty"`
	Labels                  []IncidentLabel      `json:"labels,omitempty"`
	FieldValues             []IncidentFieldValue `json:"fieldValues,omitempty"`
	Refs                    []IncidentRef        `json:"refs,omitempty"`
	IncidentChannels        []any                `json:"incidentChannels,omitempty"`
	IncidentMembership      *IncidentMembership  `json:"incidentMembership,omitempty"`
	IncidentHookRuns        *IncidentHookRuns    `json:"incidentHookRuns,omitempty"`
	TaskList                *IncidentTaskList    `json:"taskList,omitempty"`
	CreatedByUser           *IncidentUser        `json:"createdByUser,omitempty"`
	DescriptionUser         *IncidentUser        `json:"descriptionUser,omitempty"`
	StatusModifiedByUser    *IncidentUser        `json:"statusModifiedByUser,omitempty"`
	CreatedTime             FlexTime             `json:"createdTime,omitzero"`
	ModifiedTime            FlexTime             `json:"modifiedTime,omitzero"`
	ClosedTime              FlexTime             `json:"closedTime,omitzero"`
	IncidentStart           FlexTime             `json:"incidentStart,omitzero"`
	IncidentEnd             FlexTime             `json:"incidentEnd,omitzero"`
	DescriptionModifiedTime FlexTime             `json:"descriptionModifiedTime,omitzero"`
	StatusModifiedTime      FlexTime             `json:"statusModifiedTime,omitzero"`

	// updatedFields carries command result metadata through TypedCRUD.Update.
	// It is not an IRM API field and is never serialized in a resource.
	updatedFields []string
}

// IncidentUser represents a user referenced in incident fields.
type IncidentUser struct {
	UserID        string `json:"userID"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	GrafanaLogin  string `json:"grafanaLogin"`
	PhotoURL      string `json:"photoURL"`
	SlackUserID   string `json:"slackUserID"`
	ChatbotUserID string `json:"chatbotUserID"`
	MSTeamsUserID string `json:"msTeamsUserID"`
}

// IncidentFieldValue represents an entry in the fieldValues array.
type IncidentFieldValue struct {
	FieldUUID string `json:"fieldUUID"`
	Value     string `json:"value"`
}

// IncidentRef represents an entry in the refs array.
type IncidentRef struct {
	Key string `json:"key"`
	Ref string `json:"ref"`
	URL string `json:"url"`
}

// IncidentHookRuns represents the incidentHookRuns object.
type IncidentHookRuns struct {
	HookRuns []any `json:"hookRuns"`
}

// HookRunField is a key/value entry in a hook run's metadata.
type HookRunField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// HookRunMetadata carries what a hook run recorded about its result. The
// template-copying hooks set both URL and a fileURL entry in Fields to the
// document they created.
type HookRunMetadata struct {
	Fields []HookRunField `json:"fields,omitempty"`
	URL    string         `json:"url,omitempty"`
}

// HookRun is a single integration hook execution against an incident. Only
// the fields the PIR lookup reads are modelled; the API returns more.
type HookRun struct {
	HookID   string           `json:"hookID"`
	Metadata *HookRunMetadata `json:"metadata,omitempty"`
	LastRun  FlexTime         `json:"lastRun"`
}

// getHookRunsRequest is the request body for IntegrationService.GetHookRuns.
type getHookRunsRequest struct {
	IncidentID string `json:"incidentID"`
}

// getHookRunsResponse is the response from IntegrationService.GetHookRuns.
type getHookRunsResponse struct {
	HookRuns []HookRun `json:"hookRuns"`
	Error    string    `json:"error,omitempty"`
}

// IncidentPIR is the resolved post-incident review document for an incident.
// PIRURL is empty when the incident has no PIR document.
type IncidentPIR struct {
	IncidentID string `json:"incidentID"`
	PIRURL     string `json:"pirURL"`
}

// IncidentMembershipRole represents a role inside a membership assignment.
type IncidentMembershipRole struct {
	RoleID      int      `json:"roleID"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	OrgID       string   `json:"orgID"`
	Important   bool     `json:"important"`
	Mandatory   bool     `json:"mandatory"`
	Hidden      bool     `json:"hidden"`
	Archived    bool     `json:"archived"`
	CreatedAt   FlexTime `json:"createdAt"`
	UpdatedAt   FlexTime `json:"updatedAt"`
}

// IncidentMembershipAssignment represents a single assignment in incidentMembership.
type IncidentMembershipAssignment struct {
	RoleID int                    `json:"roleID"`
	Role   IncidentMembershipRole `json:"role"`
	User   IncidentUser           `json:"user"`
}

// IncidentMembership represents the incidentMembership object.
type IncidentMembership struct {
	Assignments       []IncidentMembershipAssignment `json:"assignments"`
	TotalAssignments  int                            `json:"totalAssignments"`
	TotalParticipants int                            `json:"totalParticipants"`
}

// IncidentTask represents a single task in taskList.
type IncidentTask struct {
	TaskID       string       `json:"taskID"`
	Text         string       `json:"text"`
	Status       string       `json:"status"`
	StatusKind   string       `json:"statusKind"`
	Order        int          `json:"order"`
	Immutable    bool         `json:"immutable"`
	AuthorUser   IncidentUser `json:"authorUser"`
	AssignedUser any          `json:"assignedUser"`
	Context      any          `json:"context"`
	CreatedTime  FlexTime     `json:"createdTime"`
	ModifiedTime FlexTime     `json:"modifiedTime"`
}

// IncidentTaskList represents the taskList object.
type IncidentTaskList struct {
	Tasks      []IncidentTask `json:"tasks"`
	DoneCount  int            `json:"doneCount"`
	TodoCount  int            `json:"todoCount"`
	TotalCount int            `json:"totalCount"`
}

// IncidentLabel represents a label on an incident.
type IncidentLabel struct {
	Key         string `json:"key"`
	KeyUUID     string `json:"keyUUID,omitempty"`
	Label       string `json:"label,omitempty"`
	Value       string `json:"value,omitempty"`
	LabelUUID   string `json:"labelUUID,omitempty"`
	ColorHex    string `json:"colorHex,omitempty"`
	Description string `json:"description,omitempty"`
}

// IncidentQuery holds the parameters of a List call.
type IncidentQuery struct {
	Limit          int
	OrderDirection string
	OrderField     string
	DateFrom       *FlexTime
	DateTo         *FlexTime
	IncidentLabels []string
	// Statuses filters by incident status (active/resolved). Multiple values
	// are ORed together.
	Statuses []string
	// Severity filters by severity label (e.g. "major").
	Severity string
	// QueryString is a raw incident query-string-language expression. When
	// set it is used verbatim and the structured filters above are ignored;
	// the list command rejects combining them.
	QueryString string
}

// updateStatusRequest is the request body for updating incident status.
type updateStatusRequest struct {
	IncidentID string `json:"incidentID"`
	Status     string `json:"status"`
}

// updateStatusResponse wraps the updated incident. UpdateSeverity and
// UpdateTitle answer with the same envelope.
type updateStatusResponse struct {
	Incident Incident `json:"incident"`
}

// updateSeverityRequest is the request body for IncidentsService.UpdateSeverity.
// The API takes the severity label, not the severity identifier.
type updateSeverityRequest struct {
	IncidentID string `json:"incidentID"`
	Severity   string `json:"severity"`
}

// updateTitleRequest is the request body for IncidentsService.UpdateTitle.
type updateTitleRequest struct {
	IncidentID string `json:"incidentID"`
	Title      string `json:"title"`
}

// Severity represents an organization-defined severity level.
type Severity struct {
	SeverityID   string `json:"severityID"`
	DisplayLabel string `json:"displayLabel"`
	Level        int    `json:"level"`
	Color        string `json:"color,omitempty"`
}

// ActivityItem represents a single entry in an incident's activity timeline.
type ActivityItem struct {
	ActivityItemID string       `json:"activityItemID"`
	IncidentID     string       `json:"incidentID"`
	ActivityKind   string       `json:"activityKind"`
	Body           string       `json:"body"`
	EventTime      string       `json:"eventTime"`
	CreatedTime    string       `json:"createdTime"`
	User           ActivityUser `json:"user"`
}

// ActivityUser represents the user who created an activity item.
type ActivityUser struct {
	UserID string `json:"userID"`
	Name   string `json:"name"`
}

// IncidentContextUser is a user reference returned on an incident context.
type IncidentContextUser struct {
	UserID       string `json:"userID"`
	Name         string `json:"name"`
	Email        string `json:"email,omitempty"`
	GrafanaLogin string `json:"grafanaLogin,omitempty"`
	PhotoURL     string `json:"photoURL,omitempty"`
}

// IncidentContextField is a key/value entry in an incident context's metadata.
type IncidentContextField struct {
	Key         string `json:"key"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Value       string `json:"value"`
	Secret      bool   `json:"secret,omitempty"`
	Checked     bool   `json:"checked,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
}

// IncidentContext is a single context entry attached to an incident — for
// example a linked alert group, dashboard, or other reference surface.
type IncidentContext struct {
	IncidentID    string                 `json:"incidentID"`
	ContextID     string                 `json:"contextID"`
	CreatedByUser IncidentContextUser    `json:"createdByUser"`
	CreatedTime   string                 `json:"createdTime,omitempty"`
	ModifiedTime  string                 `json:"modifiedTime,omitempty"`
	LastRun       string                 `json:"lastRun,omitempty"`
	Title         string                 `json:"title,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Type          string                 `json:"type,omitempty"`
	Payload       string                 `json:"payload,omitempty"`
	Metadata      []IncidentContextField `json:"metadata,omitempty"`
	Status        string                 `json:"status,omitempty"`
	ProcessStatus string                 `json:"processStatus,omitempty"`
	ProcessError  string                 `json:"processError,omitempty"`
	ProcessorInfo string                 `json:"processorInfo,omitempty"`
	AlertGroupID  *string                `json:"alertGroupID,omitempty"`
}

// IncidentContextQuery represents the filters accepted by the
// IncidentContextService.QueryIncidentContext endpoint.
type IncidentContextQuery struct {
	IncidentID     string `json:"incidentID"`
	Limit          int    `json:"limit,omitempty"`
	Status         string `json:"status,omitempty"`
	Type           string `json:"type,omitempty"`
	AlertGroupID   string `json:"alertGroupID,omitempty"`
	OrderField     string `json:"orderField,omitempty"`
	OrderDirection string `json:"orderDirection,omitempty"`
}

// queryIncidentContextRequest is the request body for QueryIncidentContext.
type queryIncidentContextRequest struct {
	Query IncidentContextQuery `json:"query"`
}

// queryIncidentContextResponse wraps the response from QueryIncidentContext.
type queryIncidentContextResponse struct {
	IncidentContexts []IncidentContext `json:"incidentContexts"`
}
