package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	goyaml "github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// orderedYAMLCodec encodes via go-yaml directly (instead of the default
// JSON→YAML round trip used by format.YAMLCodec). The default path goes through
// sigs.k8s.io/yaml.JSONToYAML which loses object-key order; this codec
// preserves Go struct field declaration order via go-yaml's UseJSONMarshaler
// — required because the rich AlertGroup/Alert envelope's status block has a
// deliberately non-alphabetical field order optimized for SRE drilldown.
type orderedYAMLCodec struct{ noDecodeCodec }

func (c *orderedYAMLCodec) Format() format.Format { return format.YAML }

func (c *orderedYAMLCodec) Encode(w io.Writer, v any) error {
	return goyaml.NewEncoder(w, goyaml.Indent(2), goyaml.IndentSequence(true), goyaml.UseJSONMarshaler()).Encode(v)
}

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
	o.IO.RegisterCustomCodec("yaml", &orderedYAMLCodec{})
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
		newAcknowledgeCommand(loader),
		newAlertGroupActionCommand(loader, "resolve", "Resolve an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.ResolveAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "unacknowledge", "Unacknowledge an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.UnacknowledgeAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "unresolve", "Unresolve an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.UnresolveAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupSilenceCommand(loader),
		newAlertGroupActionCommand(loader, "unsilence", "Unsilence an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.UnsilenceAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "delete", "Delete an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.DeleteAlertGroup(cmd.Context(), id)
		}),
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

			// Best-effort rich shape via the OAuth proxy path. SA-token mode
			// (oncallpublic) doesn't speak the internal API, so fall back to
			// the legacy AlertGroup shape there — fields that aren't on that
			// type just stay empty (omitempty).
			oc, ok := client.(*OnCallClient)
			if !ok {
				return listAlertGroupsLegacy(cmd, opts, filters, client, namespace)
			}

			rawItems, serverHasMore, err := listAlertGroupsRaw(cmd.Context(), oc, filters, opts.Limit)
			if err != nil {
				return err
			}

			teams, _ := oc.resolveTeams(cmd.Context()) // best-effort

			envs := make([]alertGroupEnvelope, 0, len(rawItems))
			for _, item := range rawItems {
				api, rich, err := listAlertGroupRichFromBytes(item, teams)
				if err != nil {
					return err
				}
				env, err := alertGroupRichToEnvelope(api, rich, namespace)
				if err != nil {
					return err
				}
				envs = append(envs, env)
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), envs); err != nil {
				return err
			}

			// Hint emission (locked shape, D2 round 14): only when the user
			// accepted truncation (--limit > 0), the result hit the limit
			// exactly, AND the server confirmed more pages exist. Otherwise
			// silent.
			if opts.Limit > 0 && len(envs) == opts.Limit && serverHasMore {
				emitAlertGroupListLimitHint(cmd.ErrOrStderr(), opts.Limit)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// emitAlertGroupListLimitHint surfaces the D2-round-14 truncation hint on
// stderr when alert-groups list returns exactly `limit` rows AND the server
// reported a non-empty `next` cursor. Format mirrors the locked shape:
//
//	TTY:    "hint: showing first N results — pass --limit M to fetch more or --limit 0 for all"
//	agent:  {"class":"hint","summary":"<same>"}
//
// The "next limit" suggestion doubles the current limit (50→100, 5→10),
// matching the locked shape's example and giving the user a sensible
// next step without committing to a full --limit 0 fetch.
func emitAlertGroupListLimitHint(stderr io.Writer, limit int) {
	suggested := limit * 2
	summary := fmt.Sprintf("showing first %d results — pass --limit %d to fetch more or --limit 0 for all", limit, suggested)
	emitHint(stderr, summary, "")
}

// listAlertGroupsLegacy is the SA-token-mode fallback that goes through the
// public-API client (which doesn't return the rich shape). The public API
// supports a smaller filter set than the internal API; unsupported filters
// are passed through to the public client which silently ignores them, and
// we surface a `note:` warning here so the user knows.
func listAlertGroupsLegacy(cmd *cobra.Command, opts *alertGroupListOpts, filters alertGroupListFilters, client OnCallAPI, namespace string) error {
	var listOpts []oncalltypes.ListOption
	if filters.MaxAge != "" {
		dur, err := parseDuration(filters.MaxAge)
		if err != nil {
			return fmt.Errorf("invalid --max-age value %q: %w", filters.MaxAge, err)
		}
		cutoff := time.Now().UTC().Add(-dur)
		listOpts = append(listOpts, oncalltypes.WithStartedAfter(cutoff))
	}
	if len(filters.Statuses) > 0 {
		listOpts = append(listOpts, oncalltypes.WithStatuses(filters.Statuses...))
	}
	if len(filters.Teams) > 0 {
		listOpts = append(listOpts, oncalltypes.WithTeams(filters.Teams...))
	}
	if len(filters.Integrations) > 0 {
		listOpts = append(listOpts, oncalltypes.WithIntegrations(filters.Integrations...))
	}
	if opts.Limit > 0 {
		listOpts = append(listOpts, oncalltypes.WithLimit(opts.Limit))
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
		fmt.Fprintf(os.Stderr, "note: SA-token mode uses the OnCall public API which does not honor: %s\n", strings.Join(unsupported, ", "))
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
			Status: oncalltypes.AlertGroupStatus{
				State:       state,
				AlertsCount: item.AlertsCount,
			},
		})
	}
	return opts.IO.Encode(cmd.OutOrStdout(), envs)
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
		params.Add("status", fmt.Sprintf("%d", s))
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
	params.Set("perpage", fmt.Sprintf("%d", perPage))

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

