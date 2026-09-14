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

// Incident represents an incident from the IRM API.
type Incident struct {
	IncidentID string `json:"incidentID,omitempty"`
	Title      string `json:"title"`
	Slug       string `json:"slug,omitempty"`
	Prefix     string `json:"prefix,omitempty"`
	Status     string `json:"status"`
	StatusID   string `json:"statusID,omitempty"`
	State      string `json:"state,omitempty"`
	// Severity is the display label, for example "Critical". Create ignores
	// it, because the IRM API's CreateIncident does.
	Severity string `json:"severity,omitempty"`
	// SeverityID is the severity identifier of the organization. Create
	// ignores it, because the IRM API's CreateIncident does.
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
	Value       string `json:"value,omitempty"`
	LabelUUID   string `json:"labelUUID,omitempty"`
	ColorHex    string `json:"colorHex,omitempty"`
	Description string `json:"description,omitempty"`
}

// IncidentQuery holds the parameters of a List call. Statuses and Severity
// are compiled into the incident query-string language; IncidentLabels,
// DateFrom and DateTo are matched client-side, because
// QueryIncidentPreviews has no fields for them.
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
	// set it is used verbatim and the structured filters above are ignored.
	QueryString string
}

// incidentCursor represents a cursor for paginated query responses.
type incidentCursor struct {
	HasMore   bool   `json:"hasMore"`
	NextValue string `json:"nextValue"`
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
	Cursor                   *incidentCursor       `json:"cursor,omitempty"`
	IncludeCustomFieldValues bool                  `json:"includeCustomFieldValues"`
	IncludeIncidentChannels  bool                  `json:"includeIncidentChannels"`
}

// queryIncidentPreviewsResponse is the response from QueryIncidentPreviews.
type queryIncidentPreviewsResponse struct {
	IncidentPreviews []incidentPreview `json:"incidentPreviews"`
	Cursor           incidentCursor    `json:"cursor"`
	Error            string            `json:"error,omitempty"`
}

// incidentPreview is the reduced incident shape returned by
// QueryIncidentPreviews: severity arrives as severityLabel, and the
// structured children of a full Incident (taskList, membership, hook runs,
// refs) are absent. The opt-in membership preview
// (includeMembershipPreview) is not requested: its
// important-assignments-only shape does not map onto IncidentMembership.
type incidentPreview struct {
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

// toIncident maps the preview onto the Incident shape List returns;
// severityLabel populates Severity, matching the field QueryIncidents used
// to return, and fields previews do not carry stay zero.
func (p incidentPreview) toIncident() Incident {
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

// createIncidentRequest is the request body for creating an incident. It
// carries no severity field: CreateIncident ignores both severity and
// severityID, and UpdateSeverity is the only route to a severity other than
// the default one.
type createIncidentRequest struct {
	Title          string          `json:"title"`
	Status         string          `json:"status"`
	IsDrill        bool            `json:"isDrill"`
	Labels         []IncidentLabel `json:"labels"`
	IncidentType   string          `json:"incidentType,omitempty"`
	FieldGroupUUID string          `json:"fieldGroupUUID,omitempty"`
}

// createIncidentResponse wraps the created incident.
type createIncidentResponse struct {
	Incident Incident `json:"incident"`
}
