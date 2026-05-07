// oncall_actions.go — vanguard implementation of the action-verb pattern
// (ADR § 7.1 / § 8). This file is the canonical home for shared types and
// helpers used by all bulk-capable action verbs on alert-groups (acknowledge,
// resolve, unresolve, silence, unsilence, unacknowledge); the remaining five
// verbs are mechanical clones of the acknowledge command defined here.
//
// Contract:
//   - stdout = exactly one MutationResult JSON document (or DetailedError).
//   - stderr = JSONL progress + diagnostic events in agent mode; dim plain
//     prefixed text in TTY mode.
//   - Single-target: exactly one positional <id>, exits cleanly on idempotent
//     no-op (changed:false).
//   - Bulk-by-filter: same filter flags as `alert-groups list`; required
//     confirmation prompt in TTY mode (skipped with --yes); agent mode
//     requires --yes explicitly when target count > 1 (footgun avoidance).
//   - Neither <id> NOR any filter flag → exit 2 with structured DetailedError.
package irm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// MutationResult envelope — emitted on stdout (single JSON document).
// ---------------------------------------------------------------------------

// MutationResult is the action-verb result envelope per ADR § 7.2 (refined by
// the vanguard lock — per-target outcome with structured errors instead of
// the simpler aggregate-only shape). Used by acknowledge / resolve /
// unresolve / silence / unsilence / unacknowledge.
type MutationResult struct {
	Action  string                 `json:"action"`
	Summary MutationSummary        `json:"summary"`
	Targets []MutationTargetResult `json:"targets"`
}

// MutationSummary is the aggregate roll-up of per-target outcomes.
//
//   - Matched: number of targets resolved by ID or filter.
//   - Changed: number of targets whose state actually changed
//     (idempotent no-ops are NOT counted; Changed ≤ Matched).
//   - Errors:  number of targets that failed.
type MutationSummary struct {
	Matched int `json:"matched"`
	Changed int `json:"changed"`
	Errors  int `json:"errors"`
}

// MutationTargetResult is the outcome of one target.
//
// Changed is false when the action was a no-op (e.g. acknowledge of an
// already-acknowledged group). Error is nil on success; populated on failure.
type MutationTargetResult struct {
	AlertGroupID string               `json:"alertGroupID"`
	Changed      bool                 `json:"changed"`
	Error        *MutationTargetError `json:"error"`
}

// MutationTargetError is a structured per-target error. Mirrors the
// DetailedError shape but scoped to a single target — global usage / config /
// auth errors continue to come through the DetailedError envelope on stdout.
type MutationTargetError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ---------------------------------------------------------------------------
// stderr-side: progress events + diagnostic class records (ADR § 8.2).
// ---------------------------------------------------------------------------

// actionProgressEvent is a per-target progress record emitted on stderr as
// JSONL in both TTY and agent modes (TTY mode also gets a dim plain-text
// mirror — callers use emitProgressLine, which handles the mode
// discrimination). The "action" prefix disambiguates from the throwaway
// `progressEvent` defined in spike_d1d3d6d7d8_demo.go.
type actionProgressEvent struct {
	Event  string            `json:"event"`
	Target actionProgressTgt `json:"target"`
}

type actionProgressTgt struct {
	AlertGroupID string `json:"alertGroupID"`
}

// actionDiagnosticEvent is a class:warning|note|hint record (ADR § 8.2). In
// agent mode it's emitted as a JSONL record on stderr; in TTY mode as a
// single `<class>: <summary>` line. Type prefix matches actionProgressEvent.
type actionDiagnosticEvent struct {
	Class   string `json:"class"`
	Summary string `json:"summary"`
	Command string `json:"command,omitempty"`
}