// alertGroupGetRichOpts mirrors getOpts but uses Yaml as default.
type alertGroupGetRichOpts struct {
	IO         cmdio.Options
	IncludeRaw bool
	Open       bool
}

func (o *alertGroupGetRichOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("yaml", &orderedYAMLCodec{})
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.IncludeRaw, "include-raw", false, "Include the unprocessed Alertmanager-shape payload under status.raw (hidden by default; the curated status.{target,links,...} blocks are the promoted view of the same data)")
	flags.BoolVar(&o.Open, "open", false, "Open the alert group in your browser after retrieval succeeds.")
}

// alertGroupOpenNote is the JSONL event emitted on stderr in agent mode when
// --open is passed: a "note" with the resolved permalink (browser interaction
// is not attempted in agent mode), or a "warning" when the permalink is empty.
type alertGroupOpenNote struct {
	Class   string `json:"class"`
	Summary string `json:"summary"`
	URL     string `json:"url,omitempty"`
}

func newAlertGroupGetRichCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupGetRichOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get an alert group by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, namespace, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return err
			}
			oc, ok := client.(*OnCallClient)
			if !ok {
				return errors.New("alert-groups get requires the OAuth plugin proxy (this context uses an SA token)")
			}

			api, rich, err := oc.GetAlertGroupRich(ctx, args[0])
			if err != nil {
				return err
			}
			if !opts.IncludeRaw {
				rich.Status.Raw = nil
			}
			env, err := alertGroupRichToEnvelope(api, rich, namespace)
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), env); err != nil {
				return err
			}

			if opts.Open {
				return handleAlertGroupOpen(cmd.ErrOrStderr(), env.Spec.Permalinks.Web)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// handleAlertGroupOpen implements the --open behavior for `alert-groups get`:
//   - In agent mode, never attempt to launch a browser; emit a JSONL note (or
//     warning when the permalink is empty) on stderr and return nil.
//   - In TTY mode, open the permalink in the default browser when present;
//     surface a warning when it is missing. Per the locked shape, an empty
//     permalink does not fail the command.
func handleAlertGroupOpen(stderr io.Writer, permalink string) error {
	if agent.IsAgentMode() {
		ev := alertGroupOpenNote{
			Class:   "note",
			Summary: "--open is ignored in agent mode",
			URL:     permalink,
		}
		if permalink == "" {
			ev = alertGroupOpenNote{
				Class:   "warning",
				Summary: "permalink unavailable",
			}
		}
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		fmt.Fprintln(stderr, string(b))
		return nil
	}
	if permalink == "" {
		cmdio.Warning(stderr, "permalink unavailable")
		return nil
	}
	cmdio.Info(stderr, "Opening %s", permalink)
	return deeplink.Open(permalink)
}

type alertGroupListAlertsOpts struct {
	listOpts
	Slim       bool
	Limit      int
	IncludeRaw bool
}

func (o *alertGroupListAlertsOpts) setup(flags *pflag.FlagSet) {
	o.listOpts.setup(flags, "alerts")
	o.IO.RegisterCustomCodec("yaml", &orderedYAMLCodec{})
	flags.BoolVar(&o.Slim, "slim", false, "Skip per-alert retrieval; emit only metadata + alert-group back-pointer")
	flags.IntVar(&o.Limit, "limit", alertGroupsListAlertsCap, "Cap on number of alerts retrieved (0 = no cap)")
	flags.BoolVar(&o.IncludeRaw, "include-raw", false, "Include the unprocessed Alertmanager-shape payload under status.raw on each alert (hidden by default; status.{target,links,...} are the promoted view of the same data)")
}

func newAlertGroupListAlertsCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupListAlertsOpts{}
	cmd := &cobra.Command{
		Use:   "list-alerts <alert-group-id>",
		Short: "List individual alerts for an alert group.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			groupID := args[0]

			client, namespace, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return err
			}
			oc, ok := client.(*OnCallClient)
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
						Spec: oncalltypes.AlertSpec{AlertGroupID: groupID},
					})
				}
				return opts.IO.Encode(cmd.OutOrStdout(), envs)
			}

			limit := opts.Limit
			ids, total, err := oc.listAlertIDs(ctx, groupID, limit)
			if err != nil {
				return err
			}
			if limit > 0 && len(ids) > limit {
				ids = ids[:limit]
			}
			if limit > 0 && total > len(ids) {
				fmt.Fprintf(os.Stderr, "warn: retrieved %d of %d alerts; pass `--limit 0` to fetch all\n", len(ids), total)
			}

			if opts.Slim {
				envs := make([]alertEnvelope, 0, len(ids))
				for _, id := range ids {
					envs = append(envs, slimAlertEnvelope(id, groupID, namespace))
				}
				return opts.IO.Encode(cmd.OutOrStdout(), envs)
			}

			envs, err := fetchAlertsRichConcurrent(ctx, oc, ids, groupID, namespace, opts.IncludeRaw)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), envs)
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
		Spec: oncalltypes.AlertSpec{AlertGroupID: groupID},
	}
}

