package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/watcher"
	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type versionListOpts struct {
	readOpts

	Limit     int
	Before    int64
	BeforeSet bool
}

func (o *versionListOpts) setup(flags *pflag.FlagSet) {
	o.readOpts.setup(flags)
	flags.IntVar(&o.Limit, "limit", 20, "Maximum retained version summaries in this page (1-100)")
	flags.Int64Var(&o.Before, "before-version", 0, "Read versions older than this exclusive positive version cursor; omitted starts at newest")
}
func (o *versionListOpts) Validate() error {
	if o.Limit < 1 || o.Limit > 100 {
		return errors.New("--limit must be between 1 and 100")
	}
	if o.Before < 0 || (o.BeforeSet && o.Before == 0) {
		return errors.New("--before-version must be positive when supplied")
	}
	return o.readOpts.Validate()
}
func newVersionListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &versionListOpts{}
	cmd := &cobra.Command{Use: "list-versions WATCHER", Short: "List a page of retained Watcher definition versions.", Long: "Read one newest-first page of retained finalized definition summaries, including actor, time, change summary and server-reported learning effects. --limit defaults to 20 (1-100). Continue with coverage.nextBeforeVersion using --before-version. Coverage describes caller-visible server-retained history, not lifetime history or an atomic snapshot; a cursor intentionally excludes newer versions. Retention is controlled by the server. Use versions get for one retained definition and versions diff to compare definitions.", Args: cobra.ExactArgs(1), Example: "  gcx assistant watchers list-versions checkout-health --limit 20 -o json\n  gcx assistant watchers list-versions checkout-health --before-version 12 -o yaml", RunE: func(cmd *cobra.Command, args []string) error {
		opts.Ref = args[0]
		opts.BeforeSet = cmd.Flags().Changed("before-version")
		if err := opts.Validate(); err != nil {
			return err
		}
		client, _, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		return runVersionList(cmd, client, opts)
	}}
	markExperimental(cmd)
	cmd.Annotations[agent.AnnotationTokenCost] = "medium"
	cmd.Annotations[agent.AnnotationLLMHint] = "One retained-history page; narrow with --limit and --json version,createdAt,changeSummary. Coverage and the continuation cursor remain visible under field selection."
	opts.setup(cmd.Flags())
	return cmd
}

type versionOpts struct {
	readOpts

	Version int64
}

func parseVersion(name, value string) (int64, error) {
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q; try gcx assistant watchers list-versions WATCHER to see retained versions", name, value)
	}
	return version, nil
}
func newVersionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "versions", Short: "Inspect, compare and restore retained Watcher definitions.", Long: "Read retained finalized definitions and compare their definition fields. Restore requires a non-archived inactive Watcher (draft, paused or ready) and an explicit current-version guard; it never starts monitoring."}
	markExperimental(cmd)
	cmd.AddCommand(newVersionGetCommand(loader), newVersionDiffCommand(loader), newVersionRestoreCommand(loader))
	return cmd
}

//nolint:dupl // Get and restore retain independent options and safety wiring.
func newVersionGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &versionOpts{}
	cmd := &cobra.Command{Use: "get WATCHER VERSION", Short: "Read one retained Watcher definition version.", Long: "Read one positive definition version and its retained change metadata, including server-reported learning effects when present. The definition is the version API's monitoring definition, not a reusable configuration manifest. Missing versions may be pruned or unavailable; gcx never invents earlier definitions. Use watchers get for current configuration.", Args: cobra.ExactArgs(2), Example: "  gcx assistant watchers versions get checkout-health 2 -o yaml", RunE: func(cmd *cobra.Command, args []string) error {
		opts.Ref = args[0]
		var err error
		opts.Version, err = parseVersion("VERSION", args[1])
		if err != nil {
			return err
		}
		if err := opts.Validate(); err != nil {
			return err
		}
		client, _, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		return runVersionGet(cmd, client, opts)
	}}
	markExperimental(cmd)
	cmd.Annotations[agent.AnnotationTokenCost] = "large"
	cmd.Annotations[agent.AnnotationLLMHint] = "One retained definition may contain many checks; prefer list-versions for summaries or --json version.version,version.changeSummary for metadata."
	opts.setup(cmd.Flags())
	return cmd
}

type versionDiffOpts struct {
	versionOpts

	To int64
}

func newVersionDiffCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &versionDiffOpts{}
	cmd := &cobra.Command{Use: "diff WATCHER FROM_VERSION TO_VERSION", Short: "Compare two retained Watcher definitions.", Long: "Fetch two retained versions and compare only their definition documents. Changes use sorted JSON pointer paths; arrays are compared as complete values. Runtime observations, audit metadata and learning-transition metadata are excluded. An unavailable version stops comparison instead of synthesizing history. This command does not write configuration.", Args: cobra.ExactArgs(3), Example: "  gcx assistant watchers versions diff checkout-health 1 2 -o json", RunE: func(cmd *cobra.Command, args []string) error {
		opts.Ref = args[0]
		var err error
		opts.Version, err = parseVersion("FROM_VERSION", args[1])
		if err != nil {
			return err
		}
		opts.To, err = parseVersion("TO_VERSION", args[2])
		if err != nil {
			return err
		}
		if err := opts.Validate(); err != nil {
			return err
		}
		client, _, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		return runVersionDiff(cmd, client, opts)
	}}
	markExperimental(cmd)
	cmd.Annotations[agent.AnnotationTokenCost] = "large"
	cmd.Annotations[agent.AnnotationLLMHint] = "Compares only two retained definitions; large changed arrays can produce large output. Use --json path,operation to narrow each change while retaining version and definition-only scope metadata."
	opts.setup(cmd.Flags())
	return cmd
}

type versionRestoreOpts struct {
	versionOpts

	Expected int64
	Summary  string
	Force    bool
}

func (o *versionRestoreOpts) setup(flags *pflag.FlagSet) {
	o.versionOpts.setup(flags)
	flags.Int64Var(&o.Expected, "expected-current-version", 0, "Required current definition version observed by the caller; stale guards refuse restore")
	flags.StringVar(&o.Summary, "summary", "", "Optional restore reason, at most 1000 Unicode characters")
	flags.BoolVar(&o.Force, "force", false, "Restore without confirmation")
}
func (o *versionRestoreOpts) Validate() error {
	if o.Expected < 1 {
		return errors.New("--expected-current-version requires a positive current definition version; inspect gcx assistant watchers status WATCHER")
	}
	if utf8.RuneCountInString(o.Summary) > 1000 {
		return errors.New("--summary must be at most 1000 Unicode characters")
	}
	return o.versionOpts.Validate()
}

//nolint:dupl // Get and restore retain independent options and safety wiring.
func newVersionRestoreCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &versionRestoreOpts{}
	cmd := &cobra.Command{Use: "restore WATCHER VERSION", Short: "Restore a retained definition of an inactive Watcher.", Long: "Create a new current version from one retained monitoring definition. The Watcher must be non-archived and draft, paused or ready. Pause a running Watcher separately; finish or cancel calibration in Grafana. gcx never pauses or starts it implicitly. --expected-current-version must be the current version you inspected; the server rejects a stale guard and conflicting calibration or execution. Restore retains current actions and other settings outside the version definition; it does not restore every manifest setting or copy historical learned run state. The result reports restored scope and observed inactive state, which may be draft. Confirmation is required unless --force or shared auto-approval applies. A lost response may mean restore saved: inspect status and list-versions before retrying; gcx never retries or rolls back automatically.", Args: cobra.ExactArgs(2), Example: "  gcx assistant watchers status checkout-health -o json\n  gcx assistant watchers versions restore checkout-health 1 --expected-current-version 3 --force -o json", RunE: func(cmd *cobra.Command, args []string) error {
		opts.Ref = args[0]
		var err error
		opts.Version, err = parseVersion("VERSION", args[1])
		if err != nil {
			return err
		}
		if err := opts.Validate(); err != nil {
			return err
		}
		client, _, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		return runVersionRestore(cmd, client, opts)
	}}
	markExperimental(cmd)
	cmd.Annotations[agent.AnnotationTokenCost] = "medium"
	cmd.Annotations[agent.AnnotationLLMHint] = "Requires inactive draft/paused/ready state and the caller's current-version guard. Reports saved versus uncertain restore and readback separately; inspect status/history before retrying an uncertain result."
	opts.setup(cmd.Flags())
	return cmd
}

type versionCoverage struct {
	Scope             string `json:"scope"`
	Limit             int    `json:"limit"`
	Returned          int    `json:"returned"`
	BeforeVersion     int64  `json:"beforeVersion,omitempty"`
	NextBeforeVersion *int64 `json:"nextBeforeVersion,omitempty"`
	Retention         string `json:"retention"`
	Snapshot          bool   `json:"snapshot"`
}
type versionListResult struct {
	Name     string                          `json:"name"`
	ID       string                          `json:"id"`
	Items    []clientwatchers.VersionSummary `json:"items"`
	Coverage versionCoverage                 `json:"coverage"`
}

