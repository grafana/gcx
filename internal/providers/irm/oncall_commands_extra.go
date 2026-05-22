package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// alertGroupsListAlertsCap is the default ceiling on per-call alert retrieval.
// Override with `--limit` (0 = no limit).
const alertGroupsListAlertsCap = 100

// alertGroupListDefaultLimit is the default `--limit` for `alert-groups list`.
// Mirrors the synth/slo precedent of 50; bypass with `--limit 0`.
const alertGroupListDefaultLimit = 50

// alertGroupsListAlertsConcurrency bounds the N+1 retrieve fan-out.
const alertGroupsListAlertsConcurrency = 10

// ---------------------------------------------------------------------------
// alert-groups command: list, get, actions, list-alerts
// ---------------------------------------------------------------------------

type alertGroupListOpts struct {
	listOpts

	MaxAge string

	// Limit caps the number of alert groups returned. Default
	// alertGroupListDefaultLimit; pass 0 to disable (subject to client-side
	// hardCap to avoid runaway memory).
	Limit int

	// Filter flags. See ADR 001 § 1 (alert-groups list defaults).
	States             []string
	Teams              []string
	Integrations       []string
	Mine               bool
	WithResolutionNote bool
	HasRelatedIncident bool
	All                bool
	IncludeChildGroups bool
}

func (o *alertGroupListOpts) setup(flags *pflag.FlagSet) {
	o.listOpts.setup(flags, "alert-groups")
	// Override the default JSON→sigsyaml YAML codec with the go-yaml encoder so
	// the typed envelope's deliberate field order under status (title, summary,
	// severity, state, ...) is preserved instead of alphabetized.
	o.IO.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	flags.StringVar(&o.MaxAge, "max-age", "", "Exclude groups older than this duration (e.g. 1h, 24h, 7d)")
	flags.IntVar(&o.Limit, "limit", alertGroupListDefaultLimit, "Maximum number of alert groups to return (0 for all, capped by an internal safety limit)")
	flags.StringSliceVar(&o.States, "state", nil, "Filter by state (firing|acknowledged|resolved|silenced; repeatable, comma-separated). Default: firing,acknowledged,silenced")
	flags.StringSliceVar(&o.Teams, "team", nil, "Filter by team PK (repeatable, comma-separated)")
	flags.StringSliceVar(&o.Integrations, "integration", nil, "Filter by integration PK (repeatable, comma-separated)")
	flags.BoolVar(&o.Mine, "mine", false, "Limit to alert groups for the authenticated user")
	flags.BoolVar(&o.WithResolutionNote, "with-resolution-note", false, "Limit to alert groups that have a resolution note")
	flags.BoolVar(&o.HasRelatedIncident, "has-related-incident", false, "Limit to alert groups linked to an incident")
	flags.BoolVar(&o.All, "all", false, "Bypass the default status and is_root filters (returns resolved groups and child groups too)")
	flags.BoolVar(&o.IncludeChildGroups, "include-child-groups", false, "Include child groups (drops the is_root filter while keeping the status default)")
}

// Validate is a stub to satisfy the CONSTITUTION-required
// opts/setup/Validate/constructor quartet. The flag set has no cross-field
// invariants beyond what opts.IO.Validate() already enforces.
func (o *alertGroupListOpts) Validate() error { return nil }

// stateNameToInt translates a user-facing state name into the OnCall internal
// API integer wire encoding. Accepted: firing|new, acknowledged|ack,
// resolved, silenced.
func stateNameToInt(name string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "firing", "new":
		return 0, true
	case "acknowledged", "ack":
		return 1, true
	case "resolved":
		return 2, true
	case "silenced":
		return 3, true
	}
	return 0, false
}

func newAlertGroupsCommand(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alert-groups",
		Short:   "Manage alert groups.",
		Aliases: []string{"alert-group", "ag"},
	}

	cmd.AddCommand(
		newAlertGroupListCommand(loader),
		newAlertGroupListAlertsCommand(loader),
		newAlertGroupGetRichCommand(loader),
		// Action verbs: every mutating verb emits a
		// MutationResult envelope on stdout, supports bulk-by-filter with the
		// same filter flags as `alert-groups list`, requires --force in agent
		// mode when count > 1, and emits DetailedError on the error path.
		newAcknowledgeCommand(loader),
		newResolveCommand(loader),
		newUnacknowledgeCommand(loader),
		newUnresolveCommand(loader),
		newSilenceCommand(loader),
		newUnsilenceCommand(loader),
		newDeleteCommand(loader),
	)

	return cmd
}

// alertGroupListFilters is the resolved set of filters applied to the
// alertgroups list endpoint. Built from alertGroupListOpts after default
// resolution; passed through both the OAuth-proxy path (listAlertGroupsRaw)
// and the SA-token legacy path (listAlertGroupsLegacy).
type alertGroupListFilters struct {
	MaxAge             string
	Statuses           []int
	IsRoot             *bool
	Teams              []string
	Integrations       []string
	Mine               bool
	WithResolutionNote bool
	HasRelatedIncident bool
}

// resolveAlertGroupListFilters validates and normalizes the user-facing flag
// set into wire-ready filters, applying ADR 001 § 1 defaults:
//   - status defaults to firing+acknowledged+silenced (excluding resolved),
//   - is_root=true is always applied (excluding child groups merged into parents),
//   - --all bypasses both defaults,
//   - --include-child-groups drops is_root but keeps the status default,
//   - explicit --state always wins (still subject to --include-child-groups for is_root).
func resolveAlertGroupListFilters(cmd *cobra.Command, opts *alertGroupListOpts) (alertGroupListFilters, error) {
	out := alertGroupListFilters{
		MaxAge:             opts.MaxAge,
		Teams:              opts.Teams,
		Integrations:       opts.Integrations,
		Mine:               opts.Mine,
		WithResolutionNote: opts.WithResolutionNote,
		HasRelatedIncident: opts.HasRelatedIncident,
	}

	// Translate user-facing state names into the internal wire encoding.
	stateExplicit := cmd.Flags().Changed("state")
	if stateExplicit {
		for _, name := range opts.States {
			s := strings.TrimSpace(name)
			if s == "" {
				continue
			}
			n, ok := stateNameToInt(s)
			if !ok {
				return out, fmt.Errorf("invalid --state value %q: must be one of firing, acknowledged, resolved, silenced", name)
			}
			out.Statuses = append(out.Statuses, n)
		}
	}

	if !opts.All {
		// Default status filter: firing, acknowledged, silenced.
		if !stateExplicit {
			out.Statuses = []int{0, 1, 3}
		}
		// Default is_root=true unless the user opted into child groups.
		if !opts.IncludeChildGroups {
			t := true
			out.IsRoot = &t
		}
	}

	return out, nil
}