// emitProgressLine writes a per-target progress line to stderr. In agent mode:
// JSONL record. In TTY mode: a dim plain-text line ("→ Acknowledging X...").
func emitProgressLine(stderr io.Writer, verbPresent, alertGroupID, eventName string) {
	if agent.IsAgentMode() {
		ev := actionProgressEvent{Event: eventName, Target: actionProgressTgt{AlertGroupID: alertGroupID}}
		b, _ := json.Marshal(ev) //nolint:errcheck // stable struct
		fmt.Fprintln(stderr, string(b))
		return
	}
	fmt.Fprintf(stderr, "→ %s %s...\n", verbPresent, alertGroupID)
}

// emitHint writes a hint event to stderr in the form mandated by ADR § 8.2:
// agent mode → JSONL; TTY → "hint: <summary>: <command>".
func emitHint(stderr io.Writer, summary, command string) {
	if agent.IsAgentMode() {
		ev := actionDiagnosticEvent{Class: "hint", Summary: summary, Command: command}
		b, _ := json.Marshal(ev) //nolint:errcheck
		fmt.Fprintln(stderr, string(b))
		return
	}
	if command != "" {
		fmt.Fprintf(stderr, "hint: %s: %s\n", summary, command)
		return
	}
	fmt.Fprintf(stderr, "hint: %s\n", summary)
}

// ---------------------------------------------------------------------------
// Confirmation prompt — TTY only, when target count > 1 and --yes not set.
// ---------------------------------------------------------------------------

// confirmTTY prompts the user with `<message> [y/N]` and reads a single line
// from stdin. Returns true on a y/yes (case-insensitive) response. Does NOT
// branch on agent mode or --yes — the caller is responsible for that.
func confirmTTY(stdin io.Reader, stderr io.Writer, message string) (bool, error) {
	fmt.Fprintf(stderr, "%s [y/N] ", message)
	r := bufio.NewReader(stdin)
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

// ---------------------------------------------------------------------------
// Action verb opts — shared by all alert-group action verbs.
// ---------------------------------------------------------------------------

// alertGroupActionVerbOpts collects the flags shared by every bulk-capable
// alert-group action verb (acknowledge / resolve / unresolve / silence /
// unsilence / unacknowledge). Filter flags mirror `alert-groups list`
// exactly (ADR § 7.4).
type alertGroupActionVerbOpts struct {
	Yes bool

	// Bulk-by-filter flags — mirror alertGroupListOpts.
	MaxAge       string
	States       []string
	Teams        []string
	Integrations []string
	Mine         bool
	All          bool
}

func (o *alertGroupActionVerbOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Yes, "yes", false, "Skip the count-confirmation prompt; required in agent mode when target count > 1")
	flags.StringVar(&o.MaxAge, "max-age", "", "Filter: alert groups started within this duration (e.g. 1h, 24h, 7d)")
	flags.StringSliceVar(&o.States, "state", nil, "Filter: state (firing|acknowledged|resolved|silenced; repeatable)")
	flags.StringSliceVar(&o.Teams, "team", nil, "Filter: team PK (repeatable)")
	flags.StringSliceVar(&o.Integrations, "integration", nil, "Filter: integration PK (repeatable)")
	flags.BoolVar(&o.Mine, "mine", false, "Filter: limit to alert groups for the authenticated user")
	flags.BoolVar(&o.All, "all", false, "Bypass the default status and is_root filters")
}

// hasAnyFilter reports whether the user supplied at least one filter flag.
// Used to enforce the "id-or-filter required" guardrail.
func (o *alertGroupActionVerbOpts) hasAnyFilter() bool {
	return o.MaxAge != "" ||
		len(o.States) > 0 ||
		len(o.Teams) > 0 ||
		len(o.Integrations) > 0 ||
		o.Mine ||
		o.All
}

// ---------------------------------------------------------------------------
// Acknowledge command — vanguard implementation.
// ---------------------------------------------------------------------------

