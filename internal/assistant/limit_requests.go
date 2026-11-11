package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/gcxerrors"
	"golang.org/x/sync/errgroup"
)

// Limit request endpoints, served behind the assistant.limits-requests flag.
// A stack with the flag off answers 404.
const (
	limitRequestsPath      = "/api/v1/limit-requests"
	limitRequestGroupsPath = "/api/v1/limit-request-groups"
	seatLimitPath          = "/api/v1/usage/limits/monthly-unique-users"
	seatCountPath          = "/api/v1/usage/monthly-active-users"
	tokenLimitsPath        = "/api/v1/usage/tokens/limits" //nolint:gosec // a URL path, not a credential
	limitSettingsPath      = "/api/v1/usage/limit-requests/settings"
)

// Limits a blocked person can ask an Assistant Admin to raise. Prompt and
// investigation limits are not requestable.
const (
	LimitActiveUsers = "active_users"
	LimitStackTokens = "stack_tokens"
	LimitUserTokens  = "user_tokens"
)

// RequestableLimits lists the valid limit keys for a limit request.
var RequestableLimits = []string{LimitActiveUsers, LimitStackTokens, LimitUserTokens} //nolint:gochecknoglobals // fixed enum for argument validation and completion

// LimitLabel names a limit key the way people talk about it.
func LimitLabel(key string) string {
	switch key {
	case LimitActiveUsers:
		return "seats"
	case LimitStackTokens:
		return "stack tokens"
	case LimitUserTokens:
		return "tokens per user"
	}
	return key
}

// LimitRequest is one person's request to raise one limit in one month.
// Requester is set only for admins.
type LimitRequest struct {
	ID             string     `json:"id"`
	LimitKey       string     `json:"limitKey"`
	GroupID        string     `json:"groupId"`
	State          string     `json:"state"`
	Resolution     string     `json:"resolution,omitempty"`
	Note           string     `json:"note"`
	Origin         string     `json:"origin"`
	Period         string     `json:"period"`
	UsedAtRequest  int64      `json:"usedAtRequest"`
	LimitAtRequest int64      `json:"limitAtRequest"`
	CreatedAt      time.Time  `json:"createdAt"`
	ResolvedAt     *time.Time `json:"resolvedAt"`
	Requester      string     `json:"requester,omitempty"`
}

// CreatedLimitRequest is the result of asking admins to raise a limit.
// Created is false when the same request was already pending.
type CreatedLimitRequest struct {
	Request LimitRequest `json:"request"`
	Created bool         `json:"created"`
}

// CreateLimitRequest asks the stack's Assistant Admins to raise limit. The
// server accepts it only while the caller is blocked by a limit an admin can raise.
func CreateLimitRequest(ctx context.Context, client *assistanthttp.Client, limit, note string) (*CreatedLimitRequest, error) {
	body := struct {
		LimitKey string `json:"limitKey"`
		Note     string `json:"note,omitempty"`
	}{limit, note}
	created, err := assistanthttp.DoEnvelopeRequest[CreatedLimitRequest](client, ctx, http.MethodPost, limitRequestsPath, body, "create limit request")
	if forbidden, ok := errors.AsType[*assistanthttp.ForbiddenError](err); ok {
		// The route needs no permission: the server refuses callers it can't
		// verify as a person, such as service account tokens.
		return nil, gcxerrors.DetailedError{
			Summary:     "Only the blocked person can ask to raise a limit",
			Details:     cmp.Or(serverMessage(forbidden.HTTPStatusError), forbidden.Error()),
			Parent:      err,
			Suggestions: []string{"Use a gcx context signed in as yourself rather than a service account token: gcx login"},
		}
	}
	if err != nil {
		return nil, limitRequestError(err)
	}
	if created.Request.ID == "" {
		return nil, errors.New("limit request response is missing the request")
	}
	return created, nil
}

// MyLimitRequests are the caller's pending requests. RequestsEnabled reports
// whether the stack's admins accept requests at all.
type MyLimitRequests struct {
	Requests        []LimitRequest `json:"requests"`
	RequestsEnabled bool           `json:"requestsEnabled"`
}

// Pending returns the caller's pending request for limit, or nil.
func (m *MyLimitRequests) Pending(limit string) *LimitRequest {
	if m == nil {
		return nil
	}
	for i := range m.Requests {
		if m.Requests[i].LimitKey == limit {
			return &m.Requests[i]
		}
	}
	return nil
}