const alertGroupListLong = `List alert groups.

By default, lists root alert groups (excluding child groups merged into parents) in
firing, acknowledged, or silenced state. Resolved groups are excluded.

Use --all to bypass these defaults entirely (returns resolved and child groups too).
Use --state to override the status filter (e.g. --state firing,acknowledged).
Use --include-child-groups to keep the status default but include child groups.`

func newAlertGroupListCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List alert groups.",
		Long:  alertGroupListLong,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			filters, err := resolveAlertGroupListFilters(cmd, opts)
			if err != nil {
				return err
			}

			client, namespace, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			// Rich shape via the OnCall internal API. *OnCallClient always
			// implements RichAlertGroupReader; the type-assert keeps the
			// fallback in place for tests or alternate OnCallAPI
			// implementations that don't speak the internal API.
			reader, ok := client.(RichAlertGroupReader)
			if !ok {
				return listAlertGroupsLegacy(cmd, opts, filters, client, namespace)
			}

			rawItems, serverHasMore, err := reader.ListAlertGroupsRaw(cmd.Context(), filters, opts.Limit)
			if err != nil {
				return err
			}

			teams, _ := reader.ResolveTeams(cmd.Context()) // best-effort

			envs := make([]alertGroupEnvelope, 0, len(rawItems))
			for _, item := range rawItems {
				api, rich, err := listAlertGroupRichFromBytes(item, teams)
				if err != nil {
					return err
				}
				env, err := alertGroupRichToEnvelope(rich, decodeOnCallLabels(api), namespace)
				if err != nil {
					return err
				}
				envs = append(envs, env)
			}

			// List envelope MUST be `{"items": [...]}` (never bare
			// array, never null). Empty result is `{"items": []}`.
			if err := opts.IO.Encode(cmd.OutOrStdout(), alertGroupItemsEnvelope{Items: envs}); err != nil {
				return err
			}

			stderr := cmd.ErrOrStderr()

			// Strict order: warn → note → hint. The limit warn (if any)
			// gets emitted by the upstream branch on the alerts retrieve path;
			// the list path emits only a note (on empty default-filtered
			// result) and hints (filter summary + drill-in / limit suggestion).
			//
			// Empty-result note: when the default
			// filter is in effect (no --all, no --include-child-groups, no
			// explicit --state) AND the result set is empty, surface a note
			// explaining the implicit exclusions so the user knows there may
			// be resolved or child groups they're not seeing.
			if len(envs) == 0 && !opts.All && !opts.IncludeChildGroups && len(opts.States) == 0 {
				emitNote(stderr, "default filter excludes resolved and child groups; pass --all or --include-child-groups to broaden")
			}

			// Hint emission (locked shape, D2 round 14): only when the user
			// accepted truncation (--limit > 0), the result hit the limit
			// exactly, AND the server confirmed more pages exist. Otherwise
			// silent.
			if opts.Limit > 0 && len(envs) == opts.Limit && serverHasMore {
				emitAlertGroupListLimitHint(stderr, opts.Limit)
			}

			// Filter-summary hint (D2 round 17): silent only when --all is
			// passed AND no other filter flag is in effect (the "show me
			// everything, raw" case). De Morgan: !(A && !B) == !A || B.
			if !opts.All || alertGroupListHasExplicitFilter(opts) {
				emitAlertGroupListFilterHint(stderr, stringifyAlertGroupListFilters(opts))
			}

			// Drill-in navigation hints (D2 round 17): always emitted when
			// the result set is non-empty. Use a literal `<id>` placeholder
			// so the agent-mode template stays generic.
			if len(envs) > 0 {
				emitAlertGroupListNavHints(stderr)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// emitAlertGroupListLimitHint surfaces the D2-round-14 truncation hint on
// stderr when alert-groups list returns exactly `limit` rows AND the server
// reported a non-empty `next` cursor. Format mirrors the locked shape and
// surfaces a runnable command (a doubled-limit fetch
// is a sensible next step that avoids committing to --limit 0):
//
//	TTY:    "hint: showing first N results: gcx irm oncall alert-groups list --limit M"
//	agent:  {"class":"hint","summary":"showing first N results","command":"gcx irm oncall alert-groups list --limit M"}
func emitAlertGroupListLimitHint(stderr io.Writer, limit int) {
	suggested := limit * 2
	emitHint(stderr,
		fmt.Sprintf("showing first %d results", limit),
		fmt.Sprintf("gcx irm oncall alert-groups list --limit %d", suggested))
}

// alertGroupListFilterMaxLen caps the rendered filter summary at a generous
// budget so the TTY hint stays readable on a typical terminal width.
const alertGroupListFilterMaxLen = 80

// stringifyAlertGroupListFilters renders the user's effective filter set for
// the alert-groups list filter-summary hint (D2 round 17). Returns a compact,
// comma-joined description suitable for embedding in a single hint line.
//
// When only the implicit defaults are in effect (no flag set, --all not
// passed), returns "default (excludes resolved + child groups)". Explicit
// filters are listed in a stable order: state, team, integration, max-age,
// mine, with-resolution-note, has-related-incident, include-child-groups, all.
// If the rendered summary exceeds alertGroupListFilterMaxLen, falls back to a
// short "<N filters>" summary.
func stringifyAlertGroupListFilters(opts *alertGroupListOpts) string {
	parts := make([]string, 0, 8)
	if len(opts.States) > 0 {
		parts = append(parts, "status="+strings.Join(opts.States, ","))
	}
	if len(opts.Teams) > 0 {
		parts = append(parts, "team="+strings.Join(opts.Teams, ","))
	}
	if len(opts.Integrations) > 0 {
		parts = append(parts, "integration="+strings.Join(opts.Integrations, ","))
	}
	if opts.MaxAge != "" {
		parts = append(parts, "max-age="+opts.MaxAge)
	}
	if opts.Mine {
		parts = append(parts, "mine")
	}
	if opts.WithResolutionNote {
		parts = append(parts, "with-resolution-note")
	}
	if opts.HasRelatedIncident {
		parts = append(parts, "has-related-incident")
	}
	if opts.IncludeChildGroups {
		parts = append(parts, "include-child-groups")
	}
	if opts.All {
		parts = append(parts, "all")
	}

	if len(parts) == 0 {
		// Implicit-defaults state — no flags set, --all not passed.
		return "default (excludes resolved + child groups)"
	}
	out := strings.Join(parts, ", ")
	if len(out) > alertGroupListFilterMaxLen {
		return fmt.Sprintf("%d filters", len(parts))
	}
	// When only defaults are augmented (no --all, no --include-child-groups,
	// no explicit --state), prefix with "default" to make the implicit
	// exclusions visible alongside the active filters.
	if !opts.All && !opts.IncludeChildGroups && len(opts.States) == 0 {
		out = "default + " + out
	}
	return out
}

// alertGroupListHasExplicitFilter reports whether any filter flag besides
// --all is in effect. Used to decide whether the filter-summary hint stays
// silent in the "user passed --all and nothing else" case.
func alertGroupListHasExplicitFilter(opts *alertGroupListOpts) bool {
	return opts.MaxAge != "" ||
		len(opts.States) > 0 ||
		len(opts.Teams) > 0 ||
		len(opts.Integrations) > 0 ||
		opts.Mine ||
		opts.WithResolutionNote ||
		opts.HasRelatedIncident ||
		opts.IncludeChildGroups
}

// emitAlertGroupListFilterHint emits the filter-summary hint for
// `alert-groups list`. Locked shape (each `hint:`
// line MUST contain a runnable gcx command):
//
//	TTY:    "hint: listing alert groups with filter <stringified>: gcx irm oncall alert-groups list --all"
//	agent:  {"class":"hint","summary":"listing alert groups with filter <stringified>","command":"gcx irm oncall alert-groups list --all"}
//
// Both TTY and agent-mode emissions go through the centralised emitHint
// helper so the channel split is uniform across every
// alert-groups command.
func emitAlertGroupListFilterHint(stderr io.Writer, filterSummary string) {
	emitHint(stderr,
		"listing alert groups with filter "+filterSummary,
		"gcx irm oncall alert-groups list --all")
}

// emitAlertGroupListNavHints emits the two drill-in navigation hints fired
// after `alert-groups list` returns a non-empty result set. Both use the
// literal `<id>` placeholder string so the agent-mode events stay
// template-shaped (the user picks the row).
func emitAlertGroupListNavHints(stderr io.Writer) {
	emitHint(stderr, "drill into a group", "gcx irm oncall alert-groups get <id>")
	emitHint(stderr, "see alerts within a group", "gcx irm oncall alert-groups list-alerts <id>")
}

// emitAlertGroupGetNavHints emits the three post-result drilldown hints
// fired after a successful `alert-groups get <id>`:
//
//   - `gcx alert instances list --rule <uid>`     — pivot to the alert rule's live instances
//   - `gcx resources get dashboards/<uid>`        — pivot to the linked dashboard
//   - `gcx irm oncall alert-groups list-alerts <id>` — pivot to per-alert detail
//
// Each hint is conditional on the corresponding identifier being populated:
// rule.uid drives the first hint, dashboard.uid drives the second; the
// list-alerts hint is unconditional because it always has a valid <id>.
// `omitempty` semantics on the status.links block mean a missing
// rule or dashboard simply suppresses its hint.
func emitAlertGroupGetNavHints(stderr io.Writer, env alertGroupEnvelope, id string) {
	if env.Status.Links != nil && env.Status.Links.Alert != nil && env.Status.Links.Alert.Rule != nil && env.Status.Links.Alert.Rule.UID != "" {
		emitHint(stderr, "inspect live instances of the alert rule",
			"gcx alert instances list --rule "+safeUID(env.Status.Links.Alert.Rule.UID))
	}
	if env.Status.Links != nil && env.Status.Links.Dashboard != nil && env.Status.Links.Dashboard.UID != "" {
		emitHint(stderr, "open the linked dashboard",
			"gcx resources get dashboards/"+safeUID(env.Status.Links.Dashboard.UID))
	}
	emitHint(stderr, "see individual alerts in the group",
		"gcx irm oncall alert-groups list-alerts "+id)
}

// safeUID strips ASCII control characters from server-supplied UIDs before
// embedding them in hint command strings, consistent with the pattern in
// oncall_actions.go. Without this, crafted UIDs (e.g. containing newlines)
// could produce misleading multi-line TTY hint output.
func safeUID(uid string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, uid)
}