// fetchAlertsRichConcurrent fans out alert retrieves with bounded concurrency.
// On error from any single retrieve, the function aborts and returns the first error.
func fetchAlertsRichConcurrent(ctx context.Context, c *OnCallClient, ids []string, groupID, namespace string, includeRaw bool) ([]alertEnvelope, error) {
	results := make([]alertEnvelope, len(ids))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(alertGroupsListAlertsConcurrency)
	for i, id := range ids {
		i, id := i, id
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

type alertGroupActionOpts struct {
	IO cmdio.Options
}

func (o *alertGroupActionOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newAlertGroupActionCommand(loader OnCallConfigLoader, name, short string, actionFn func(OnCallAPI, *cobra.Command, string) error) *cobra.Command {
	opts := &alertGroupActionOpts{}
	cmd := &cobra.Command{
		Use:   name + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			id := args[0]
			if err := actionFn(client, cmd, id); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Alert group %q %s successfully", id, name)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newAlertGroupSilenceCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupActionOpts{}
	var duration int
	cmd := &cobra.Command{
		Use:   "silence <id>",
		Short: "Silence an alert group for a specified duration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			id := args[0]
			if err := client.SilenceAlertGroup(cmd.Context(), id, duration); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Alert group %q silenced for %d seconds", id, duration)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	cmd.Flags().IntVar(&duration, "duration", 3600, "Duration to silence in seconds")
	return cmd
}

// ---------------------------------------------------------------------------
// final-shifts command (mounted under schedules)
// Uses the internal API filter_events endpoint instead of the public final_shifts endpoint.
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
	APIVersion string                       `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                       `json:"kind" yaml:"kind"`
	Metadata   k8sMetadata                  `json:"metadata" yaml:"metadata"`
	Spec       oncalltypes.AlertGroupSpec   `json:"spec" yaml:"spec"`
	Status     oncalltypes.AlertGroupStatus `json:"status" yaml:"status"`
}

// alertEnvelope is the K8s-style envelope for a single Alert with explicit field order.
type alertEnvelope struct {
	APIVersion string                  `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                  `json:"kind" yaml:"kind"`
	Metadata   k8sMetadata             `json:"metadata" yaml:"metadata"`
	Spec       oncalltypes.AlertSpec   `json:"spec" yaml:"spec"`
	Status     oncalltypes.AlertStatus `json:"status" yaml:"status"`
}

// alertGroupRichToEnvelope wraps the rich AlertGroup into the typed envelope
// for ordered emission. Mirrors alertGroupRichToUnstructured but produces a
// struct (not an unstructured map) so JSON/YAML encoders preserve field order.
func alertGroupRichToEnvelope(api *alertGroupAPI, rich *oncalltypes.AlertGroupRich, namespace string) (alertGroupEnvelope, error) {
	if api == nil || rich == nil {
		return alertGroupEnvelope{}, errors.New("internal: nil api or rich payload")
	}
	return alertGroupEnvelope{
		APIVersion: APIVersion,
		Kind:       "AlertGroup",
		Metadata: k8sMetadata{
			Name:              api.PK,
			Namespace:         namespace,
			CreationTimestamp: api.StartedAt,
			Labels:            decodeOnCallLabels(api),
		},
		Spec:   rich.Spec,
		Status: rich.Status,
	}, nil
}

// alertRichToEnvelope wraps the rich Alert into the typed envelope.
func alertRichToEnvelope(api *alertAPI, rich *oncalltypes.AlertRich, groupID, namespace string) (alertEnvelope, error) {
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
