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
//     confirmation prompt in TTY mode (skipped with --force); agent mode
//     requires --force explicitly when target count > 1 (footgun avoidance).
//   - Neither <id> NOR any filter flag → exit 2 with structured DetailedError.
//
// Result-shape contract (locked, two-shape):
//
//	Single-target (positional <id> form):
//	  {
//	    "action":  "acknowledge",
//	    "target":  {"alertGroupId": "I..."},
//	    "changed": true | false              // false = idempotent no-op
//	  }
//	Single-target failure path (Option A — stays single-shape):
//	  {
//	    "action":  "acknowledge",
//	    "target":  {"alertGroupId": "I..."},
//	    "error":   {"code", "message", "suggestion"}
//	  }
//
//	Bulk-by-filter (--filter form):
//	  {
//	    "action":  "acknowledge",
//	    "summary": {"matched", "succeeded", "skipped", "failed"},
//	    "failures": [{"target": {"alertGroupId"}, "error": {...}}]
//	  }
//
// The two shapes are mutually exclusive — single-target invocations never
// emit summary/failures; bulk invocations never emit top-level
// target/changed. `fields[]` (declarative-config writes) is
// OMITTED — ack is a state-machine verb.
package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// Result envelopes — emitted on stdout (single JSON document).
//
// Two distinct shapes, dispatched by single-vs-bulk invocation. See the file
// header for the locked contract.
// ---------------------------------------------------------------------------

// irmTarget is the scalar target descriptor used by both shapes. For
// alert-group action verbs only `alertGroupId` is populated; future
// IRM-domain verbs (e.g. silence, escalation-chain) may extend this with
// additional `omitempty` fields without breaking parsers.
type irmTarget struct {
	AlertGroupID string `json:"alertGroupId,omitempty"`
}

// singleMutationResult is the single-target envelope (scalar
// Target + top-level Changed). On success/idempotent the `error` field is
// nil-omitted; on failure `changed` is omitted (zero value with omitempty)
// and `error` is populated. `fields[]` is intentionally absent — ack is a
// state-machine verb, not a declarative-config write.
type singleMutationResult struct {
	Action  string               `json:"action"`
	Target  irmTarget            `json:"target"`
	Changed *bool                `json:"changed,omitempty"`
	Error   *mutationTargetError `json:"error,omitempty"`
}

// bulkMutationResult is the bulk-by-filter envelope: counts for
// successes/skips, enumerated entries only for failures. Both
// `summary` and `failures` are always present; `failures` is `[]` (not
// omitempty) when no targets failed, for predictable agent-side parsing.
type bulkMutationResult struct {
	Action   string            `json:"action"`
	Summary  mutationSummary   `json:"summary"`
	Failures []mutationFailure `json:"failures"`
}