// emitListAlertsLinkHints emits the conditional rule/instance hints fired
// after `alert-groups list-alerts <id>` (D2 round 17) when at least one
// alert in the result set carries a links.alert.rule.uid.
//
// Multi-rule case: first occurrence wins (avoids hint noise from per-rule
// repetition). SLO and dashboard hints intentionally skipped this round —
// SLO is group-level, dashboard pivot syntax is unconfirmed.
func emitListAlertsLinkHints(stderr io.Writer, ruleUID string) {
	if ruleUID == "" {
		return
	}
	safe := safeUID(ruleUID)
	emitHint(stderr, "inspect rule", "gcx alert rules get "+safe)
	emitHint(stderr, "see live instances", "gcx alert instances list --rule "+safe)
}

// listAlertGroupsLegacy is the SA-token-mode fallback that goes through the
// public-API client (which doesn't return the rich shape). The public API
// supports a smaller filter set than the internal API; unsupported filters
// are passed through to the public client which silently ignores them, and
// we surface a `note:` warning here so the user knows.
func listAlertGroupsLegacy(cmd *cobra.Command, opts *alertGroupListOpts, filters alertGroupListFilters, client OnCallAPI, namespace string) error {
	var listOpts []ListOption
	if filters.MaxAge != "" {
		dur, err := parseDuration(filters.MaxAge)
		if err != nil {
			return fmt.Errorf("invalid --max-age value %q: %w", filters.MaxAge, err)
		}
		cutoff := time.Now().UTC().Add(-dur)
		listOpts = append(listOpts, WithStartedAfter(cutoff))
	}
	if len(filters.Statuses) > 0 {
		listOpts = append(listOpts, WithStatuses(filters.Statuses...))
	}
	if len(filters.Teams) > 0 {
		listOpts = append(listOpts, WithTeams(filters.Teams...))
	}
	if len(filters.Integrations) > 0 {
		listOpts = append(listOpts, WithIntegrations(filters.Integrations...))
	}
	if opts.Limit > 0 {
		listOpts = append(listOpts, WithLimit(opts.Limit))
	}

	// Surface unsupported-filter warnings once at the command edge — the
	// public API doesn't speak is_root, mine, with_resolution_note, or
	// has_related_incident. Honor what we can and tell the user about the
	// rest.
	var unsupported []string
	if filters.IsRoot != nil {
		unsupported = append(unsupported, "is_root (root-only / include-child-groups)")
	}
	if filters.Mine {
		unsupported = append(unsupported, "--mine")
	}
	if filters.WithResolutionNote {
		unsupported = append(unsupported, "--with-resolution-note")
	}
	if filters.HasRelatedIncident {
		unsupported = append(unsupported, "--has-related-incident")
	}
	if len(unsupported) > 0 {
		// Centralised note emission. Agent mode renders
		// JSONL with typed `class`; TTY renders dim plain text. Direct
		// fmt.Fprintf is forbidden because it would bypass the class split.
		emitNote(cmd.ErrOrStderr(),
			"SA-token mode uses the OnCall public API which does not honor: "+strings.Join(unsupported, ", "))
	}

	items, err := client.ListAlertGroups(cmd.Context(), listOpts...)
	if err != nil {
		return err
	}
	// SA-token mode doesn't get the rich shape (no internal API access). The
	// envelope type is the same — most status fields stay empty (omitempty);
	// only AlertsCount/Status (decoded) are populated from the public payload.
	envs := make([]alertGroupEnvelope, 0, len(items))
	for _, item := range items {
		state := ""
		if n, ok := item.Status.(float64); ok {
			s := int(n)
			state = decodeAlertGroupState(&s)
		}
		envs = append(envs, alertGroupEnvelope{
			APIVersion: APIVersion,
			Kind:       "AlertGroup",
			Metadata: k8sMetadata{
				Name:              item.PK,
				Namespace:         namespace,
				CreationTimestamp: item.StartedAt,
			},
			Status: AlertGroupStatus{
				State:       state,
				AlertsCount: item.AlertsCount,
			},
		})
	}
	// Wrap in the items envelope on every list path, including the
	// SA-token fallback.
	return opts.IO.Encode(cmd.OutOrStdout(), alertGroupItemsEnvelope{Items: envs})
}