func (versionListResult) ListItemsKey() string { return "items" }
func runVersionList(cmd *cobra.Command, client *clientwatchers.Client, opts *versionListOpts) error {
	item, err := watcher.Resolve(cmd.Context(), client, opts.Ref)
	if err != nil {
		return err
	}
	page, err := client.ListVersions(cmd.Context(), item.ID, opts.Limit, opts.Before)
	if err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(), versionListResult{Name: watcher.Watcher{Title: item.Name}.GetResourceName(), ID: item.ID, Items: page.Items, Coverage: versionCoverage{Scope: "caller-visible-retained-finalized-history", Limit: opts.Limit, Returned: len(page.Items), BeforeVersion: opts.Before, NextBeforeVersion: page.NextBeforeVersion, Retention: "server-controlled; lifetime completeness unknown", Snapshot: false}})
}

type versionGetResult struct {
	Name    string                        `json:"name"`
	ID      string                        `json:"id"`
	Version *clientwatchers.VersionDetail `json:"version"`
}

func runVersionGet(cmd *cobra.Command, client *clientwatchers.Client, opts *versionOpts) error {
	item, err := watcher.Resolve(cmd.Context(), client, opts.Ref)
	if err != nil {
		return err
	}
	version, err := client.GetVersion(cmd.Context(), item.ID, opts.Version)
	if err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(), versionGetResult{Name: watcher.Watcher{Title: item.Name}.GetResourceName(), ID: item.ID, Version: version})
}

type versionDiffResult struct {
	Name        string                     `json:"name"`
	ID          string                     `json:"id"`
	FromVersion int64                      `json:"fromVersion"`
	ToVersion   int64                      `json:"toVersion"`
	Scope       string                     `json:"scope"`
	Changes     []watcher.DefinitionChange `json:"changes"`
}

func (versionDiffResult) ListItemsKey() string { return "changes" }

func runVersionDiff(cmd *cobra.Command, client *clientwatchers.Client, opts *versionDiffOpts) error {
	item, err := watcher.Resolve(cmd.Context(), client, opts.Ref)
	if err != nil {
		return err
	}
	from, err := client.GetVersion(cmd.Context(), item.ID, opts.Version)
	if err != nil {
		return err
	}
	to := from
	if opts.To != opts.Version {
		to, err = client.GetVersion(cmd.Context(), item.ID, opts.To)
		if err != nil {
			return err
		}
	}
	changes, err := watcher.DiffDefinitions(from.Definition, to.Definition)
	if err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(), versionDiffResult{Name: watcher.Watcher{Title: item.Name}.GetResourceName(), ID: item.ID, FromVersion: opts.Version, ToVersion: opts.To, Scope: "definition-only", Changes: changes})
}

type versionRestoreScope struct {
	Restored []string `json:"restored"`
	Retained []string `json:"retained"`
	Learning string   `json:"learning"`
}
type versionRestoreResult struct {
	cmdio.SingleMutation

	Outcome                string                             `json:"outcome"`
	RequestedVersion       int64                              `json:"requestedVersion"`
	ExpectedCurrentVersion int64                              `json:"expectedCurrentVersion"`
	SavedVersion           *clientwatchers.VersionSummary     `json:"savedVersion,omitempty"`
	Observed               *watcher.VersionRestoreObservation `json:"observed,omitempty"`
	Scope                  *versionRestoreScope               `json:"scope,omitempty"`
	Guidance               string                             `json:"guidance,omitempty"`
}

func runVersionRestore(cmd *cobra.Command, client *clientwatchers.Client, opts *versionRestoreOpts) error {
	item, err := watcher.Resolve(cmd.Context(), client, opts.Ref)
	if err != nil {
		return err
	}
	if err := watcher.ValidateVersionRestore(item, opts.Expected); err != nil {
		return err
	}
	proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Restore Watcher %q from version %d?", opts.Ref, opts.Version))
	if err != nil {
		return err
	}
	if !proceed {
		code := gcxerrors.ExitCancelled
		return &gcxerrors.DetailedError{Summary: "version restore cancelled", ExitCode: &code}
	}
	outcome, restoreErr := watcher.RestoreVersion(cmd.Context(), client, item, opts.Version, opts.Expected, opts.Summary)
	if outcome == nil {
		return restoreErr
	}
	result := versionRestoreResult{SingleMutation: cmdio.NewSingleMutation("restore", cmdio.MutationTarget{Kind: watcher.WatcherKind, Name: watcher.Watcher{Title: item.Name}.GetResourceName(), ID: item.ID}), Outcome: outcome.Outcome, RequestedVersion: opts.Version, ExpectedCurrentVersion: opts.Expected, Observed: outcome.Observed, Guidance: outcome.Guidance}
	result.Changed = outcome.Changed
	if outcome.Accepted != nil {
		result.SavedVersion = &outcome.Accepted.VersionSummary
		var fields map[string]json.RawMessage
		// The accepted response already validates definition as a JSON object.
		_ = json.Unmarshal(outcome.Accepted.Definition, &fields)
		restored := make([]string, 0, len(fields))
		for field := range fields {
			restored = append(restored, field)
		}
		slices.Sort(restored)
		result.Scope = &versionRestoreScope{
			Restored: restored,
			Retained: []string{"notification and investigation actions", "secrets", "labels", "automatic-stop settings", "automatic-recalibration enrollment"},
			Learning: "server-reconciled; historical learned run state is not copied",
		}
	}
	if restoreErr != nil {
		result.Error = restoreErr.Error()
	}
	if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
		return fmt.Errorf("restore outcome %s; inspect Watcher %s before retrying: encode result: %w", result.Outcome, item.ID, err)
	}
	if restoreErr != nil {
		code := gcxerrors.ExitGeneralError
		switch {
		case outcome.Accepted != nil:
			code = gcxerrors.ExitPartialFailure
		case errors.Is(restoreErr, clientwatchers.ErrPermissionDenied):
			code = gcxerrors.ExitAuthFailure
		case errors.Is(restoreErr, context.Canceled):
			code = gcxerrors.ExitCancelled
		}
		return gcxerrors.NewEmittedError(code, restoreErr)
	}
	return nil
}