// maxPages stops a server that keeps returning a cursor; a stack has a
// handful of groups, and a person a handful of requests, per month.
const maxPages = 50

// ListMyLimitRequests returns the caller's requests, newest first. An empty
// state returns requests in every state.
func ListMyLimitRequests(ctx context.Context, client *assistanthttp.Client, state string) (*MyLimitRequests, error) {
	mine := &MyLimitRequests{Requests: make([]LimitRequest, 0)}
	query := url.Values{"pageSize": {"100"}}
	if state != "" {
		query.Set("state", state)
	}
	for range maxPages {
		page, err := assistanthttp.DoEnvelopeRequest[*struct {
			Requests        *[]LimitRequest `json:"requests"`
			NextCursor      string          `json:"nextCursor"`
			RequestsEnabled bool            `json:"requestsEnabled"`
		}](client, ctx, http.MethodGet, limitRequestsPath+"/mine?"+query.Encode(), nil, "list your limit requests")
		if err != nil {
			return nil, limitRequestError(err)
		}
		if *page == nil || (*page).Requests == nil {
			return nil, errors.New("limit requests response is missing requests")
		}
		mine.Requests = append(mine.Requests, *(*page).Requests...)
		mine.RequestsEnabled = (*page).RequestsEnabled
		if (*page).NextCursor == "" {
			return mine, nil
		}
		query.Set("cursor", (*page).NextCursor)
	}
	return nil, errors.New("limit requests did not finish paging")
}

// fetchMyPendingLimitRequests returns nil, without an error, when the stack
// does not serve limit requests.
func fetchMyPendingLimitRequests(ctx context.Context, client *assistanthttp.Client) (*MyLimitRequests, error) {
	mine, err := ListMyLimitRequests(ctx, client, "pending")
	if isNotFound(err) {
		return nil, nil //nolint:nilnil // a missing endpoint means limit requests are off for this stack
	}
	return mine, err
}

// LimitRequestGroup is every pending request that one limit change resolves.
// TeamName is resolved by gcx from the team UID when known.
type LimitRequestGroup struct {
	ID               string     `json:"id"`
	LimitKey         string     `json:"limitKey"`
	GroupKey         string     `json:"groupKey"`
	Scope            GroupScope `json:"scope"`
	PendingCount     int        `json:"pendingCount"`
	FirstRequestedAt *time.Time `json:"firstRequestedAt"`
	Period           string     `json:"period"`
	TeamName         string     `json:"teamName,omitempty"`
}

// GroupScope is the setting an admin would change: the stack's own limit, one
// team's per-user grant, or the default per-user limit.
type GroupScope struct {
	Kind    string `json:"kind"`
	TeamUID string `json:"teamUid,omitempty"`
}

// ScopeLabel renders a group's scope for people.
func (g LimitRequestGroup) ScopeLabel() string {
	switch g.Scope.Kind {
	case "team":
		return "team: " + cmp.Or(g.TeamName, g.Scope.TeamUID)
	case "default_per_user":
		return "default per user"
	}
	return g.Scope.Kind
}

// ListLimitRequestGroups returns this month's groups with pending requests,
// most requests first. Empty filters match every group.
func ListLimitRequestGroups(ctx context.Context, client *assistanthttp.Client, limit, teamUID string) ([]LimitRequestGroup, error) {
	groups := make([]LimitRequestGroup, 0)
	query := url.Values{"pageSize": {"100"}}
	if limit != "" {
		query.Set("limitKey", limit)
	}
	if teamUID != "" {
		query.Set("teamUid", teamUID)
	}
	for range maxPages {
		page, err := assistanthttp.DoEnvelopeRequest[*struct {
			Groups     *[]LimitRequestGroup `json:"groups"`
			NextCursor string               `json:"nextCursor"`
		}](client, ctx, http.MethodGet, limitRequestGroupsPath+"?"+query.Encode(), nil, "list limit request groups")
		if err != nil {
			return nil, adminLimitRequestError(err, "grafana-assistant-app.usage:read")
		}
		if *page == nil || (*page).Groups == nil {
			return nil, errors.New("limit request groups response is missing groups")
		}
		groups = append(groups, *(*page).Groups...)
		if (*page).NextCursor == "" {
			return groups, nil
		}
		query.Set("cursor", (*page).NextCursor)
	}
	return nil, errors.New("limit request groups did not finish paging")
}