// alertGroupListHardCap bounds the maximum number of items returned by
// listAlertGroupsRaw when no caller-supplied limit applies. Prevents runaway
// memory when --limit 0 is passed and the server has very many groups.
const alertGroupListHardCap = 1000

// alertGroupListPerPageMax bounds the per-page request size sent to the
// internal API. Conservative — keeps individual round trips small while still
// fitting the default limit (50) into a single request.
const alertGroupListPerPageMax = 100

// listAlertGroupsRaw issues the paginated GET against alertgroups/?... and
// returns the per-item raw JSON for downstream rich conversion plus a
// `hasMore` flag indicating whether the server reported additional pages
// when we stopped early due to the caller-supplied cap.
//
// limit semantics:
//   - limit > 0  → fetch up to `limit` items; perpage=min(limit, perPageMax).
//   - limit == 0 → fetch up to alertGroupListHardCap items; perpage=perPageMax.
//
// hasMore is true only when the result was truncated by `limit` AND the page
// that triggered the stop reported a non-empty `next` cursor. It stays false
// when the server's pagination naturally ends or when only the hardCap kicks
// in (the latter is silent — `--limit 0` callers opted into "give me all").
func listAlertGroupsRaw(ctx context.Context, c *OnCallClient, filters alertGroupListFilters, limit int) ([]json.RawMessage, bool, error) {
	params := url.Values{}
	if filters.MaxAge != "" {
		dur, err := parseDuration(filters.MaxAge)
		if err != nil {
			return nil, false, fmt.Errorf("invalid --max-age value %q: %w", filters.MaxAge, err)
		}
		const layout = "2006-01-02T15:04:05"
		start := time.Now().UTC().Add(-dur).Format(layout)
		end := time.Now().UTC().Format(layout)
		params.Set("started_at", start+"_"+end)
	}
	for _, s := range filters.Statuses {
		params.Add("status", strconv.Itoa(s))
	}
	if filters.IsRoot != nil {
		if *filters.IsRoot {
			params.Set("is_root", "true")
		} else {
			params.Set("is_root", "false")
		}
	}
	for _, t := range filters.Teams {
		params.Add("team", t)
	}
	for _, i := range filters.Integrations {
		params.Add("integration", i)
	}
	if filters.Mine {
		params.Set("mine", "true")
	}
	if filters.WithResolutionNote {
		params.Set("with_resolution_note", "true")
	}
	if filters.HasRelatedIncident {
		params.Set("has_related_incident", "true")
	}

	// perpage: the OnCall internal API uses `perpage` (NOT page_size, which is
	// silently ignored). We set it only on the first request — the cursor URL
	// echoed in `next` already encodes perpage for follow-up pages.
	perPage := alertGroupListPerPageMax
	if limit > 0 {
		perPage = min(limit, alertGroupListPerPageMax)
	}
	params.Set("perpage", strconv.Itoa(perPage))

	path := alertGroupsPath + "?" + params.Encode()

	// effectiveCap: the upper bound on `out`. When the user passes --limit 0
	// we still want a runaway guard, so fall back to alertGroupListHardCap.
	effectiveCap := alertGroupListHardCap
	if limit > 0 && limit < effectiveCap {
		effectiveCap = limit
	}

	var (
		out           []json.RawMessage
		next          = path
		serverHasMore bool
	)
	for next != "" {
		resp, err := c.DoRequest(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, false, fmt.Errorf("irm: list alert groups: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, false, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, false, fmt.Errorf("irm: list alert groups: HTTP %d: %s", resp.StatusCode, string(body))
		}
		var page struct {
			Results []json.RawMessage `json:"results"`
			Next    *string           `json:"next"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, false, fmt.Errorf("irm: decode alert groups: %w", err)
		}
		out = append(out, page.Results...)
		pageNext := ""
		if page.Next != nil {
			pageNext = *page.Next
		}
		if len(out) >= effectiveCap {
			out = out[:effectiveCap]
			serverHasMore = pageNext != ""
			break
		}
		if pageNext == "" {
			break
		}
		np, err := ExtractNextPath(pageNext)
		if err != nil {
			return nil, false, err
		}
		next = np
	}

	// serverHasMore is true only when the cap (caller-supplied or hardCap)
	// truncated us AND the server reported a non-empty `next` cursor on the
	// page we stopped on. The caller decides whether to surface a hint based
	// on its own --limit semantics.
	return out, serverHasMore, nil
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}

// formatRelativeAge renders a timestamp as a compact "Nh ago" / "Nd ago" string
// for the STARTED column on alert-groups list and list-alerts. Empty/zero/
// unparseable input yields "-" so the column never renders empty cells.
func formatRelativeAge(ts string) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// OnCall sometimes serializes without a trailing Z — try a couple of
		// fallback layouts before giving up.
		for _, layout := range []string{"2006-01-02T15:04:05.999999Z", "2006-01-02T15:04:05Z", "2006-01-02T15:04:05"} {
			if tt, e := time.Parse(layout, ts); e == nil {
				t = tt
				err = nil
				break
			}
		}
		if err != nil {
			return "-"
		}
	}
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	}
}

// alertCollapseKey returns the de-duplication key for an alert envelope:
// the Alertmanager fingerprint (status.links.alert.instance.id) when
// present (== hash of the alert's labels per AM semantics, so collapsing
// by fingerprint is equivalent to collapsing by sorted-labels equality);
// otherwise the canonical sorted-labels rendering of the dimensions block,
// otherwise the alert's own metadata.name (so alerts with no fingerprint
// AND no dimensions stay distinct rows — the natural fallback).
func alertCollapseKey(env alertEnvelope) string {
	if env.Status.Links != nil && env.Status.Links.Alert != nil && env.Status.Links.Alert.Instance != nil {
		if id := env.Status.Links.Alert.Instance.ID; id != "" {
			return "fp:" + id
		}
	}
	if env.Status.Dimensions != nil && len(env.Status.Dimensions.Labels) > 0 {
		return "dim:" + canonicalLabelKey(env.Status.Dimensions.Labels)
	}
	// Distinct metadata.name → distinct row. Prefix avoids collisions with
	// the fp: / dim: namespaces.
	return "id:" + env.Metadata.Name
}

// applyAlertCollapse implements the ADR §6 collapse semantics for
// `alert-groups list-alerts`. With history=false (default), groups stored
// alerts by alertCollapseKey; emits one envelope per unique key with
// status.occurrences = count within that group; preserves the first-seen
// envelope's metadata/spec/status for the row. With history=true, every
// stored alert keeps its own row and occurrences=1.
//
// The collapse is uniform across -o table / -o yaml / -o json (it's a
// behaviour mode, not an output mode).
func applyAlertCollapse(envs []alertEnvelope, history bool) []alertEnvelope {
	if history {
		out := make([]alertEnvelope, len(envs))
		for i, env := range envs {
			env.Status.Occurrences = 1
			out[i] = env
		}
		return out
	}
	if len(envs) == 0 {
		return envs
	}
	type slot struct {
		idx int
		env alertEnvelope
	}
	order := make([]string, 0, len(envs))
	bucket := make(map[string]*slot, len(envs))
	for _, env := range envs {
		key := alertCollapseKey(env)
		if s, ok := bucket[key]; ok {
			s.env.Status.Occurrences++
			continue
		}
		s := &slot{idx: len(order), env: env}
		s.env.Status.Occurrences = 1
		bucket[key] = s
		order = append(order, key)
	}
	out := make([]alertEnvelope, 0, len(order))
	for _, k := range order {
		out = append(out, bucket[k].env)
	}
	return out
}

// alertGroupGetRichOpts wires up `alert-groups get <id>` with the codec
// contract: `text` (single-row table) is the TTY default; `agents` flips
// in via the shared mechanism in agent mode; `yaml`, `json`, `agents`, and
// `wide` are selectable via `-o`. format.NewOrderedYAMLCodec()
// preserves the rich-type Go struct field declaration order under -o yaml.
type alertGroupGetRichOpts struct {
	IO         cmdio.Options
	IncludeRaw bool
}

func (o *alertGroupGetRichOpts) setup(flags *pflag.FlagSet) {
	// Register the table codec so `get` renders the same
	// single-row layout as `list` and call DefaultFormat("table") — uniform
	// with the CRUD-data-command default model in CONSTITUTION/DESIGN.
	// The codec.Encode method accepts a single `alertGroupEnvelope` value as
	// well as the list envelope shapes (see alertGroupTableCodec.Encode).
	o.IO.RegisterCustomCodec("table", &alertGroupTableCodec{})
	o.IO.RegisterCustomCodec("wide", &alertGroupTableCodec{Wide: true})
	// Stable yaml key order via format.NewOrderedYAMLCodec().
	o.IO.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.IncludeRaw, "include-raw", false, "Include the unprocessed Alertmanager-shape payload under status.raw (hidden by default; the curated status.{target,links,...} blocks are the promoted view of the same data)")
}

// Validate is a stub to satisfy the CONSTITUTION-required
// opts/setup/Validate/constructor quartet. The flag set has no cross-field
// invariants beyond what opts.IO.Validate() already enforces.
func (o *alertGroupGetRichOpts) Validate() error { return nil }

func newAlertGroupGetRichCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupGetRichOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get an alert group by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, namespace, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return err
			}
			reader, ok := client.(RichAlertGroupReader)
			if !ok {
				return errors.New("alert-groups get requires the OAuth plugin proxy; SA-token mode does not support rich operations")
			}

			rich, err := reader.GetAlertGroupRich(ctx, args[0])
			if err != nil {
				return err
			}
			if !opts.IncludeRaw {
				rich.Status.Raw = nil
			}
			// Labels are not available on the get path (GetAlertGroupRich does
			// not return the raw *alertGroupAPI). Pass nil so metadata.labels is
			// omitted (omitempty); the curated status block carries the data.
			env, err := alertGroupRichToEnvelope(rich, nil, namespace)
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), env); err != nil {
				return err
			}
			// Post-result drilldown hints on success.
			// Three pivots (alert rule, dashboard, per-alert detail), each
			// filled with the actual identifiers from the result. Hints are
			// only emitted when the corresponding identifier is populated —
			// omitempty everywhere keeps the JSON honest —
			// so the same omission logic applies to hints.
			emitAlertGroupGetNavHints(cmd.ErrOrStderr(), env, args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type alertGroupListAlertsOpts struct {
	listOpts

	Slim       bool
	Limit      int
	IncludeRaw bool
	History    bool
}

func (o *alertGroupListAlertsOpts) setup(flags *pflag.FlagSet) {
	o.listOpts.setup(flags, "alerts")
	o.IO.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	flags.BoolVar(&o.Slim, "slim", false, "Skip per-alert retrieval; emit only metadata + alert-group back-pointer")
	flags.IntVar(&o.Limit, "limit", alertGroupsListAlertsCap, "Cap on number of alerts retrieved (0 = no cap)")
	flags.BoolVar(&o.IncludeRaw, "include-raw", false, "Include the unprocessed Alertmanager-shape payload under status.raw on each alert (hidden by default; status.{dimensions,links,...} are the promoted view of the same data)")
	flags.BoolVar(&o.History, "history", false, "Opt out of collapse: emit every stored Alert as its own row with status.occurrences=1 (default behaviour collapses re-fires by alert label set)")
}

// Validate is a stub to satisfy the CONSTITUTION-required
// opts/setup/Validate/constructor quartet. The flag set has no cross-field
// invariants beyond what opts.IO.Validate() already enforces.
func (o *alertGroupListAlertsOpts) Validate() error { return nil }

func newAlertGroupListAlertsCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupListAlertsOpts{}
	cmd := &cobra.Command{
		Use:   "list-alerts <alert-group-id>",
		Short: "List individual alerts for an alert group.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			groupID := args[0]

			client, namespace, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return err
			}
			stderr := cmd.ErrOrStderr()
			reader, ok := client.(RichAlertGroupReader)
			if !ok {
				// Fall back to the slim public-API alerts list — no rich shape.
				items, err := client.ListAlerts(ctx, groupID)
				if err != nil {
					return err
				}
				envs := make([]alertEnvelope, 0, len(items))
				for _, item := range items {
					envs = append(envs, alertEnvelope{
						APIVersion: APIVersion,
						Kind:       "Alert",
						Metadata: k8sMetadata{
							Name:              item.ID,
							Namespace:         namespace,
							CreationTimestamp: item.CreatedAt,
						},
						Spec: AlertSpec{AlertGroupID: groupID},
					})
				}
				return opts.IO.Encode(cmd.OutOrStdout(), alertItemsEnvelope{Items: envs})
			}

			limit := opts.Limit
			ids, total, err := reader.ListAlertIDs(ctx, groupID, limit)
			if err != nil {
				return err
			}
			if limit > 0 && len(ids) > limit {
				ids = ids[:limit]
			}
			if limit > 0 && total > len(ids) {
				// Warn class on stderr; agent mode renders
				// JSONL with typed `class`, TTY renders dim plain text.
				emitWarn(stderr,
					fmt.Sprintf("retrieved %d of %d alerts; pass `--limit 0` to fetch all", len(ids), total))
			}

			if opts.Slim {
				envs := make([]alertEnvelope, 0, len(ids))
				for _, id := range ids {
					envs = append(envs, slimAlertEnvelope(id, groupID, namespace))
				}
				return opts.IO.Encode(cmd.OutOrStdout(), alertItemsEnvelope{Items: envs})
			}

			envs, err := fetchAlertsRichConcurrent(ctx, reader, ids, groupID, namespace, opts.IncludeRaw)
			if err != nil {
				return err
			}

			// Collapse semantics (ADR §6 + brief A2): default mode groups
			// stored alerts by their AM fingerprint (== hash of labels) and
			// emits one row per unique label set with occurrences=N. With
			// --history the collapse is bypassed: every stored alert is its
			// own row with occurrences=1.
			envs = applyAlertCollapse(envs, opts.History)

			if err := opts.IO.Encode(cmd.OutOrStdout(), alertItemsEnvelope{Items: envs}); err != nil {
				return err
			}

			// Conditional rule-pivot hints (D2 round 17). Emit when the
			// result set carries at least one rule UID; first occurrence
			// wins so multi-rule groups don't produce hint noise.
			emitListAlertsLinkHints(cmd.ErrOrStderr(), firstAlertRuleUID(envs))
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// slimAlertEnvelope builds a typed envelope for an alert without the rich
// status block — used for `--slim` output that skips the N+1 fetch.
func slimAlertEnvelope(id, groupID, namespace string) alertEnvelope {
	return alertEnvelope{
		APIVersion: APIVersion,
		Kind:       "Alert",
		Metadata: k8sMetadata{
			Name:      id,
			Namespace: namespace,
		},
		Spec: AlertSpec{AlertGroupID: groupID},
	}
}

// fetchAlertsRichConcurrent fans out alert retrieves with bounded concurrency.
// On error from any single retrieve, the function aborts and returns the first error.
func fetchAlertsRichConcurrent(ctx context.Context, c RichAlertGroupReader, ids []string, groupID, namespace string, includeRaw bool) ([]alertEnvelope, error) {
	results := make([]alertEnvelope, len(ids))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(alertGroupsListAlertsConcurrency)
	for i, id := range ids {
		g.Go(func() error {
			api, rich, err := c.GetAlertRich(gctx, id)
			if err != nil {
				return fmt.Errorf("alert %s: %w", id, err)
			}
			if !includeRaw {
				rich.Status.Raw = nil
			}
			env, err := alertRichToEnvelope(api, rich, groupID, namespace)
			if err != nil {
				return err
			}
			results[i] = env
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// (Action-verb wiring lives in oncall_actions.go: every mutating verb on
// `alert-groups` goes through newActionVerbCommand against a verbConfig.)

// ---------------------------------------------------------------------------
// final-shifts command (mounted under schedules)
// ---------------------------------------------------------------------------

type finalShiftsOpts struct {
	IO    cmdio.Options
	Start string
	End   string
}

func (o *finalShiftsOpts) setup(flags *pflag.FlagSet) {
	today := time.Now().Format("2006-01-02")
	endDate := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	o.Start = today
	o.End = endDate

	o.IO.RegisterCustomCodec("table", &finalShiftTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Start, "start", o.Start, "Start date (YYYY-MM-DD)")
	flags.StringVar(&o.End, "end", o.End, "End date (YYYY-MM-DD)")
}

func newScheduleFinalShiftsCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &finalShiftsOpts{}
	cmd := &cobra.Command{
		Use:   "final-shifts <schedule-id>",
		Short: "List final shifts for a schedule.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			startDate, err := time.Parse("2006-01-02", opts.Start)
			if err != nil {
				return fmt.Errorf("invalid --start date %q: expected YYYY-MM-DD", opts.Start)
			}
			endDate, err := time.Parse("2006-01-02", opts.End)
			if err != nil {
				return fmt.Errorf("invalid --end date %q: expected YYYY-MM-DD", opts.End)
			}
			days := int(endDate.Sub(startDate).Hours()/24) + 1
			if days < 1 {
				return errors.New("--end must be after --start")
			}

			tz := time.Now().Location().String()
			result, err := client.ListFilterEvents(cmd.Context(), args[0], tz, opts.Start, days)
			if err != nil {
				return err
			}

			var shifts []FlatShift
			for _, event := range result.Events {
				if event.IsGap {
					continue
				}
				for _, user := range event.Users {
					shifts = append(shifts, FlatShift{
						UserPK:       user.PK,
						UserEmail:    user.Email,
						UserUsername: user.DisplayName,
						ShiftStart:   event.Start,
						ShiftEnd:     event.End,
					})
				}
			}

			return opts.IO.Encode(cmd.OutOrStdout(), shifts)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// users command: list, get, current
// ---------------------------------------------------------------------------

type usersCurrentOpts struct {
	IO cmdio.Options
}

func (o *usersCurrentOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newUsersCommand(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "users",
		Short:   "Manage OnCall users.",
		Aliases: []string{"user"},
	}

	cmd.AddCommand(
		newListSubcommand(loader, "users", "User", "List OnCall users.", "pk",
			func(ctx context.Context, c OnCallAPI) ([]User, error) { return c.ListUsers(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*User, error) { return c.GetUser(ctx, name) }),
		newGetSubcommand(loader, "Get a user by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*User, error) { return c.GetUser(ctx, name) }),
		newUsersCurrentCommand(loader),
	)

	return cmd
}

func newUsersCurrentCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &usersCurrentOpts{}
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Get the current user.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			user, err := client.GetCurrentUser(cmd.Context())
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), user)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// escalate command (uses internal API direct_paging endpoint)
// ---------------------------------------------------------------------------

type escalateOpts struct {
	IO        cmdio.Options
	Title     string
	Message   string
	Team      string
	UserIDs   []string
	Important bool
}

func (o *escalateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Title, "title", "", "Title of the escalation (required)")
	flags.StringVar(&o.Message, "message", "", "Message for the escalation")
	flags.StringVar(&o.Team, "team", "", "Team ID")
	flags.StringSliceVar(&o.UserIDs, "user-ids", nil, "User IDs (comma-separated)")
	flags.BoolVar(&o.Important, "important", false, "Mark as important")
}

func (o *escalateOpts) Validate() error {
	if o.Title == "" {
		return errors.New("--title is required")
	}
	return nil
}

func newEscalateCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &escalateOpts{}
	cmd := &cobra.Command{
		Use:   "escalate",
		Short: "Create a direct escalation.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			// Build internal API input with per-user importance.
			var users []UserReference
			for _, uid := range opts.UserIDs {
				users = append(users, UserReference{
					ID:        uid,
					Important: opts.Important,
				})
			}

			input := DirectPagingInput{
				Title:                   opts.Title,
				Message:                 opts.Message,
				Team:                    opts.Team,
				Users:                   users,
				ImportantTeamEscalation: opts.Important,
			}

			result, err := client.CreateDirectPaging(cmd.Context(), input)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Direct escalation created with alert group ID: %s", result.AlertGroupID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// itemsToUnstructured + FinalShift table codec
// ---------------------------------------------------------------------------

func itemsToUnstructured[T any](items []T, kind, idField, namespace string) ([]unstructured.Unstructured, error) {
	objs := make([]unstructured.Unstructured, 0, len(items))
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %s: %w", kind, err)
		}

		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("failed to unmarshal %s: %w", kind, err)
		}

		id := ""
		if v, ok := m[idField]; ok {
			id = fmt.Sprint(v)
		}
		delete(m, idField)

		obj := unstructured.Unstructured{Object: map[string]any{
			"apiVersion": APIVersion,
			"kind":       kind,
			"metadata": map[string]any{
				"name":      id,
				"namespace": namespace,
			},
			"spec": m,
		}}
		objs = append(objs, obj)
	}
	return objs, nil
}

// decodeOnCallLabels extracts the OnCall app's user-set labels[] off the alert
// group payload and returns them as a {key: value} map for inclusion under
// metadata.labels. Returns nil when no usable labels are present.
//
// The OnCall internal API serializes labels as `[{"key": {...}, "value": {...}}, ...]`
// where each side is itself an object — we accept either string or {name|repr|id}
// nested forms as the value to be friendly to schema variation.
func decodeOnCallLabels(api *alertGroupAPI) map[string]any {
	if len(api.Labels) == 0 || string(api.Labels) == "null" {
		return nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(api.Labels, &arr); err != nil {
		return nil
	}
	out := map[string]any{}
	for _, lbl := range arr {
		k := lblFieldString(lbl["key"])
		v := lblFieldString(lbl["value"])
		if k != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// lblFieldString coerces a label key/value field into a flat string. Strings
// pass through; nested objects yield the first non-empty of name/repr/id.
func lblFieldString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		for _, k := range []string{"name", "repr", "id"} {
			if s, ok := t[k].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// k8sMetadata is a typed metadata block with explicit field order (name,
// namespace, creationTimestamp, labels). Used by the typed envelope structs
// below to render meaningful YAML order through go-yaml's struct-aware encoder.
type k8sMetadata struct {
	Name              string         `json:"name" yaml:"name"`
	Namespace         string         `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	CreationTimestamp string         `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
	Labels            map[string]any `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// alertGroupEnvelope is the K8s-style envelope for a single AlertGroup with
// fields in a meaningful order — used in place of unstructured.Unstructured
// where ordered YAML/JSON output matters (i.e., the get-style commands).
type alertGroupEnvelope struct {
	APIVersion string           `json:"apiVersion" yaml:"apiVersion"`
	Kind       string           `json:"kind" yaml:"kind"`
	Metadata   k8sMetadata      `json:"metadata" yaml:"metadata"`
	Spec       AlertGroupSpec   `json:"spec" yaml:"spec"`
	Status     AlertGroupStatus `json:"status" yaml:"status"`
}

// alertEnvelope is the K8s-style envelope for a single Alert with explicit field order.
type alertEnvelope struct {
	APIVersion string      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string      `json:"kind" yaml:"kind"`
	Metadata   k8sMetadata `json:"metadata" yaml:"metadata"`
	Spec       AlertSpec   `json:"spec" yaml:"spec"`
	Status     AlertStatus `json:"status" yaml:"status"`
}

// alertGroupItemsEnvelope wraps the list of `alert-groups list` rows in the
// list envelope: stdout MUST be `{"items": [...]}` — never a bare array,
// never `null`. Empty result is `{"items": []}`.
//
// The table codec recognises this shape and renders the embedded slice; the
// json / yaml / agents codecs serialise it verbatim and the `--json list`
// discovery path follows the `items` key automatically (see
// `internal/output/format.go::marshalToSampleMap`).
type alertGroupItemsEnvelope struct {
	Items []alertGroupEnvelope `json:"items" yaml:"items"`
}

// alertItemsEnvelope is the list envelope for `alert-groups list-alerts`.
type alertItemsEnvelope struct {
	Items []alertEnvelope `json:"items" yaml:"items"`
}

// alertGroupRichToEnvelope wraps the rich AlertGroup into the typed envelope
// for ordered emission. Mirrors alertGroupRichToUnstructured but produces a
// struct (not an unstructured map) so JSON/YAML encoders preserve field order.
//
// labels is the decoded OnCall metadata.labels map; callers that hold the raw
// alertGroupAPI should pass decodeOnCallLabels(api). On the get path (where
// GetAlertGroupRich returns only *AlertGroupRich), pass nil — metadata.labels
// will be omitted (omitempty).
func alertGroupRichToEnvelope(rich *AlertGroupRich, labels map[string]any, namespace string) (alertGroupEnvelope, error) {
	if rich == nil {
		return alertGroupEnvelope{}, errors.New("internal: nil rich payload")
	}
	return alertGroupEnvelope{
		APIVersion: APIVersion,
		Kind:       "AlertGroup",
		Metadata: k8sMetadata{
			Name:              rich.Metadata.PK,
			Namespace:         namespace,
			CreationTimestamp: rich.Metadata.StartedAt,
			Labels:            labels,
		},
		Spec:   rich.Spec,
		Status: rich.Status,
	}, nil
}

// alertRichToEnvelope wraps the rich Alert into the typed envelope.
func alertRichToEnvelope(api *alertAPI, rich *AlertRich, groupID, namespace string) (alertEnvelope, error) {
	if api == nil || rich == nil {
		return alertEnvelope{}, errors.New("internal: nil api or rich payload")
	}
	if rich.Spec.AlertGroupID == "" && groupID != "" {
		rich.Spec.AlertGroupID = groupID
	}
	return alertEnvelope{
		APIVersion: APIVersion,
		Kind:       "Alert",
		Metadata: k8sMetadata{
			Name:              api.ID,
			Namespace:         namespace,
			CreationTimestamp: api.CreatedAt,
		},
		Spec:   rich.Spec,
		Status: rich.Status,
	}, nil
}

type finalShiftTableCodec struct{ noDecodeCodec }

func (c *finalShiftTableCodec) Format() format.Format { return "table" }

func (c *finalShiftTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]FlatShift)
	if !ok {
		return errors.New("invalid data type for table codec: expected []FlatShift")
	}

	t := style.NewTable("USER_PK", "EMAIL", "USERNAME", "START", "END")
	for _, item := range items {
		start := item.ShiftStart
		if len(start) > 16 {
			start = start[:16]
		}
		end := item.ShiftEnd
		if len(end) > 16 {
			end = end[:16]
		}
		t.Row(item.UserPK, item.UserEmail, item.UserUsername, start, end)
	}
	return t.Render(w)
}