// mutationSummary aggregates per-target outcomes.
//
// Invariant: matched == succeeded + skipped + failed (always, by construction).
//   - matched:   targets resolved by filter (or 1 for single-target).
//   - succeeded: state actually changed this run.
//   - skipped:   already in target state (idempotent no-op).
//   - failed:    API call errored.
type mutationSummary struct {
	Matched   int `json:"matched"`
	Succeeded int `json:"succeeded"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

// mutationFailure is one entry in bulkMutationResult.Failures — the target
// whose action failed and the structured error explaining why. Successes and
// skips are NOT enumerated — only the count is reported.
type mutationFailure struct {
	Target irmTarget           `json:"target"`
	Error  mutationTargetError `json:"error"`
}

// mutationTargetError is a structured per-target error. Mirrors the
// DetailedError shape but scoped to a single target — global usage / config /
// auth errors continue to come through the DetailedError envelope on stdout.
type mutationTargetError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// boolPtr returns a pointer to b. Used to populate singleMutationResult.Changed
// with a non-omitempty true/false (so that {…,"changed":false} is preserved
// for the idempotent-noop path, and the field is omitted only on the failure
// path where Error is set).
//
//nolint:modernize // &b is clearer than new(bool)+assign at call sites; the named helper documents intent.
func boolPtr(b bool) *bool { return &b }

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

// emitProgressLine writes a per-target progress line to stderr. In agent mode:
// JSONL record. In TTY mode: a dim plain-text line ("→ Acknowledging X...").
func emitProgressLine(stderr io.Writer, verbPresent, alertGroupID, eventName string) {
	if agent.IsAgentMode() {
		ev := actionProgressEvent{Event: eventName, Target: actionProgressTgt{AlertGroupID: alertGroupID}}
		b, _ := json.Marshal(ev) //nolint:errchkjson // stable struct; errchkjson wants checked error from json.Marshal
		fmt.Fprintln(stderr, string(b))
		return
	}
	fmt.Fprintf(stderr, "→ %s %s...\n", verbPresent, alertGroupID)
}

// emitHint writes a hint event to stderr in the form mandated by ADR § 8.2:
// agent mode → JSONL; TTY → "hint: <summary>: <command>".
// Delegates to output.EmitHint — package-private alias kept for call-site
// brevity within the irm package and backward compat with white-box tests.
func emitHint(stderr io.Writer, summary, command string) {
	cmdio.EmitHint(stderr, summary, command)
}

// emitWarn writes a warn-class diagnostic to stderr. In agent mode the
// record is JSONL with `class:"warning"`; in TTY mode the line is rendered
// as `warn: <summary>`.
// Delegates to output.EmitWarn — package-private alias kept for call-site
// brevity and backward compat with white-box tests.
func emitWarn(stderr io.Writer, summary string) {
	cmdio.EmitWarn(stderr, summary)
}

// emitNote writes a note-class diagnostic to stderr. In agent mode the
// record is JSONL with `class:"note"`; in TTY mode the line is rendered
// as `note: <summary>`.
// Delegates to output.EmitNote — package-private alias kept for call-site
// brevity and backward compat with white-box tests.
func emitNote(stderr io.Writer, summary string) {
	cmdio.EmitNote(stderr, summary)
}

// emitTTYSummary writes a one-line TTY-only human summary to stderr after
// the JSON result has been written to stdout. No-op in agent mode (the
// JSONL progress + JSON result envelope already cover the agent surface).
func emitTTYSummary(stderr io.Writer, line string) {
	if agent.IsAgentMode() {
		return
	}
	fmt.Fprintln(stderr, line)
}

// ---------------------------------------------------------------------------
// Action verb opts — shared by all alert-group action verbs.
// ---------------------------------------------------------------------------

// alertGroupActionVerbOpts collects the flags shared by every bulk-capable
// alert-group action verb (acknowledge / resolve / unresolve / silence /
// unsilence / unacknowledge). Filter flags mirror `alert-groups list`
// exactly (ADR § 7.4).
type alertGroupActionVerbOpts struct {
	// IO routes mutation-result output through the codec system
	// (CONSTITUTION: all output goes through codecs). The default is JSON to
	// preserve the locked MutationResult on-the-wire contract; -o yaml and
	// -o text are also available via the registered custom codecs.
	IO cmdio.Options

	Force bool

	// Bulk-by-filter flags — mirror alertGroupListOpts.
	MaxAge       string
	States       []string
	Teams        []string
	Integrations []string
	Mine         bool
	All          bool
}

func (o *alertGroupActionVerbOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip the count-confirmation prompt and proceed without interactive confirmation")
	flags.StringVar(&o.MaxAge, "max-age", "", "Filter: alert groups started within this duration (e.g. 1h, 24h, 7d)")
	flags.StringSliceVar(&o.States, "state", nil, "Filter: state (firing|acknowledged|resolved|silenced; repeatable)")
	flags.StringSliceVar(&o.Teams, "team", nil, "Filter: team PK (repeatable)")
	flags.StringSliceVar(&o.Integrations, "integration", nil, "Filter: integration PK (repeatable)")
	flags.BoolVar(&o.Mine, "mine", false, "Filter: limit to alert groups for the authenticated user")
	flags.BoolVar(&o.All, "all", false, "Bypass the default status and is_root filters")

	// Wire the codec system. Text is the default for human/TTY mode; agent
	// mode overrides to "agents" inside BindFlags so the locked MutationResult
	// contract (a single structured document on stdout) is preserved for
	// parsers. yaml uses the stable-key-order codec; text is a one-line
	// human-readable summary covering both single- and bulk-shape envelopes
	// via a type switch.
	o.IO.RegisterCustomCodec("text", &mutationTextCodec{})
	o.IO.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
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

// Validate satisfies the constitutional invariant that every opts struct
// expose a Validate() method. The shape-level guardrails (no-args-no-filters,
// id+filters mutual exclusion) live in runActionVerb where they have access
// to the positional argv; Validate stays a no-op until field-level checks
// emerge.
func (o *alertGroupActionVerbOpts) Validate() error { return nil }

// ---------------------------------------------------------------------------
// Parameterized action-verb runner. The acknowledge vanguard was the first
// verb wired to the locked MutationResult contract; the remaining six (
// resolve / unresolve / unacknowledge / silence / unsilence / delete) share
// the same shape and differ only on (a) which API call is made and (b) the
// terminal-state predicate used to detect idempotent no-ops.
// ---------------------------------------------------------------------------

// verbConfig parameterises the action-verb runner with the verb-specific bits.
// Construct one per command; the runner stays generic.
type verbConfig struct {
	// Name is the canonical verb name as it appears on the command line and
	// in the MutationResult envelope's `action` field (e.g. "acknowledge").
	Name string

	// PresentGerund is the present-progressive verb used in TTY progress
	// lines (e.g. "Acknowledging", "Resolving"). The noop / done forms are
	// derived from PastTense and NoopHuman below.
	PresentGerund string

	// PastTense is the past-tense verb used in TTY one-liners and in the
	// "Already X" idempotent skip message (e.g. "acknowledged").
	PastTense string

	// NoopHuman is the human-facing summary fragment used in the bulk TTY
	// summary line (e.g. "already-acked"). May be the empty string for
	// destructive verbs (delete) that have no idempotent state.
	NoopHuman string

	// ErrorCode is the typed error code emitted in mutationTargetError.Code
	// on per-target failure (e.g. "acknowledge_failed"). Stable for parsers.
	ErrorCode string

	// HintCommand is the post-result hint command template surfaced on
	// stderr after a successful single-target or first-success bulk run.
	// `%s` is replaced with the alert-group ID. May be the empty string to
	// suppress the hint (e.g. delete — the target no longer exists).
	HintCommandTemplate string

	// HintSummary is the human-facing summary attached to the post-result
	// hint (e.g. "See live alerts"). Ignored when HintCommandTemplate is empty.
	HintSummary string

	// TargetState is the state name this verb tries to put the target into,
	// used for idempotent no-op detection. Empty (destructive verbs such as
	// delete) skips the check and always runs the API call.
	TargetState string

	// APICall executes the verb against one target. The closure captures any
	// extra flags (e.g. --duration on silence) by reference at command-
	// construction time.
	APICall func(ctx context.Context, c OnCallAPI, id string) error
}

// ---------------------------------------------------------------------------
// Action-verb command wrappers.
// ---------------------------------------------------------------------------

// acknowledgeVerb is the verbConfig for the acknowledge vanguard. The other
// verbs follow the same shape (see below).
func acknowledgeVerb() verbConfig {
	return verbConfig{
		Name:                "acknowledge",
		PresentGerund:       "Acknowledging",
		PastTense:           "acknowledged",
		NoopHuman:           "already-acked",
		ErrorCode:           "acknowledge_failed",
		HintCommandTemplate: "gcx irm oncall alert-groups get %s",
		HintSummary:         "See live alerts",
		TargetState:         "acknowledged",
		APICall: func(ctx context.Context, c OnCallAPI, id string) error {
			return c.AcknowledgeAlertGroup(ctx, id)
		},
	}
}

// resolveVerb closes the alert group (state → resolved).
func resolveVerb() verbConfig {
	return verbConfig{
		Name:                "resolve",
		PresentGerund:       "Resolving",
		PastTense:           "resolved",
		NoopHuman:           "already-resolved",
		ErrorCode:           "resolve_failed",
		HintCommandTemplate: "gcx irm oncall alert-groups get %s",
		HintSummary:         "Inspect the resolved group",
		TargetState:         "resolved",
		APICall: func(ctx context.Context, c OnCallAPI, id string) error {
			return c.ResolveAlertGroup(ctx, id)
		},
	}
}

// unacknowledgeVerb returns the group to firing from acknowledged.
func unacknowledgeVerb() verbConfig {
	return verbConfig{
		Name:                "unacknowledge",
		PresentGerund:       "Unacknowledging",
		PastTense:           "unacknowledged",
		NoopHuman:           "already-firing",
		ErrorCode:           "unacknowledge_failed",
		HintCommandTemplate: "gcx irm oncall alert-groups get %s",
		HintSummary:         "See current state",
		// Target state: firing (unacknowledge moves out of acknowledged).
		TargetState: "firing",
		APICall: func(ctx context.Context, c OnCallAPI, id string) error {
			return c.UnacknowledgeAlertGroup(ctx, id)
		},
	}
}

// unresolveVerb returns the group to firing from resolved.
func unresolveVerb() verbConfig {
	return verbConfig{
		Name:                "unresolve",
		PresentGerund:       "Unresolving",
		PastTense:           "unresolved",
		NoopHuman:           "already-firing",
		ErrorCode:           "unresolve_failed",
		HintCommandTemplate: "gcx irm oncall alert-groups get %s",
		HintSummary:         "See current state",
		TargetState:         "firing",
		APICall: func(ctx context.Context, c OnCallAPI, id string) error {
			return c.UnresolveAlertGroup(ctx, id)
		},
	}
}

// unsilenceVerb removes the silence (state → firing).
func unsilenceVerb() verbConfig {
	return verbConfig{
		Name:                "unsilence",
		PresentGerund:       "Unsilencing",
		PastTense:           "unsilenced",
		NoopHuman:           "already-firing",
		ErrorCode:           "unsilence_failed",
		HintCommandTemplate: "gcx irm oncall alert-groups get %s",
		HintSummary:         "See current state",
		TargetState:         "firing",
		APICall: func(ctx context.Context, c OnCallAPI, id string) error {
			return c.UnsilenceAlertGroup(ctx, id)
		},
	}
}

// silenceVerb is built per-invocation because it captures the --duration flag
// (the closure binds the int by reference; Cobra parses --duration before the
// RunE fires, so the captured value is correct at call time).
func silenceVerb(duration *int) verbConfig {
	return verbConfig{
		Name:                "silence",
		PresentGerund:       "Silencing",
		PastTense:           "silenced",
		NoopHuman:           "already-silenced",
		ErrorCode:           "silence_failed",
		HintCommandTemplate: "gcx irm oncall alert-groups get %s",
		HintSummary:         "Confirm the silence",
		TargetState:         "silenced",
		APICall: func(ctx context.Context, c OnCallAPI, id string) error {
			return c.SilenceAlertGroup(ctx, id, *duration)
		},
	}
}

// deleteVerb is destructive: it removes the alert group entirely. There is no
// terminal-state idempotency (TargetState=""); every successful API call is
// reported as `changed:true`. The post-result hint is suppressed because the
// target ID is no longer a valid input.
func deleteVerb() verbConfig {
	return verbConfig{
		Name:                "delete",
		PresentGerund:       "Deleting",
		PastTense:           "deleted",
		NoopHuman:           "", // destructive verbs have no idempotent state
		ErrorCode:           "delete_failed",
		HintCommandTemplate: "",
		HintSummary:         "",
		TargetState:         "", // destructive — always changed
		APICall: func(ctx context.Context, c OnCallAPI, id string) error {
			return c.DeleteAlertGroup(ctx, id)
		},
	}
}

// newAcknowledgeCommand wires up `gcx irm oncall alert-groups acknowledge`
// per the locked vanguard contract.
func newAcknowledgeCommand(loader OnCallConfigLoader) *cobra.Command {
	return newActionVerbCommand(loader, acknowledgeVerb(), nil)
}

// newResolveCommand wires `alert-groups resolve` against the resolve verb config.
func newResolveCommand(loader OnCallConfigLoader) *cobra.Command {
	return newActionVerbCommand(loader, resolveVerb(), nil)
}

// newUnacknowledgeCommand wires `alert-groups unacknowledge`.
func newUnacknowledgeCommand(loader OnCallConfigLoader) *cobra.Command {
	return newActionVerbCommand(loader, unacknowledgeVerb(), nil)
}

// newUnresolveCommand wires `alert-groups unresolve`.
func newUnresolveCommand(loader OnCallConfigLoader) *cobra.Command {
	return newActionVerbCommand(loader, unresolveVerb(), nil)
}

// newUnsilenceCommand wires `alert-groups unsilence`.
func newUnsilenceCommand(loader OnCallConfigLoader) *cobra.Command {
	return newActionVerbCommand(loader, unsilenceVerb(), nil)
}

// newSilenceCommand wires `alert-groups silence` — special-cased because of
// the `--duration` flag (no other action verb takes a verb-specific flag).
func newSilenceCommand(loader OnCallConfigLoader) *cobra.Command {
	var duration int
	cfg := silenceVerb(&duration)
	cmd := newActionVerbCommand(loader, cfg, func(flags *pflag.FlagSet) {
		flags.IntVar(&duration, "duration", 3600, "Silence duration in seconds")
	})
	return cmd
}

// newDeleteCommand wires `alert-groups delete`.
func newDeleteCommand(loader OnCallConfigLoader) *cobra.Command {
	return newActionVerbCommand(loader, deleteVerb(), nil)
}

// newActionVerbCommand builds the Cobra command for any alert-group action
// verb. The shared single-target + bulk-by-filter plumbing lives in
// runActionVerb; extraFlags optionally binds verb-specific flags (e.g.
// --duration on silence) onto the Cobra flag set.
func newActionVerbCommand(loader OnCallConfigLoader, cfg verbConfig, extraFlags func(*pflag.FlagSet)) *cobra.Command {
	opts := &alertGroupActionVerbOpts{}
	cmd := &cobra.Command{
		Use:   cfg.Name + " [<id>]",
		Short: actionVerbShort(cfg),
		Long:  actionVerbLong(cfg),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runActionVerb(cmd, args, opts, loader, cfg)
		},
	}
	opts.setup(cmd.Flags())
	if extraFlags != nil {
		extraFlags(cmd.Flags())
	}
	return cmd
}

// actionVerbShort returns the one-line cobra Short help text for a verb.
func actionVerbShort(cfg verbConfig) string {
	return capitalize(cfg.Name) + " alert groups (single by ID, or bulk by filter)."
}

// actionVerbLong returns the multi-line cobra Long help text shared by every
// action verb. The body is parameterised by the verb name + past tense; the
// rest of the text (single/bulk form, --force semantics, idempotency) is
// invariant across verbs.
func actionVerbLong(cfg verbConfig) string {
	// Destructive verbs (delete) skip the idempotency paragraph because
	// there is no terminal state to detect.
	if cfg.TargetState == "" {
		return fmt.Sprintf(`%s alert groups.

Two forms are supported:

  - Single-target: pass a positional <id>.
  - Bulk-by-filter: omit the positional and pass one or more filter flags
    (--team, --state, --integration, --max-age, --mine, --all).

Bulk-by-filter prompts for confirmation in TTY mode when the matched count
exceeds 1; pass --force to skip the prompt. Agent mode requires --force
explicitly when count > 1 (auto-confirm of destructive bulk operations is
disabled by design).

Destructive: %s removes the alert group; there is no idempotent skip path.`,
			capitalize(cfg.Name), cfg.Name)
	}
	return fmt.Sprintf(`%s alert groups.

Two forms are supported:

  - Single-target: pass a positional <id>.
  - Bulk-by-filter: omit the positional and pass one or more filter flags
    (--team, --state, --integration, --max-age, --mine, --all).

Bulk-by-filter prompts for confirmation in TTY mode when the matched count
exceeds 1; pass --force to skip the prompt. Agent mode requires --force
explicitly when count > 1 (auto-confirm of destructive bulk operations is
disabled by design).

Idempotent: re-running on an already-%s group reports changed:false
(single-target) or summary.skipped++ (bulk) — not an error.`,
		capitalize(cfg.Name), cfg.PastTense)
}

// capitalize returns s with the first rune uppercased. ASCII-only; used for
// command help-text composition where the verb names are stable ASCII.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// runActionVerb is the entry point for any alert-group action verb. Split
// out from newActionVerbCommand so the test suite can drive it directly with
// a fake OnCallAPI client (without going through Cobra's argv parser).
func runActionVerb(cmd *cobra.Command, args []string, opts *alertGroupActionVerbOpts, loader OnCallConfigLoader, cfg verbConfig) error {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()
	stdin := cmd.InOrStdin()

	if err := opts.Validate(); err != nil {
		return err
	}

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

	// Dispatch on shape: single-target (positional <id>) vs bulk-by-filter.
	if len(args) == 1 {
		return runActionVerbSingle(ctx, client, args[0], stdout, stderr, cfg, &opts.IO)
	}
	return runActionVerbBulk(ctx, client, opts, stdin, stdout, stderr, cfg, &opts.IO)
}

// runActionVerbSingle executes the single-target flow and emits the
// single-shape envelope on stdout. Failure path stays single-shape: top-level
// `error` replaces `changed`.
func runActionVerbSingle(ctx context.Context, client OnCallAPI, id string, stdout, stderr io.Writer, cfg verbConfig, ioOpts *cmdio.Options) error {
	tgt, err := resolveSingleTarget(ctx, client, id)
	if err != nil {
		return err
	}

	result := executeActionVerbOne(ctx, client, tgt, stderr, cfg)

	// Build single-shape envelope.
	env := singleMutationResult{
		Action: cfg.Name,
		Target: irmTarget{AlertGroupID: result.id},
	}
	if result.err != nil {
		env.Error = result.err
	} else {
		env.Changed = boolPtr(result.changed) //nolint:modernize
	}

	if werr := ioOpts.Encode(stdout, env); werr != nil {
		return werr
	}

	// TTY-only one-liner + post-result hint on success.
	// EmittedError on failure: stdout already carries the JSON envelope with
	// the structured error, so the sentinel makes reportError exit with the
	// partial-failure code without rendering a second error document —
	// while keeping the normal exit path (telemetry, PersistentPostRun).
	if result.err != nil {
		emitTTYSummary(stderr, fmt.Sprintf("%s %q: failed (%s)", cfg.Name, result.id, result.err.Code))
		return fail.NewEmittedError(fail.ExitPartialFailure,
			fail.NewPartialFailureError(cfg.Name, 1, 1))
	}

	if result.changed {
		emitTTYSummary(stderr, fmt.Sprintf("%s %q: done", cfg.Name, result.id))
	} else {
		emitTTYSummary(stderr, fmt.Sprintf("%s %q: no changes", cfg.Name, result.id))
	}
	if cfg.HintCommandTemplate != "" {
		emitHint(stderr, cfg.HintSummary, fmt.Sprintf(cfg.HintCommandTemplate, result.id))
	}
	return nil
}

// confirmBulkAction enforces the bulk-operation confirmation gate when the
// matched count exceeds 1. Returns nil when the user consents or count ≤ 1.
// Separated from runActionVerbBulk to keep nesting depth in the caller at
// the accepted complexity threshold.
//
// Delegates to providers.ConfirmDestructive, which centralises the bypass
// chain (--force, GCX_AUTO_APPROVE, agent-mode guard) and the [y/N] prompt.
// Agent-mode rejection is re-wrapped into a typed DetailedError so the user
// gets actionable suggestions and the documented exit-2 usage-error code.
func confirmBulkAction(count int, force bool, verb string, stdin io.Reader, stderr io.Writer) error {
	if count <= 1 {
		return nil
	}
	prompt := fmt.Sprintf("About to %s %d alert groups. Continue?", verb, count)
	ok, err := providers.ConfirmDestructive(stdin, stderr, force, prompt)
	if err != nil {
		if errors.Is(err, providers.ErrAgentModeRequiresForce) {
			return agentModeRequiresForceError(count)
		}
		return err
	}
	if !ok {
		return cancelledError()
	}
	return nil
}

// buildBulkMutationResult rolls up per-target outcomes into the bulk
// envelope. Extracted from runActionVerbBulk to keep the loop's nesting
// depth within the accepted nestif threshold.
//
// Invariant: summary.Matched == Succeeded + Skipped + Failed (by construction).
// failures is always a non-nil slice ([] rather than null) for predictable
// agent-side parsing.
func buildBulkMutationResult(action string, outcomes []ackOutcome) bulkMutationResult {
	summary := mutationSummary{Matched: len(outcomes)}
	failures := []mutationFailure{} // never nil — predictable parsing.
	for _, r := range outcomes {
		switch {
		case r.err != nil:
			summary.Failed++
			failures = append(failures, mutationFailure{
				Target: irmTarget{AlertGroupID: r.id},
				Error:  *r.err,
			})
		case r.changed:
			summary.Succeeded++
		default:
			summary.Skipped++
		}
	}
	return bulkMutationResult{
		Action:   action,
		Summary:  summary,
		Failures: failures,
	}
}

// runActionVerbBulk executes the bulk-by-filter flow and emits the
// bulk-shape envelope on stdout.
func runActionVerbBulk(ctx context.Context, client OnCallAPI, opts *alertGroupActionVerbOpts, stdin io.Reader, stdout, stderr io.Writer, cfg verbConfig, ioOpts *cmdio.Options) error {
	targets, err := resolveBulkTargets(ctx, client, opts)
	if err != nil {
		return err
	}

	// Confirm if the matched set exceeds 1. Extracted to a helper to reduce
	// nesting depth (nestif).
	if err := confirmBulkAction(len(targets), opts.Force, cfg.Name, stdin, stderr); err != nil {
		return err
	}

	// Sequential to avoid thundering-herd on OnCall during broad bulk acks.
	results := make([]ackOutcome, 0, len(targets))
	for _, tgt := range targets {
		results = append(results, executeActionVerbOne(ctx, client, tgt, stderr, cfg))
	}

	env := buildBulkMutationResult(cfg.Name, results)
	if werr := ioOpts.Encode(stdout, env); werr != nil {
		return werr
	}

	// TTY-only one-liner. For destructive verbs (NoopHuman=""), skip the
	// idempotent-skip count fragment — there is no terminal-state skip path.
	if cfg.NoopHuman != "" {
		emitTTYSummary(stderr, fmt.Sprintf(
			"%s: %d/%d succeeded (%d %s, %d failed)",
			cfg.Name, env.Summary.Succeeded, env.Summary.Matched, env.Summary.Skipped, cfg.NoopHuman, env.Summary.Failed,
		))
	} else {
		emitTTYSummary(stderr, fmt.Sprintf(
			"%s: %d/%d succeeded (%d failed)",
			cfg.Name, env.Summary.Succeeded, env.Summary.Matched, env.Summary.Failed,
		))
	}

	// Post-result hint — pivot on the first non-failed target. Suppressed
	// for destructive verbs where the target ID is no longer valid.
	if cfg.HintCommandTemplate != "" {
		for _, r := range results {
			if r.err == nil {
				emitHint(stderr, cfg.HintSummary, fmt.Sprintf(cfg.HintCommandTemplate, r.id))
				break
			}
		}
	}

	// EmittedError when any target failed: stdout already carries the JSON
	// envelope with the full failures list, so the sentinel makes reportError
	// exit with the partial-failure code without rendering a second error
	// document — while keeping the normal exit path (telemetry,
	// PersistentPostRun) that a direct os.Exit would bypass.
	if env.Summary.Failed > 0 {
		return fail.NewEmittedError(fail.ExitPartialFailure,
			fail.NewPartialFailureError(cfg.Name, env.Summary.Matched, env.Summary.Failed))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Target resolution — single-target vs bulk-by-filter.
// ---------------------------------------------------------------------------

// acknowledgeTarget is an internal carrier for an alert group plus its
// pre-action state (used for idempotency).
type acknowledgeTarget struct {
	ID    string
	State string // "firing", "acknowledged", "resolved", "silenced", or "" if unknown.
}

// resolveSingleTarget fetches the current state of one alert group for
// idempotency-aware execution.
func resolveSingleTarget(ctx context.Context, client OnCallAPI, id string) (acknowledgeTarget, error) {
	ag, err := client.GetAlertGroup(ctx, id)
	if err != nil {
		return acknowledgeTarget{}, fmt.Errorf("fetch alert group %q: %w", id, err)
	}
	return acknowledgeTarget{ID: id, State: alertGroupStatusString(ag)}, nil
}

// resolveBulkTargets returns the deduplicated, sorted list of targets to
// operate on (bulk-by-filter path), plus their current state.
func resolveBulkTargets(ctx context.Context, client OnCallAPI, opts *alertGroupActionVerbOpts) ([]acknowledgeTarget, error) {
	filters, err := opts.toListFilters()
	if err != nil {
		return nil, err
	}

	reader, ok := client.(RichAlertGroupReader)
	if !ok {
		return nil, errors.New("bulk-by-filter requires the OAuth plugin proxy; SA-token mode does not support rich list operations")
	}

	// Bulk action targets aren't UI-truncated — pass limit=0 so the existing
	// hardCap is the only bound. The hint affordance is list-only.
	rawItems, _, err := reader.ListAlertGroupsRaw(ctx, filters, 0)
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

// ackOutcome is the internal per-target outcome: either changed, idempotent
// (changed=false, err=nil), or failed (err != nil).
type ackOutcome struct {
	id      string
	changed bool
	err     *mutationTargetError
}

// executeActionVerbOne applies the action verb to one target with idempotent
// change detection when the verb has a TargetState. For destructive verbs
// (TargetState=""), the idempotency check is skipped and every successful
// API call is reported as changed:true.
func executeActionVerbOne(ctx context.Context, client OnCallAPI, tgt acknowledgeTarget, stderr io.Writer, cfg verbConfig) ackOutcome {
	// Idempotent no-op: target already in the verb's target state.
	if cfg.TargetState != "" && tgt.State == cfg.TargetState {
		emitProgressLine(stderr, "Already "+cfg.PastTense, tgt.ID, "noop")
		return ackOutcome{id: tgt.ID, changed: false}
	}

	emitProgressLine(stderr, cfg.PresentGerund, tgt.ID, strings.ToLower(cfg.PresentGerund))
	if err := cfg.APICall(ctx, client, tgt.ID); err != nil {
		// Destructive verbs suppress the "alert-groups get" suggestion
		// because the group is being removed; for state-machine verbs the
		// suggestion is a useful pivot to inspect why the call failed.
		suggestion := ""
		if cfg.HintCommandTemplate != "" {
			// Sanitize tgt.ID before embedding in the suggestion string:
			// strip newlines and ASCII control characters (< 0x20 or DEL)
			// so crafted IDs cannot produce misleading multi-line TTY output.
			safeID := strings.Map(func(r rune) rune {
				if r < 32 || r == 127 {
					return -1
				}
				return r
			}, tgt.ID)
			suggestion = "verify the alert group exists: gcx irm oncall alert-groups get " + safeID
		}
		return ackOutcome{
			id: tgt.ID,
			err: &mutationTargetError{
				Code:       cfg.ErrorCode,
				Message:    err.Error(),
				Suggestion: suggestion,
			},
		}
	}
	emitProgressLine(stderr, capitalize(cfg.PastTense), tgt.ID, cfg.PastTense)
	return ackOutcome{id: tgt.ID, changed: true}
}

// ---------------------------------------------------------------------------
// Output codecs.
// ---------------------------------------------------------------------------

// mutationTextCodec renders either shape of the action-verb result envelope
// (singleMutationResult / bulkMutationResult) as a one-line human-readable
// summary. Registered as `-o text`; the JSON / YAML / agents codecs handle
// the structured shapes used by scripts and agents. Decoding is not
// supported — mutation results are an output-only contract.
type mutationTextCodec struct{}

func (c *mutationTextCodec) Format() format.Format { return format.Format("text") }

func (c *mutationTextCodec) Encode(w io.Writer, v any) error {
	switch r := v.(type) {
	case singleMutationResult:
		// Order matters: failure path has Changed=nil and Error!=nil, so
		// check Error first to avoid a nil-pointer dereference on Changed.
		switch {
		case r.Error != nil:
			_, err := fmt.Fprintf(w, "%s %s: failed (%s)\n", r.Action, r.Target.AlertGroupID, r.Error.Code)
			return err
		case r.Changed != nil && *r.Changed:
			_, err := fmt.Fprintf(w, "%s %s: done\n", r.Action, r.Target.AlertGroupID)
			return err
		default:
			// Idempotent no-op: Changed != nil && *Changed == false.
			_, err := fmt.Fprintf(w, "%s %s: no-op\n", r.Action, r.Target.AlertGroupID)
			return err
		}
	case bulkMutationResult:
		if _, err := fmt.Fprintf(w, "%s: %d/%d succeeded (%d skipped, %d failed)\n",
			r.Action, r.Summary.Succeeded, r.Summary.Matched, r.Summary.Skipped, r.Summary.Failed); err != nil {
			return err
		}
		for _, f := range r.Failures {
			if _, err := fmt.Fprintf(w, "  failed: %s — %s\n", f.Target.AlertGroupID, f.Error.Code); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("text codec: unsupported value type %T (expected singleMutationResult or bulkMutationResult)", v)
	}
}

func (c *mutationTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// ---------------------------------------------------------------------------
// Error builders — DetailedError envelopes for the guardrails.
// ---------------------------------------------------------------------------

func missingIDOrFilterError() error {
	exit2 := 2
	return &fail.DetailedError{
		Summary:  "<id> argument or filter flag required",
		Details:  "Bulk action verbs require either a positional <id> or one or more filter flags to scope the operation. Acting on every alert group is not supported.",
		ExitCode: &exit2,
		Suggestions: []string{
			"Pass an alert-group ID: gcx irm oncall alert-groups acknowledge <id>",
			"Filter by team: gcx irm oncall alert-groups acknowledge --team <name> --force",
			"Filter by status + age: gcx irm oncall alert-groups acknowledge --state firing --max-age 24h --force",
		},
	}
}

func idAndFilterError() error {
	exit2 := 2
	return &fail.DetailedError{
		Summary:  "<id> argument and filter flags are mutually exclusive",
		Details:  "Pass either a positional <id> for single-target mode OR filter flags for bulk-by-filter mode, but not both.",
		ExitCode: &exit2,
		Suggestions: []string{
			"Drop the filter flags to act on the single ID",
			"Drop the positional argument to act on the filtered set",
		},
	}
}

func agentModeRequiresForceError(count int) error {
	exit2 := 2
	return &fail.DetailedError{
		Summary:  "agent mode requires --force when target count > 1",
		Details:  fmt.Sprintf("Matched %d alert groups. Bulk action verbs require an explicit --force in agent mode to avoid auto-confirming destructive operations.", count),
		ExitCode: &exit2,
		Suggestions: []string{
			"Re-run with --force if the filter set is correct",
			"Narrow the filter to confirm the intended target set first",
		},
	}
}

func cancelledError() error {
	exitCancelled := fail.ExitCancelled
	return &fail.DetailedError{
		Summary:  "operation cancelled by user",
		Details:  "Confirmation prompt was declined; no targets were modified.",
		ExitCode: &exitCancelled,
	}
}