// ListGroupRequests returns every pending request in a group, oldest first.
func ListGroupRequests(ctx context.Context, client *assistanthttp.Client, groupID string) ([]LimitRequest, error) {
	requests := make([]LimitRequest, 0)
	for page := 1; ; page++ {
		data, err := assistanthttp.DoEnvelopeRequest[*struct {
			Requests   *[]LimitRequest `json:"requests"`
			TotalCount int             `json:"totalCount"`
		}](client, ctx, http.MethodGet, limitRequestGroupsPath+"/"+url.PathEscape(groupID)+"/requests?pageSize=100&page="+strconv.Itoa(page), nil, "list limit requests")
		if err != nil {
			return nil, adminLimitRequestError(err, "grafana-assistant-app.usage:read")
		}
		if *data == nil || (*data).Requests == nil {
			return nil, errors.New("limit requests response is missing requests")
		}
		requests = append(requests, *(*data).Requests...)
		if len(*(*data).Requests) == 0 || len(requests) >= (*data).TotalCount {
			break
		}
	}
	slices.SortStableFunc(requests, func(a, b LimitRequest) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return requests, nil
}

// UsageTeam is a team from the admin usage snapshot; gcx uses it to name the
// teams that limit request groups identify only by UID.
type UsageTeam struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// NameTeams fills each team group's TeamName from teams.
func NameTeams(groups []LimitRequestGroup, teams []UsageTeam) {
	for i := range groups {
		if j := slices.IndexFunc(teams, func(t UsageTeam) bool { return t.UID == groups[i].Scope.TeamUID }); j >= 0 && groups[i].Scope.Kind == "team" {
			groups[i].TeamName = teams[j].Name
		}
	}
}

// SeatUsage is the stack's monthly Assistant seats: each person who uses
// Assistant in a month takes one. A zero limit means no cap.
type SeatUsage struct {
	Active int64  `json:"active"`
	Limit  int64  `json:"limit"`
	Month  string `json:"month"`
}

// fetchSeatUsage returns nil, without an error, when the caller may not read
// the seat limit, which needs usage:write, or the stack does not serve it.
func fetchSeatUsage(ctx context.Context, client *assistanthttp.Client) (*SeatUsage, error) {
	var limit *struct {
		Limit int64 `json:"limit"`
	}
	var count *struct {
		Count int64  `json:"count"`
		Month string `json:"month"`
	}
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		data, err := assistanthttp.DoEnvelopeRequest[*struct {
			Limit int64 `json:"limit"`
		}](client, ctx, http.MethodGet, seatLimitPath, nil, "get seat limit")
		if err == nil {
			limit = *data
		}
		return err
	})
	group.Go(func() error {
		data, err := assistanthttp.DoEnvelopeRequest[*struct {
			Count int64  `json:"count"`
			Month string `json:"month"`
		}](client, ctx, http.MethodGet, seatCountPath, nil, "get active users")
		if err == nil {
			count = *data
		}
		return err
	})
	if err := group.Wait(); err != nil {
		if _, denied := errors.AsType[*assistanthttp.ForbiddenError](err); denied || isNotFound(err) {
			return nil, nil //nolint:nilnil // readers without usage:write, and stacks without seat limits, show no seats
		}
		return nil, err
	}
	if limit == nil || count == nil {
		return nil, errors.New("seat usage response is missing data")
	}
	if limit.Limit < 0 || count.Count < 0 {
		return nil, errors.New("seat usage response contains a negative count or limit")
	}
	return &SeatUsage{Active: count.Count, Limit: limit.Limit, Month: count.Month}, nil
}