func encodeVersionList(dst io.Writer, result versionListResult) error {
	table := style.NewTable("VERSION", "TIME", "ACTOR", "CHANGE", "SUMMARY")
	for _, v := range result.Items {
		actor := v.Actor.UserID
		if actor == "" {
			actor = v.Actor.Type
		}
		table.Row(strconv.FormatInt(v.Version, 10), v.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), actor, v.ChangeKind, v.ChangeSummary)
	}
	if err := table.Render(dst); err != nil {
		return err
	}
	next := "none"
	if result.Coverage.NextBeforeVersion != nil {
		next = strconv.FormatInt(*result.Coverage.NextBeforeVersion, 10)
	}
	before := "newest"
	if result.Coverage.BeforeVersion != 0 {
		before = strconv.FormatInt(result.Coverage.BeforeVersion, 10)
	}
	_, err := fmt.Fprintf(dst, "Coverage: %d retained finalized versions; before: %s; next --before-version: %s; retention server-controlled, lifetime completeness unknown; not an atomic snapshot.\n", result.Coverage.Returned, before, next)
	return err
}
func encodeVersionGet(dst io.Writer, result versionGetResult) error {
	table := style.NewTable("FIELD", "VALUE")
	v := result.Version
	table.Row("version", strconv.FormatInt(v.Version, 10))
	table.Row("time", v.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	table.Row("actor", v.Actor.Type+" "+v.Actor.UserID)
	table.Row("summary", v.ChangeSummary)
	if len(v.LearningTransition) > 0 {
		table.Row("learningTransition", string(v.LearningTransition))
	}
	table.Row("definition", string(v.Definition))
	return table.Render(dst)
}
func encodeVersionDiff(dst io.Writer, result versionDiffResult) error {
	table := style.NewTable("PATH", "OPERATION", "BEFORE", "AFTER")
	for _, c := range result.Changes {
		before, after := "<absent>", "<absent>"
		if c.Before != nil {
			before = string(*c.Before)
		}
		if c.After != nil {
			after = string(*c.After)
		}
		table.Row(c.Path, c.Operation, before, after)
	}
	if err := table.Render(dst); err != nil {
		return err
	}
	_, err := fmt.Fprintf(dst, "Definition-only comparison: version %d -> %d; %d change(s).\n", result.FromVersion, result.ToVersion, len(result.Changes))
	return err
}
func encodeVersionRestore(dst io.Writer, result versionRestoreResult) error {
	table := style.NewTable("NAME", "ID", "OUTCOME", "REQUESTED VERSION", "NEW VERSION", "OBSERVED LIFECYCLE")
	saved, lifecycle := "unknown", "unknown"
	if result.SavedVersion != nil {
		saved = strconv.FormatInt(result.SavedVersion.Version, 10)
	}
	if result.Observed != nil {
		lifecycle = result.Observed.Lifecycle
	}
	table.Row(result.Target.Name, result.Target.ID, result.Outcome, strconv.FormatInt(result.RequestedVersion, 10), saved, lifecycle)
	if err := table.Render(dst); err != nil {
		return err
	}
	lines := []string{}
	if result.Scope != nil {
		lines = append(lines, "Restored: "+strings.Join(result.Scope.Restored, ", "), "Retained: "+strings.Join(result.Scope.Retained, ", "), result.Scope.Learning)
	}
	lines = append(lines, result.Error, result.Guidance)
	for _, line := range lines {
		if line != "" {
			if _, err := fmt.Fprintln(dst, line); err != nil {
				return err
			}
		}
	}
	return nil
}
