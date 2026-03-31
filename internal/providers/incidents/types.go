package incidents

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

// IncidentQuery represents a query for incidents.
type IncidentQuery struct {
	Limit           int       `json:"limit"`
	OrderDirection  string    `json:"orderDirection"`
	OrderField      string    `json:"orderField"`
	QueryString     string    `json:"queryString"`
	ContextPayload  string    `json:"contextPayload,omitempty"`
	DateFrom        *FlexTime `json:"dateFrom,omitempty"`
	DateTo          *FlexTime `json:"dateTo,omitempty"`
	Severity        string    `json:"severity,omitempty"`
	OnlyDrills      bool      `json:"onlyDrills,omitempty"`
	IncludeStatuses []string  `json:"includeStatuses,omitempty"`
	ExcludeStatuses []string  `json:"excludeStatuses,omitempty"`
	IncidentLabels  []string  `json:"incidentLabels,omitempty"`
	IncidentRoles   []string  `json:"incidentRoles,omitempty"`
}

// queryIncidentsRequest is the request body for querying incidents.
type queryIncidentsRequest struct {
	Query IncidentQuery `json:"query"`
}

// queryIncidentsResponse is the response from querying incidents.
type queryIncidentsResponse struct {
	Incidents []Incident     `json:"incidents"`
	Cursor    IncidentCursor `json:"cursor"`
	Query     IncidentQuery  `json:"query"`
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
