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

// ErrorResponse is the error response body returned by the IRM API.
type ErrorResponse struct {
	Error string `json:"error"`
}

// GetResourceName returns the incident ID.
func (i Incident) GetResourceName() string { return i.IncidentID }

// SetResourceName restores the incident ID.
func (i *Incident) SetResourceName(name string) { i.IncidentID = name }

// Incident represents an incident from the IRM API.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Incident struct {
	IncidentID              string               `json:"incidentID,omitempty"`
	Title                   string               `json:"title"`
	Slug                    string               `json:"slug,omitempty"`
	Prefix                  string               `json:"prefix,omitempty"`
	Status                  string               `json:"status"`
	StatusID                string               `json:"statusID,omitempty"`
	State                   string               `json:"state,omitempty"`
	Severity                string               `json:"severity,omitempty"`
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
	LabelUUID   string `json:"labelUUID,omitempty"`
	ColorHex    string `json:"colorHex,omitempty"`
	Description string `json:"description,omitempty"`
}

// IncidentCursor represents a cursor for paginated query responses.
type IncidentCursor struct {
	HasMore   bool   `json:"hasMore"`
	NextValue string `json:"nextValue"`
}

// QueryIncidentPreviews: severity arrives as severityLabel, and the rest of
// a full Incident's fields are absent — the structured children (taskList,
// incidentMembership, incidentHookRuns, refs) as well as overviewURL,
// durationSeconds, prefix, state, statusID, fieldGroupUUID, descriptionUser,
// descriptionModifiedTime, statusModifiedByUser and statusModifiedTime. The
// opt-in membership preview (includeMembershipPreview) is not requested:
// its important-assignments-only shape does not map onto IncidentMembership.
type IncidentQuery struct {
	Limit          int
	OrderDirection string
	OrderField     string
	DateFrom       *FlexTime
	DateTo         *FlexTime
	IncidentLabels []string
}

// incidentPreviewsQuery is the documented IncidentPreviewsQuery wire type.
type incidentPreviewsQuery struct {
	Limit          int    `json:"limit"`
	OrderDirection string `json:"orderDirection"`
	OrderField     string `json:"orderField,omitempty"`
	QueryString    string `json:"queryString,omitempty"`
}

// queryIncidentPreviewsRequest is the request body for QueryIncidentPreviews.
// The cursor rides next to the query, not inside it: pass the cursor
// returned by the previous page to fetch the next one.
type queryIncidentPreviewsRequest struct {
	Query                    incidentPreviewsQuery `json:"query"`
	Cursor                   *IncidentCursor       `json:"cursor,omitempty"`
	IncludeCustomFieldValues bool                  `json:"includeCustomFieldValues"`
	IncludeIncidentChannels  bool                  `json:"includeIncidentChannels"`
}

// queryIncidentPreviewsResponse is the response from QueryIncidentPreviews.
type queryIncidentPreviewsResponse struct {
	IncidentPreviews []IncidentPreview `json:"incidentPreviews"`
	Cursor           IncidentCursor    `json:"cursor"`
	Error            string            `json:"error,omitempty"`
}

// IncidentPreview is the reduced incident shape returned by
// QueryIncidentPreviews: severity arrives as severityLabel, and the
// structured children of a full Incident (taskList, membership, hook runs,
// refs) are absent.
type IncidentPreview struct {
	IncidentID       string               `json:"incidentID"`
	Title            string               `json:"title"`
	Slug             string               `json:"slug,omitempty"`
	Status           string               `json:"status"`
	SeverityID       string               `json:"severityID,omitempty"`
	SeverityLabel    string               `json:"severityLabel,omitempty"`
	IsDrill          bool                 `json:"isDrill"`
	IncidentType     string               `json:"incidentType,omitempty"`
	Description      string               `json:"description,omitempty"`
	Summary          string               `json:"summary,omitempty"`
	Version          int                  `json:"version,omitempty"`
	Labels           []IncidentLabel      `json:"labels,omitempty"`
	FieldValues      []IncidentFieldValue `json:"fieldValues,omitempty"`
	IncidentChannels []any                `json:"incidentChannels,omitempty"`
	CreatedByUser    *IncidentUser        `json:"createdByUser,omitempty"`
	CreatedTime      FlexTime             `json:"createdTime,omitzero"`
	ModifiedTime     FlexTime             `json:"modifiedTime,omitzero"`
	ClosedTime       FlexTime             `json:"closedTime,omitzero"`
	IncidentStart    FlexTime             `json:"incidentStart,omitzero"`
	IncidentEnd      FlexTime             `json:"incidentEnd,omitzero"`
}

// ToIncident maps the preview onto the Incident shape used across the
// provider; severityLabel populates Severity, matching the field
// QueryIncidents used to return, and fields previews do not carry stay zero.
func (p IncidentPreview) ToIncident() Incident {
	return Incident{
		IncidentID:       p.IncidentID,
		Title:            p.Title,
		Slug:             p.Slug,
		Status:           p.Status,
		Severity:         p.SeverityLabel,
		SeverityID:       p.SeverityID,
		IsDrill:          p.IsDrill,
		IncidentType:     p.IncidentType,
		Description:      p.Description,
		Summary:          p.Summary,
		Version:          p.Version,
		Labels:           p.Labels,
		FieldValues:      p.FieldValues,
		IncidentChannels: p.IncidentChannels,
		CreatedByUser:    p.CreatedByUser,
		CreatedTime:      p.CreatedTime,
		ModifiedTime:     p.ModifiedTime,
		ClosedTime:       p.ClosedTime,
		IncidentStart:    p.IncidentStart,
		IncidentEnd:      p.IncidentEnd,
	}
}

// createIncidentRequest is the request body for creating an incident.
type createIncidentRequest struct {
	Title          string          `json:"title"`
	Status         string          `json:"status"`
	IsDrill        bool            `json:"isDrill"`
	Labels         []IncidentLabel `json:"labels"`
	IncidentType   string          `json:"incidentType,omitempty"`
	FieldGroupUUID string          `json:"fieldGroupUUID,omitempty"`
	SeverityID     string          `json:"severityID,omitempty"`
}

// createIncidentResponse wraps the created incident.
type createIncidentResponse struct {
	Incident Incident `json:"incident"`
}

// updateStatusRequest is the request body for updating incident status.
type updateStatusRequest struct {
	IncidentID string `json:"incidentID"`
	Status     string `json:"status"`
}

// updateStatusResponse wraps the updated incident.
type updateStatusResponse struct {
	Incident Incident `json:"incident"`
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