// newAcknowledgeCommand wires up `gcx irm oncall alert-groups acknowledge`
// per the locked vanguard contract. The remaining five action verbs
// (resolve / unresolve / silence / unsilence / unacknowledge) are
// mechanical clones of this command — see the action verb plan.
func newAcknowledgeCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupActionVerbOpts{}
	cmd := &cobra.Command{
		Use:   "acknowledge [<id>]",
		Short: "Acknowledge alert groups (single by ID, or bulk by filter).",
		Long: `Acknowledge alert groups.

Two forms are supported:

  • Single-target: pass a positional <id>.
  • Bulk-by-filter: omit the positional and pass one or more filter flags
    (--team, --state, --integration, --max-age, --mine, --all).

Bulk-by-filter prompts for confirmation in TTY mode when the matched count
exceeds 1; pass --yes to skip the prompt. Agent mode requires --yes
explicitly when count > 1 (auto-confirm of destructive bulk operations is
disabled by design).

Idempotent: re-acknowledging an already-acknowledged group reports
changed:false — not an error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAcknowledge(cmd, args, opts, loader)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runAcknowledge is the entry point for the acknowledge verb. Split out from
// newAcknowledgeCommand so the test suite can drive it directly with a fake
// OnCallAPI client (without going through Cobra's argv parser).
func runAcknowledge(cmd *cobra.Command, args []string, opts *alertGroupActionVerbOpts, loader OnCallConfigLoader) error {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()
	stdin := cmd.InOrStdin()

	// Guardrail: no <id> AND no filters → usage error.
	if len(args) == 0 && !opts.hasAnyFilter() {
		return missingIDOrFilterError()
	}

	// Mutually-exclusive: <id> + filters is ambiguous; reject.
	if len(args) == 1 && opts.hasAnyFilter() {
		return idAndFilterError()
	}

	client, _, err := loader.LoadOnCallClient(ctx)
	if err != nil {
		return err
	}

	// Resolve target list.
	targets, err := resolveAcknowledgeTargets(ctx, client, args, opts)
	if err != nil {
		return err
	}

	// Confirm if bulk + interactive.
	if len(targets) > 1 {
		if !opts.Yes {
			if agent.IsAgentMode() {
				return agentModeRequiresYesError(len(targets))
			}
			ok, cerr := confirmTTY(stdin, stderr, fmt.Sprintf("About to acknowledge %d alert groups. Continue?", len(targets)))
			if cerr != nil {
				return cerr
			}
			if !ok {
				return cancelledError()
			}
		}
	}

	// Execute per-target acknowledge with idempotency tracking.
	results := executeAcknowledge(ctx, client, targets, stderr)

	// Roll up summary.
	summary := MutationSummary{Matched: len(results)}
	for _, r := range results {
		if r.Error != nil {
			summary.Errors++
		} else if r.Changed {
			summary.Changed++
		}
	}

	envelope := MutationResult{
		Action:  "acknowledge",
		Summary: summary,
		Targets: results,
	}
	if err := writeMutationResult(stdout, envelope); err != nil {
		return err
	}

	// Post-result hint.
	emitAcknowledgeHints(stderr, results)

	// Exit code: 1 if any target failed. We've already written the result
	// envelope to stdout — returning an error from RunE would cause main.go
	// to write a second JSON document, breaking the "exactly one document on
	// stdout" contract. exitFuncForTesting allows tests to inject a fake.
	if summary.Errors > 0 {
		exitFuncForTesting(1)
	}
	return nil
}

// exitFuncForTesting is os.Exit by default. Tests override this to capture
// the exit code instead of terminating the test runner.
//
//nolint:gochecknoglobals
var exitFuncForTesting = os.Exit

// ---------------------------------------------------------------------------
// Target resolution — single-target vs bulk-by-filter.
// ---------------------------------------------------------------------------

// acknowledgeTarget is an internal carrier for an alert group plus its
// pre-action state (used for idempotency).
type acknowledgeTarget struct {
	ID    string
	State string // "firing", "acknowledged", "resolved", "silenced", or "" if unknown.
}

// resolveAcknowledgeTargets returns the deduplicated, sorted list of targets
// to operate on, plus their current state (when known). Single-target path
// does one GET; bulk path uses the list response.
func resolveAcknowledgeTargets(ctx context.Context, client OnCallAPI, args []string, opts *alertGroupActionVerbOpts) ([]acknowledgeTarget, error) {
	if len(args) == 1 {
		id := args[0]
		// Best-effort fetch state for idempotency. Failure to fetch state is
		// non-fatal — we proceed with state-unknown and let the POST be
		// authoritative.
		ag, err := client.GetAlertGroup(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("fetch alert group %q: %w", id, err)
		}
		return []acknowledgeTarget{{ID: id, State: alertGroupStatusString(ag)}}, nil
	}

	// Bulk-by-filter path: build filter set the same way `alert-groups list` does.
	filters, err := opts.toListFilters()
	if err != nil {
		return nil, err
	}

	oc, ok := client.(*OnCallClient)
	if !ok {
		return nil, errors.New("bulk-by-filter requires the OAuth plugin proxy (this context uses an SA token)")
	}

	// Bulk action targets aren't UI-truncated — pass limit=0 so the existing
	// hardCap is the only bound. The hint affordance is list-only.
	rawItems, _, err := listAlertGroupsRaw(ctx, oc, filters, 0)
	if err != nil {
		return nil, err
	}

	out := make([]acknowledgeTarget, 0, len(rawItems))
	seen := map[string]bool{}
	for _, item := range rawItems {
		api, _, derr := listAlertGroupRichFromBytes(item, nil)
		if derr != nil || api == nil || api.PK == "" {
			continue
		}
		if seen[api.PK] {
			continue
		}
		seen[api.PK] = true
		out = append(out, acknowledgeTarget{
			ID:    api.PK,
			State: decodeAlertGroupState(api.Status),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// toListFilters re-uses the same filter-resolution logic as `alert-groups list`
// (resolveAlertGroupListFilters expects a *cobra.Command for the "did the user
// pass --state explicitly" check, so we replicate just the bits we need here).
func (o *alertGroupActionVerbOpts) toListFilters() (alertGroupListFilters, error) {
	out := alertGroupListFilters{
		MaxAge:       o.MaxAge,
		Teams:        o.Teams,
		Integrations: o.Integrations,
		Mine:         o.Mine,
	}

	if len(o.States) > 0 {
		for _, name := range o.States {
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

	if !o.All {
		// Default status filter when the user didn't override.
		if len(out.Statuses) == 0 {
			out.Statuses = []int{0, 1, 3}
		}
		// is_root=true so we don't operate on child groups.
		t := true
		out.IsRoot = &t
	}

	return out, nil
}

// alertGroupStatusString decodes the public-API AlertGroup.Status (typed as
// any) into the rich-shape state string. Mirrors decodeAlertGroupState which
// takes a *int — the public type uses any because JSON unmarshal yields
// float64 by default.
func alertGroupStatusString(ag *AlertGroup) string {
	if ag == nil {
		return ""
	}
	switch n := ag.Status.(type) {
	case float64:
		s := int(n)
		return decodeAlertGroupState(&s)
	case int:
		return decodeAlertGroupState(&n)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Per-target execution.
// ---------------------------------------------------------------------------

// executeAcknowledge applies the acknowledge action to each target with
// idempotent change detection. Errors are captured per-target and reported in
// the MutationResult; they do NOT short-circuit the loop.
func executeAcknowledge(ctx context.Context, client OnCallAPI, targets []acknowledgeTarget, stderr io.Writer) []MutationTargetResult {
	out := make([]MutationTargetResult, 0, len(targets))
	for _, tgt := range targets {
		// Idempotent no-op: state already "acknowledged" → skip POST.
		if tgt.State == "acknowledged" {
			emitProgressLine(stderr, "Already acknowledged", tgt.ID, "noop")
			out = append(out, MutationTargetResult{AlertGroupID: tgt.ID, Changed: false})
			continue
		}

		emitProgressLine(stderr, "Acknowledging", tgt.ID, "acknowledging")
		err := client.AcknowledgeAlertGroup(ctx, tgt.ID)
		if err != nil {
			out = append(out, MutationTargetResult{
				AlertGroupID: tgt.ID,
				Changed:      false,
				Error: &MutationTargetError{
					Code:       "acknowledge_failed",
					Message:    err.Error(),
					Suggestion: fmt.Sprintf("verify the alert group exists: gcx irm oncall alert-groups get %s", tgt.ID),
				},
			})
			continue
		}
		emitProgressLine(stderr, "Acknowledged", tgt.ID, "acknowledged")
		out = append(out, MutationTargetResult{AlertGroupID: tgt.ID, Changed: true})
	}
	return out
}

// ---------------------------------------------------------------------------
// Output helpers.
// ---------------------------------------------------------------------------

// writeMutationResult marshals the envelope and writes it to stdout as a
// single JSON document terminated by a newline.
func writeMutationResult(stdout io.Writer, env MutationResult) error {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mutation result: %w", err)
	}
	if _, err := fmt.Fprintln(stdout, string(data)); err != nil {
		return err
	}
	return nil
}

// emitAcknowledgeHints emits exactly one post-result hint per target on the
// success / idempotent path: how to view live state. On errors-only no hint
// is emitted (errors carry their own per-target suggestions; ADR § 8.4).
func emitAcknowledgeHints(stderr io.Writer, results []MutationTargetResult) {
	// Pick a representative target for the hint: prefer a successfully-changed
	// one, fall back to any with no error.
	var pivot string
	for _, r := range results {
		if r.Error == nil && r.Changed {
			pivot = r.AlertGroupID
			break
		}
	}
	if pivot == "" {
		for _, r := range results {
			if r.Error == nil {
				pivot = r.AlertGroupID
				break
			}
		}
	}
	if pivot == "" {
		return
	}
	emitHint(stderr, "See live alerts", "gcx irm oncall alert-groups get "+pivot)
}

// ---------------------------------------------------------------------------
// Error builders — DetailedError envelopes for the guardrails.
// ---------------------------------------------------------------------------

func missingIDOrFilterError() error {
	exit2 := 2
	return fail.DetailedError{
		Summary:  "<id> argument or filter flag required",
		Details:  "Bulk action verbs require either a positional <id> or one or more filter flags to scope the operation. Acting on every alert group is not supported.",
		ExitCode: &exit2,
		Suggestions: []string{
			"Pass an alert-group ID: gcx irm oncall alert-groups acknowledge <id>",
			"Filter by team: gcx irm oncall alert-groups acknowledge --team <name> --yes",
			"Filter by status + age: gcx irm oncall alert-groups acknowledge --state firing --max-age 24h --yes",
		},
	}
}

func idAndFilterError() error {
	exit2 := 2
	return fail.DetailedError{
		Summary:  "<id> argument and filter flags are mutually exclusive",
		Details:  "Pass either a positional <id> for single-target mode OR filter flags for bulk-by-filter mode, but not both.",
		ExitCode: &exit2,
		Suggestions: []string{
			"Drop the filter flags to act on the single ID",
			"Drop the positional argument to act on the filtered set",
		},
	}
}

func agentModeRequiresYesError(count int) error {
	exit2 := 2
	return fail.DetailedError{
		Summary:  "agent mode requires --yes when target count > 1",
		Details:  fmt.Sprintf("Matched %d alert groups. Bulk action verbs require an explicit --yes in agent mode to avoid auto-confirming destructive operations.", count),
		ExitCode: &exit2,
		Suggestions: []string{
			"Re-run with --yes if the filter set is correct",
			"Narrow the filter to confirm the intended target set first",
		},
	}
}

func cancelledError() error {
	exit2 := 2 // Treat user cancellation as a usage-style exit (intentional non-action).
	return fail.DetailedError{
		Summary:  "operation cancelled by user",
		Details:  "Confirmation prompt was declined; no targets were modified.",
		ExitCode: &exit2,
	}
}