// fetchPendingLimitRequestGroups returns nil, without an error, when the stack
// does not serve limit requests or the caller may not list them.
func fetchPendingLimitRequestGroups(ctx context.Context, client *assistanthttp.Client, teams []UsageTeam) ([]LimitRequestGroup, error) {
	groups, err := ListLimitRequestGroups(ctx, client, "", "")
	if err != nil {
		if _, denied := errors.AsType[*assistanthttp.ForbiddenError](err); denied || isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	NameTeams(groups, teams)
	return groups, nil
}

// PreviewRaise reports how many of a group's pending requests a new limit
// would resolve. value is in the group's unit: seats or tokens.
func PreviewRaise(ctx context.Context, client *assistanthttp.Client, groupID string, value int64) (int, error) {
	data, err := assistanthttp.DoEnvelopeRequest[struct {
		Resolves int `json:"resolves"`
	}](client, ctx, http.MethodGet, limitRequestGroupsPath+"/"+url.PathEscape(groupID)+"/raise-preview?value="+strconv.FormatInt(value, 10), nil, "preview limit raise")
	if err != nil {
		return 0, adminLimitRequestError(err, "grafana-assistant-app.usage:read")
	}
	return data.Resolves, nil
}

// RaiseLimit sets the limit a group asks about to value, in the group's unit,
// and returns the value it replaces; nil means the stack uses its default.
// With apply false it only reads and checks. After a write the server
// resolves the pending requests the new value unblocks.
func RaiseLimit(ctx context.Context, client *assistanthttp.Client, group LimitRequestGroup, value int64, apply bool) (*int64, error) {
	previous, err := raiseLimit(ctx, client, group, value, apply)
	if err != nil {
		return nil, adminLimitRequestError(err, "grafana-assistant-app.usage:write")
	}
	return previous, nil
}

func raiseLimit(ctx context.Context, client *assistanthttp.Client, group LimitRequestGroup, value int64, apply bool) (*int64, error) {
	put := func(path string, body any, op string) error {
		if !apply {
			return nil
		}
		_, err := assistanthttp.DoEnvelopeRequest[json.RawMessage](client, ctx, http.MethodPut, path, body, op)
		return err
	}
	if group.LimitKey == LimitActiveUsers {
		current, err := assistanthttp.DoEnvelopeRequest[struct {
			Limit int64 `json:"limit"`
		}](client, ctx, http.MethodGet, seatLimitPath, nil, "get seat limit")
		if err != nil {
			return nil, err
		}
		if err := checkRaise(current.Limit, value); err != nil {
			return nil, err
		}
		return &current.Limit, put(seatLimitPath, map[string]int64{"limit": value}, "set seat limit")
	}
	if value%tokensPerMillion != 0 {
		return nil, fmt.Errorf("token limits are whole millions of tokens, and %s is not", strconv.FormatInt(value, 10))
	}
	mtok := value / tokensPerMillion
	// PUT replaces every stack override and clears omitted ones, so send back
	// the overrides this raise does not change.
	type overrides struct {
		StackTotalMtok         *int64 `json:"stackTotalMtok,omitempty"`
		PerUserDefaultMtok     *int64 `json:"perUserDefaultMtok,omitempty"`
		ServiceAccountPoolMtok *int64 `json:"serviceAccountPoolMtok,omitempty"`
	}
	config, err := assistanthttp.DoEnvelopeRequest[struct {
		overrides

		Teams []struct {
			TeamUID     string `json:"teamUid"`
			PerUserMtok int64  `json:"perUserMtok"`
		} `json:"teams"`
	}](client, ctx, http.MethodGet, tokenLimitsPath, nil, "get token limits")
	if err != nil {
		return nil, err
	}
	var current *int64
	switch {
	case group.LimitKey == LimitStackTokens:
		current = config.StackTotalMtok
		config.StackTotalMtok = &mtok
	case group.Scope.Kind == "team":
		for _, team := range config.Teams {
			if team.TeamUID == group.Scope.TeamUID {
				current = &team.PerUserMtok
			}
		}
	default:
		current = config.PerUserDefaultMtok
		config.PerUserDefaultMtok = &mtok
	}
	var previous *int64
	if current != nil {
		previous = new(*current * tokensPerMillion)
		if err := checkRaise(*previous, value); err != nil {
			return nil, err
		}
	}
	if group.Scope.Kind == "team" {
		return previous, put(tokenLimitsPath+"/teams/"+url.PathEscape(group.Scope.TeamUID), map[string]int64{"perUserMtok": mtok}, "set team token limit")
	}
	return previous, put(tokenLimitsPath, config.overrides, "set token limits")
}

// checkRaise refuses a value that would not raise the current limit, where
// zero means no limit.
func checkRaise(current, value int64) error {
	if current == 0 {
		return errors.New("this limit is already unlimited")
	}
	if value <= current {
		return fmt.Errorf("the limit is already %s; raise it above that", compactUsageNumber(current))
	}
	return nil
}

// DismissLimitRequest ends one pending request without changing a limit; the
// requester is notified.
func DismissLimitRequest(ctx context.Context, client *assistanthttp.Client, requestID string) (*LimitRequest, error) {
	data, err := assistanthttp.DoEnvelopeRequest[struct {
		Request LimitRequest `json:"request"`
	}](client, ctx, http.MethodPost, limitRequestsPath+"/"+url.PathEscape(requestID)+"/dismiss", nil, "dismiss limit request")
	if err != nil {
		return nil, adminLimitRequestError(err, "grafana-assistant-app.usage:write")
	}
	return &data.Request, nil
}

// DismissLimitRequestGroup ends every pending request in a group and returns
// how many it ended.
func DismissLimitRequestGroup(ctx context.Context, client *assistanthttp.Client, groupID string) (int, error) {
	data, err := assistanthttp.DoEnvelopeRequest[struct {
		Dismissed int `json:"dismissed"`
	}](client, ctx, http.MethodPost, limitRequestGroupsPath+"/"+url.PathEscape(groupID)+"/dismiss", nil, "dismiss limit requests")
	if err != nil {
		return 0, adminLimitRequestError(err, "grafana-assistant-app.usage:write")
	}
	return data.Dismissed, nil
}

// LimitRequestSettings is whether a stack accepts limit requests, how admins
// hear about them, and when people are warned that a limit is close.
type LimitRequestSettings struct {
	Enabled       bool `json:"enabled"`
	AdminChannels struct {
		Inbox       bool `json:"inbox"`
		EmailDigest bool `json:"emailDigest"`
	} `json:"adminChannels"`
	Alert struct {
		Enabled          bool `json:"enabled"`
		ThresholdPercent int  `json:"thresholdPercent"`
		Email            bool `json:"email"`
	} `json:"alert"`
}

// GetLimitRequestSettings reads the stack's limit request settings.
func GetLimitRequestSettings(ctx context.Context, client *assistanthttp.Client) (*LimitRequestSettings, error) {
	data, err := assistanthttp.DoEnvelopeRequest[struct {
		Settings LimitRequestSettings `json:"settings"`
	}](client, ctx, http.MethodGet, limitSettingsPath, nil, "get limit request settings")
	if err != nil {
		return nil, adminLimitRequestError(err, "grafana-assistant-app.usage:read")
	}
	return &data.Settings, nil
}

// UpdateLimitRequestSettings replaces the stack's limit request settings.
func UpdateLimitRequestSettings(ctx context.Context, client *assistanthttp.Client, settings LimitRequestSettings) (*LimitRequestSettings, error) {
	data, err := assistanthttp.DoEnvelopeRequest[struct {
		Settings LimitRequestSettings `json:"settings"`
	}](client, ctx, http.MethodPut, limitSettingsPath, settings, "update limit request settings")
	if err != nil {
		return nil, adminLimitRequestError(err, "grafana-assistant-app.usage:write")
	}
	return &data.Settings, nil
}

// limitRequestError turns the server's refusals into their own message: it
// explains why (not blocked, not raisable by admins, requests turned off).
func limitRequestError(err error) error {
	if isNotFound(err) {
		return fmt.Errorf("this stack's Grafana Assistant does not accept limit requests: %w", err)
	}
	if statusErr, ok := errors.AsType[*gcxerrors.HTTPStatusError](err); ok && statusErr.Status == http.StatusConflict {
		if message := serverMessage(statusErr); message != "" {
			return errors.New(message)
		}
	}
	return err
}

func serverMessage(err *gcxerrors.HTTPStatusError) string {
	var body struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal([]byte(err.ServerMessage), &body)
	return body.Message
}

func adminLimitRequestError(err error, permission string) error {
	if forbidden, ok := errors.AsType[*assistanthttp.ForbiddenError](err); ok && forbidden.Permission == "" {
		forbidden.Permission = permission
		return forbidden
	}
	return limitRequestError(err)
}

func isNotFound(err error) bool {
	statusErr, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
	return ok && statusErr.Status == http.StatusNotFound
}
