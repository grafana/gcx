// Package changereview exposes Grafana Assistant change review commands: the
// checks it runs on GitHub pull requests and how those checks perform.
package changereview

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	client "github.com/grafana/gcx/internal/assistant/changereview"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/shared"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const experimentalPreamble = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

// maxWindow is the longest window the statistics API reads.
const maxWindow = 90 * 24 * time.Hour

func markExperimental(cmd *cobra.Command) {
	long := cmd.Long
	if long == "" {
		long = cmd.Short
	}
	cmd.Short = "[experimental] " + cmd.Short
	cmd.Long = experimentalPreamble + "\n\n" + long
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[agent.AnnotationStability] = agent.StabilityExperimental
}

// Commands returns the change-review command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change-review",
		Short: "Manage Assistant change review of GitHub pull requests.",
		Long: `Configure the checks Grafana Assistant change review runs on GitHub pull
requests, and see how they perform.

Checks are stack-wide: built-in checks ship with change review and can be
tuned or turned off; custom checks carry your own instructions. Saves are
guarded by a revision, so a concurrent edit in Grafana is never overwritten.`,
		Example: `  gcx assistant change-review checks list
  gcx assistant change-review stats --since 7d`,
	}
	markExperimental(cmd)

	checks := &cobra.Command{
		Use:   "checks",
		Short: "Manage change review checks.",
		Long:  "List, inspect, create, update, and delete the checks change review runs on pull requests.",
	}
	markExperimental(checks)
	checks.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
		newListPullRequestsCommand(loader),
	)
	cmd.AddCommand(checks, newStatsCommand(loader))
	return cmd
}

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*client.Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return client.NewClient(base), nil
}

// requireID returns the trimmed check ID, or a usage error when it is empty.
func requireID(cmd *cobra.Command, arg string) (string, error) {
	id := strings.TrimSpace(arg)
	if id == "" {
		return "", usageError(cmd, "CHECK must be a nonempty check ID; list IDs with: gcx assistant change-review checks list")
	}
	return id, nil
}

// usageError reports invalid input with exit code 2, so callers can tell it
// from a failed request.
func usageError(cmd *cobra.Command, format string, args ...any) error {
	return gcxerrors.NewCommandUsageError(cmd, fmt.Sprintf(format, args...), nil)
}

func findCheck(checks []client.Check, id string) (int, error) {
	index := slices.IndexFunc(checks, func(c client.Check) bool { return c.ID == id })
	if index < 0 {
		return -1, &gcxerrors.DetailedError{
			Summary:     "Change review check not found",
			Details:     fmt.Sprintf("No check has ID %q.", id),
			Suggestions: []string{"List check IDs: gcx assistant change-review checks list"},
		}
	}
	return index, nil
}

// ---- list ----

// checkList is the list payload; --json field selection applies to its checks.
type checkList struct {
	Checks []client.Check `json:"checks"`
}

func (checkList) ListItemsKey() string { return "checks" }

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	table := cmdio.TableAdapter[checkList, client.Check]{
		Table: checksTable(),
		Rows:  func(list checkList) ([]client.Check, error) { return list.Checks, nil },
	}
	for _, name := range []string{cmdio.FormatText, cmdio.FormatTable, cmdio.FormatWide} {
		o.IO.RegisterCustomCodec(name, table.Codec(name))
	}
	o.IO.DefaultFormat(cmdio.FormatText)
	o.IO.BindFlags(flags)
}

func (o *listOpts) Validate() error { return o.IO.Validate() }

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List change review checks.",
		Long: `List every change review check: each built-in check with the stack's settings
for it, followed by the stack's custom checks.

Use --output wide for where each check applies and its repository overrides,
or get CHECK for a check's full instructions.`,
		Example: `  gcx assistant change-review checks list
  gcx assistant change-review checks list -o wide
  gcx assistant change-review checks list -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			c, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			checks, err := c.ListChecks(cmd.Context())
			if err != nil {
				return err
			}
			list := checkList{Checks: checks.Checks}
			if list.Checks == nil {
				list.Checks = []client.Check{}
			}
			return opts.IO.Encode(cmd.OutOrStdout(), list)
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func checksTable() cmdio.Table[client.Check] {
	return cmdio.Table[client.Check]{
		Columns: []cmdio.Column[client.Check]{
			{Header: "ID", Content: func(c client.Check) string { return c.ID }},
			{Header: "NAME", Content: func(c client.Check) string { return c.Name }},
			{Header: "TYPE", Content: func(c client.Check) string {
				if c.BuiltIn {
					return "built-in"
				}
				return "custom"
			}},
			{Header: "ENABLED", Content: func(c client.Check) string { return strconv.FormatBool(c.Enabled) }},
			{Header: "BLOCKING", Content: func(c client.Check) string { return strconv.FormatBool(c.Blocking) }},
			{Header: "PATHS", Content: func(c client.Check) string { return orAll(c.Paths) }},
			{Header: "APPLIES-TO", Visible: cmdio.WideOnly, Content: func(c client.Check) string {
				if len(c.AppliesTo) == 0 {
					return "pr"
				}
				return strings.Join(c.AppliesTo, ",")
			}},
			{Header: "OVERRIDES", Visible: cmdio.WideOnly, Content: func(c client.Check) string { return overridesSummary(c.RepositoryOverrides) }},
		},
		Empty: func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "No change review checks.")
			return err
		},
	}
}

func orAll(paths []string) string {
	if len(paths) == 0 {
		return "*"
	}
	return strings.Join(paths, ",")
}

func overridesSummary(overrides map[string]client.CheckOverride) string {
	if len(overrides) == 0 {
		return "-"
	}
	repos := make([]string, 0, len(overrides))
	for repo, o := range overrides {
		var parts []string
		if o.Enabled != nil {
			parts = append(parts, "enabled="+strconv.FormatBool(*o.Enabled))
		}
		if o.Blocking != nil {
			parts = append(parts, "blocking="+strconv.FormatBool(*o.Blocking))
		}
		repos = append(repos, repo+"("+strings.Join(parts, ",")+")")
	}
	slices.Sort(repos)
	return strings.Join(repos, " ")
}

// ---- get ----

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (o *getOpts) Validate() error { return o.IO.Validate() }

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get CHECK",
		Short: "Get a change review check.",
		Long: `Get one change review check by ID, including its full instructions, paths,
and repository overrides.

The output is valid input for update -f, so get, edit, and update round-trip.`,
		Example: `  gcx assistant change-review checks get deploy-hazards
  gcx assistant change-review checks get deploy-hazards -o json
  gcx assistant change-review checks get my-check > check.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := requireID(cmd, args[0])
			if err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			c, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			checks, err := c.ListChecks(cmd.Context())
			if err != nil {
				return err
			}
			index, err := findCheck(checks.Checks, id)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), checks.Checks[index])
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

// ---- create / update ----

// checkInput is a check as read from a file: a field left out keeps its value.
// IDs, built-in and description are read-only and ignored, so the output of
// get is valid input.
type checkInput struct {
	ID                  string                           `json:"id,omitempty"`
	Name                *string                          `json:"name,omitempty"`
	Instructions        *string                          `json:"instructions,omitempty"`
	Enabled             *bool                            `json:"enabled,omitempty"`
	Blocking            *bool                            `json:"blocking,omitempty"`
	Paths               *[]string                        `json:"paths,omitempty"`
	RepositoryOverrides *map[string]client.CheckOverride `json:"repositoryOverrides,omitempty"`
	AppliesTo           *[]string                        `json:"appliesTo,omitempty"`
}

// editFlags are the flags create and update share. A flag changes the check
// only when it is set.
type editFlags struct {
	File         string
	Name         string
	Instructions string
	Enabled      bool
	Blocking     bool
	Paths        []string
	AppliesTo    []string
}

func (f *editFlags) bind(flags *pflag.FlagSet) {
	flags.StringVarP(&f.File, "filename", "f", "", "Read the check from a YAML or JSON file (- for stdin); flags override its fields")
	flags.StringVar(&f.Name, "name", "", "Name shown on findings (custom checks only)")
	flags.StringVar(&f.Instructions, "instructions", "", "What the reviewer should look for; on a built-in check, added to its own guidance")
	flags.BoolVar(&f.Enabled, "enabled", true, "Run the check; --enabled=false turns it off")
	flags.BoolVar(&f.Blocking, "blocking", false, "Fail the GitHub Check on a confirmed finding; otherwise findings are advisory")
	flags.StringArrayVar(&f.Paths, "path", nil, "File pattern the pull request must change for the check to run (repeatable; ** matches directories); --path '' runs it on every pull request")
	flags.StringSliceVar(&f.AppliesTo, "applies-to", nil, "Reviews that apply a custom check: plan, code, pr (comma-separated; default pr)")
}

// input merges the file and the set flags into one checkInput.
func (f *editFlags) input(cmd *cobra.Command) (checkInput, error) {
	var in checkInput
	if cmd.Flags().Changed("filename") && strings.TrimSpace(f.File) == "" {
		return in, usageError(cmd, "--filename cannot be empty; pass a YAML or JSON file, or - for stdin")
	}
	if f.File != "" {
		if err := providers.ReadFileOrStdin(f.File, cmd.InOrStdin(), &in); err != nil {
			return in, err
		}
	}
	flags := cmd.Flags()
	if flags.Changed("name") {
		in.Name = &f.Name
	}
	if flags.Changed("instructions") {
		in.Instructions = &f.Instructions
	}
	if flags.Changed("enabled") {
		in.Enabled = &f.Enabled
	}
	if flags.Changed("blocking") {
		in.Blocking = &f.Blocking
	}
	if flags.Changed("path") {
		paths := make([]string, 0, len(f.Paths))
		for _, p := range f.Paths {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
		in.Paths = &paths
	}
	if flags.Changed("applies-to") {
		in.AppliesTo = &f.AppliesTo
	}
	if in.AppliesTo != nil {
		for _, stage := range *in.AppliesTo {
			if !slices.Contains([]string{"plan", "code", "pr"}, stage) {
				return in, usageError(cmd, "invalid --applies-to %q: use plan, code, or pr, for example --applies-to pr", stage)
			}
		}
	}
	return in, nil
}

// apply writes the set fields of in onto check.
func (in checkInput) apply(cmd *cobra.Command, check client.Check) (client.Check, error) {
	if check.BuiltIn {
		if in.Name != nil && *in.Name != check.Name {
			return check, usageError(cmd, "built-in check %q keeps its own name; --name applies to custom checks only", check.ID)
		}
		if in.AppliesTo != nil && len(*in.AppliesTo) > 0 && !slices.Equal(*in.AppliesTo, []string{"pr"}) {
			return check, usageError(cmd, "built-in check %q reviews pull requests only; --applies-to applies to custom checks only", check.ID)
		}
	}
	if in.Name != nil {
		check.Name = strings.TrimSpace(*in.Name)
	}
	if in.Instructions != nil {
		check.Instructions = strings.TrimSpace(*in.Instructions)
	}
	if in.Enabled != nil {
		check.Enabled = *in.Enabled
	}
	if in.Blocking != nil {
		check.Blocking = *in.Blocking
	}
	if in.Paths != nil {
		check.Paths = *in.Paths
	}
	if in.RepositoryOverrides != nil {
		check.RepositoryOverrides = *in.RepositoryOverrides
	}
	if in.AppliesTo != nil && !check.BuiltIn {
		check.AppliesTo = *in.AppliesTo
	}
	if !check.BuiltIn && (check.Name == "" || check.Instructions == "") {
		return check, usageError(cmd, "a custom check needs --name and --instructions")
	}
	return check, nil
}

// mutationResult reports a create, update, or delete with the check it
// affected.
// It is the shared gcx.mutation shape with the saved check added.
type mutationResult struct {
	cmdio.SingleMutation

	Check client.Check `json:"check"`
}

func newMutationResult(action string, check client.Check) mutationResult {
	return mutationResult{
		SingleMutation: cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: "ChangeReviewCheck", ID: check.ID, Name: check.Name}),
		Check:          check,
	}
}

type mutationOpts struct {
	IO   cmdio.Options
	Edit editFlags
}

func (o *mutationOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	o.Edit.bind(flags)
}

func (o *mutationOpts) Validate() error { return o.IO.Validate() }

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &mutationOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a custom change review check.",
		Long: `Create a custom change review check from flags or a file. Its ID is derived
from its name. New checks are enabled and advisory unless --enabled=false or
--blocking is given.

Change review applies the check to pull requests opened or updated after the
save; reviews already running keep the checks they started with.`,
		Example: `  gcx assistant change-review checks create --name "Migration safety" \
    --instructions "Flag schema migrations that lock large tables." --path 'migrations/**'
  gcx assistant change-review checks create -f check.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			in, err := opts.Edit.input(cmd)
			if err != nil {
				return err
			}
			check, err := in.apply(cmd, client.Check{Enabled: true})
			if err != nil {
				return err
			}
			c, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			var before map[string]bool
			saved, err := c.EditChecks(cmd.Context(), func(checks []client.Check) ([]client.Check, error) {
				before = make(map[string]bool, len(checks))
				for _, existing := range checks {
					before[existing.ID] = true
				}
				return append(slices.Clone(checks), check), nil
			})
			if err != nil {
				return saveError(err)
			}
			for _, created := range saved.Checks {
				if !before[created.ID] {
					return opts.IO.Encode(cmd.OutOrStdout(), newMutationResult("created", created))
				}
			}
			return &gcxerrors.DetailedError{
				Summary:     "Created change review check not found in the saved checks",
				Details:     fmt.Sprintf("The save succeeded, but no check in the response is new, so %q could not be identified.", check.Name),
				Suggestions: []string{"List the checks to see whether it was saved: gcx assistant change-review checks list"},
			}
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &mutationOpts{}
	cmd := &cobra.Command{
		Use:   "update CHECK",
		Short: "Update a change review check.",
		Long: `Update a built-in or custom change review check. Only the fields set by flags
or present in the file change; the rest keep their values.

The revision guard covers the moment of the save, not the time since a file
was exported: a file from get carries every field, so saving it later
overwrites any edit made in between. Keep files to the fields you mean to
change, or use flags.

Built-in checks keep their name and guidance: --instructions adds to that
guidance, and --enabled, --blocking, and --path tune where and how they run.`,
		Example: `  gcx assistant change-review checks update instrumentation --blocking
  gcx assistant change-review checks update production-impact --enabled=false
  gcx assistant change-review checks update my-check --path 'api/**' --path 'pkg/**'
  gcx assistant change-review checks update my-check -f check.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := requireID(cmd, args[0])
			if err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			in, err := opts.Edit.input(cmd)
			if err != nil {
				return err
			}
			if in.ID != "" && in.ID != id {
				return usageError(cmd, "the file describes check %q, not %q; check IDs cannot be changed", in.ID, id)
			}
			c, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			saved, err := c.EditChecks(cmd.Context(), func(checks []client.Check) ([]client.Check, error) {
				index, err := findCheck(checks, id)
				if err != nil {
					return nil, err
				}
				next := slices.Clone(checks)
				if next[index], err = in.apply(cmd, next[index]); err != nil {
					return nil, err
				}
				return next, nil
			})
			if err != nil {
				return saveError(err)
			}
			index, err := findCheck(saved.Checks, id)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), newMutationResult("updated", saved.Checks[index]))
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

// ---- delete ----

type deleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.Force, "force", false, "Delete without confirmation")
}

func (o *deleteOpts) Validate() error { return o.IO.Validate() }

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete CHECK",
		Short: "Delete a custom change review check.",
		Long: `Delete a custom change review check. Built-in checks cannot be deleted; turn
one off with update CHECK --enabled=false.

The command prompts for confirmation by default. Use --force to bypass the
prompt. GCX_AUTO_APPROVE also bypasses the prompt for non-interactive workflows.
In agent mode, deletion requires --force or an enabled GCX_AUTO_APPROVE.`,
		Example: `  gcx assistant change-review checks delete migration-safety
  gcx assistant change-review checks delete migration-safety --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := requireID(cmd, args[0])
			if err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete change review check %q?", id))
			if err != nil {
				return err
			}
			if !proceed {
				exitCancelled := gcxerrors.ExitCancelled
				return &gcxerrors.DetailedError{
					Summary:  "delete cancelled",
					Details:  fmt.Sprintf("Confirmation prompt was declined; change review check %q was not deleted.", id),
					ExitCode: &exitCancelled,
				}
			}
			c, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			var deleted client.Check
			_, err = c.EditChecks(cmd.Context(), func(checks []client.Check) ([]client.Check, error) {
				index, err := findCheck(checks, id)
				if err != nil {
					return nil, err
				}
				if checks[index].BuiltIn {
					return nil, &gcxerrors.DetailedError{
						Summary:     "Built-in checks cannot be deleted",
						Details:     fmt.Sprintf("%q ships with change review.", id),
						Suggestions: []string{"Turn it off instead: gcx assistant change-review checks update " + id + " --enabled=false"},
					}
				}
				deleted = checks[index]
				return slices.Delete(slices.Clone(checks), index, index+1), nil
			})
			if err != nil {
				return saveError(err)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), newMutationResult("deleted", deleted))
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func saveError(err error) error {
	if errors.Is(err, client.ErrConflict) {
		return &gcxerrors.DetailedError{
			Summary:     "Change review checks changed while saving",
			Details:     "Another edit saved the checks twice while this command was applying its change; nothing was saved.",
			Parent:      err,
			Suggestions: []string{"Re-run the command; it applies the change to the latest checks"},
		}
	}
	return err
}

// ---- time window (stats, list-pull-requests) ----

type windowFlags struct {
	Since      string
	From       string
	To         string
	Repository string
}

func (w *windowFlags) bind(flags *pflag.FlagSet) {
	flags.StringVar(&w.Since, "since", "30d", "Window ending now (e.g. 7d, 30d; at most 90d); cannot be used with --from/--to")
	flags.StringVar(&w.From, "from", "", "Start of the window (RFC3339, Unix seconds, or now-7d); empty is invalid")
	flags.StringVar(&w.To, "to", "", "End of the window (RFC3339, Unix seconds, or now); defaults to now; empty is invalid")
	flags.StringVar(&w.Repository, "repository", "", "Only reviews of this owner/name repository; empty is invalid")
}

// resolve turns the flags into a query before any request.
func (w *windowFlags) resolve(cmd *cobra.Command, now time.Time) (client.StatsQuery, error) {
	var q client.StatsQuery
	flags := cmd.Flags()
	if flags.Changed("repository") {
		owner, name, ok := strings.Cut(strings.TrimSpace(w.Repository), "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return q, usageError(cmd, "invalid --repository %q: use owner/name, for example --repository grafana/gcx", w.Repository)
		}
		q.Repository = owner + "/" + name
	}
	fromChanged, toChanged := flags.Changed("from"), flags.Changed("to")
	if flags.Changed("since") && (fromChanged || toChanged) {
		return q, usageError(cmd, "--since cannot be used together with --from or --to")
	}
	if !fromChanged && !toChanged {
		since, err := shared.ParseDuration(w.Since)
		if err != nil || since <= 0 || since > maxWindow {
			return q, usageError(cmd, "invalid --since %q: use a positive duration of at most 90d, for example --since 7d", w.Since)
		}
		q.From, q.To = now.Add(-since), now
		return q, nil
	}
	for _, bound := range []struct {
		name, value string
		changed     bool
		target      *time.Time
	}{{"from", w.From, fromChanged, &q.From}, {"to", w.To, toChanged, &q.To}} {
		if !bound.changed {
			continue
		}
		if strings.TrimSpace(bound.value) == "" {
			return q, usageError(cmd, "--%s cannot be empty; use RFC3339, Unix seconds, or a relative time such as now-7d", bound.name)
		}
		t, err := shared.ParseTime(bound.value, now)
		if err != nil {
			return q, usageError(cmd, "invalid --%s %q: %v", bound.name, bound.value, err)
		}
		*bound.target = t
	}
	if !toChanged {
		q.To = now
	}
	if !fromChanged {
		q.From = q.To.Add(-30 * 24 * time.Hour)
	}
	switch {
	case !q.From.Before(q.To):
		return q, usageError(cmd, "--from must be before --to")
	case q.To.Sub(q.From) > maxWindow:
		return q, usageError(cmd, "the window can be at most 90 days; narrow --from or --to")
	}
	return q, nil
}

func truncationHint(cmd *cobra.Command, truncated bool) {
	if truncated {
		cmdio.EmitHint(cmd.ErrOrStderr(), "partial result: the server reads at most 5000 reviews and lists at most 100 pull requests, covering the latest. Narrow --since or --repository for complete counts", "")
	}
}

// ---- stats ----

type statsOpts struct {
	IO     cmdio.Options
	Window windowFlags
}

func (o *statsOpts) setup(flags *pflag.FlagSet) {
	for _, name := range []string{cmdio.FormatText, cmdio.FormatTable, cmdio.FormatWide} {
		o.IO.RegisterCustomCodec(name, &statsCodec{name: name})
	}
	o.IO.DefaultFormat(cmdio.FormatText)
	o.IO.BindFlags(flags)
	o.Window.bind(flags)
}

func (o *statsOpts) Validate() error { return o.IO.Validate() }

func newStatsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &statsOpts{}
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show how change review checks perform.",
		Long: `Summarize the stack's published change reviews in a window: totals, and per
check the issues it confirmed, how many a later review found fixed or still
present, and how many it ruled out.

Counts only; finding text is never returned. Use checks list-pull-requests
CHECK for the pull requests behind one check's counts. Reviews are kept only
as long as their review chat, so older windows may be partial.`,
		Example: `  gcx assistant change-review stats
  gcx assistant change-review stats --since 7d --repository grafana/gcx
  gcx assistant change-review stats -o wide
  gcx assistant change-review stats -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			q, err := opts.Window.resolve(cmd, time.Now())
			if err != nil {
				return err
			}
			c, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			stats, err := c.Stats(cmd.Context(), q)
			if err != nil {
				return err
			}
			normalizeStats(stats)
			if err := opts.IO.Encode(cmd.OutOrStdout(), *stats); err != nil {
				return err
			}
			truncationHint(cmd, stats.Truncated)
			return nil
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func normalizeStats(s *client.Stats) {
	if s.Checks == nil {
		s.Checks = []client.CheckStats{}
	}
	if s.Repositories == nil {
		s.Repositories = []client.RepositoryStats{}
	}
	if s.IncompleteReasons == nil {
		s.IncompleteReasons = []client.IncompleteReason{}
	}
}

type statsCodec struct{ name string }

func (c *statsCodec) Format() format.Format { return format.Format(c.name) }

func (c *statsCodec) Decode(io.Reader, any) error {
	return fmt.Errorf("%s format does not support decoding", c.name)
}

func (c *statsCodec) Encode(w io.Writer, v any) error {
	s, ok := v.(client.Stats)
	if !ok {
		return fmt.Errorf("invalid data type for %s codec: expected client.Stats, got %T", c.name, v)
	}
	wide := c.name == cmdio.FormatWide
	t := s.Totals
	if _, err := fmt.Fprintf(w, "Window: %s to %s\nReviews: %d on %d pull requests (%d incomplete, %d failed the Check)\nConfirmed issues: %d (%d fixed, %d still present, %d awaiting review, %d unknown)\n\n",
		s.Window.From.Format(time.DateOnly), s.Window.To.Format(time.DateOnly),
		t.Reviews, t.PullRequests, t.Incomplete, t.Holds,
		t.Confirmed, t.Fixed, t.StillPresent, t.AwaitingReview, t.Unknown); err != nil {
		return err
	}
	if len(s.Checks) == 0 {
		_, err := fmt.Fprintln(w, "No published reviews in this window.")
		return err
	}
	headers := []string{"CHECK", "TITLE", "CONFIRMED", "FIXED", "STILL-PRESENT", "AWAITING", "RULED-OUT", "HOLDS"}
	if wide {
		headers = append(headers, "UNCONFIRMED", "UNKNOWN", "TYPE", "BLOCKING")
	}
	checks := style.NewTable(headers...)
	for _, ch := range s.Checks {
		row := []string{ch.ID, ch.Title, itoa(ch.Confirmed), itoa(ch.Fixed), itoa(ch.StillPresent), itoa(ch.AwaitingReview), itoa(ch.RuledOut), itoa(ch.Holds)}
		if wide {
			kind := "built-in"
			if ch.Custom {
				kind = "custom"
			}
			row = append(row, itoa(ch.Unconfirmed), itoa(ch.Unknown), kind, strconv.FormatBool(ch.Blocking))
		}
		checks.Row(row...)
	}
	if err := checks.Render(w); err != nil {
		return err
	}
	if !wide {
		return nil
	}
	if len(s.Repositories) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		repos := style.NewTable("REPOSITORY", "REVIEWS", "CONFIRMED", "FIXED", "INCOMPLETE")
		for _, r := range s.Repositories {
			repos.Row(r.Repository, itoa(r.Reviews), itoa(r.Confirmed), itoa(r.Fixed), itoa(r.Incomplete))
		}
		if err := repos.Render(w); err != nil {
			return err
		}
	}
	if len(s.IncompleteReasons) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		reasons := style.NewTable("INCOMPLETE-REASON", "REVIEWS")
		for _, r := range s.IncompleteReasons {
			reasons.Row(r.Kind, itoa(r.Count))
		}
		return reasons.Render(w)
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// ---- list-pull-requests ----

// pullRequestList is the list-pull-requests payload; --json field selection
// applies to its pull requests.
type pullRequestList struct {
	client.PullRequests

	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

// maxPullRequests is the most pull requests one response lists.
const maxPullRequests = 100

func (pullRequestList) ListItemsKey() string { return "pullRequests" }

type pullRequestsOpts struct {
	IO     cmdio.Options
	Window windowFlags
}

func (o *pullRequestsOpts) setup(flags *pflag.FlagSet) {
	table := cmdio.TableAdapter[pullRequestList, client.PullRequest]{
		Table: pullRequestsTable(),
		Rows:  func(list pullRequestList) ([]client.PullRequest, error) { return list.PullRequests.PullRequests, nil },
	}
	for _, name := range []string{cmdio.FormatText, cmdio.FormatTable, cmdio.FormatWide} {
		o.IO.RegisterCustomCodec(name, table.Codec(name))
	}
	o.IO.DefaultFormat(cmdio.FormatText)
	o.IO.BindFlags(flags)
	o.Window.bind(flags)
}

func (o *pullRequestsOpts) Validate() error { return o.IO.Validate() }

func newListPullRequestsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pullRequestsOpts{}
	cmd := &cobra.Command{
		Use:   "list-pull-requests CHECK",
		Short: "List the pull requests behind a check's statistics.",
		Long: `List the pull requests whose published reviews raised findings from one
check, or failed the GitHub Check because of it, most recently reviewed first,
with that check's counts on each.

Counts only; finding text is never returned. Use stats for every check's
totals.`,
		Example: `  gcx assistant change-review checks list-pull-requests deploy-hazards
  gcx assistant change-review checks list-pull-requests deploy-hazards --since 7d --repository grafana/gcx
  gcx assistant change-review checks list-pull-requests deploy-hazards -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := requireID(cmd, args[0])
			if err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			q, err := opts.Window.resolve(cmd, time.Now())
			if err != nil {
				return err
			}
			c, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			pulls, err := c.PullRequests(cmd.Context(), id, q)
			if err != nil {
				return err
			}
			if pulls.PullRequests == nil {
				pulls.PullRequests = []client.PullRequest{}
			}
			list := pullRequestList{PullRequests: *pulls}
			if pulls.Truncated {
				// The server caps the reviews it reads and the pull requests it
				// lists; neither can be raised, only narrowed.
				list.ListMeta = &cmdio.ListMeta{Truncated: true, Returned: len(pulls.PullRequests)}
				if len(pulls.PullRequests) >= maxPullRequests {
					list.ListMeta.Cap = maxPullRequests
				}
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), list); err != nil {
				return err
			}
			truncationHint(cmd, pulls.Truncated)
			return nil
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func pullRequestsTable() cmdio.Table[client.PullRequest] {
	return cmdio.Table[client.PullRequest]{
		Columns: []cmdio.Column[client.PullRequest]{
			{Header: "REPOSITORY", Content: func(p client.PullRequest) string { return p.Repository }},
			{Header: "PR", Content: func(p client.PullRequest) string { return "#" + itoa(p.PullNumber) }},
			{Header: "REVIEWS", Content: func(p client.PullRequest) string { return itoa(p.Reviews) }},
			{Header: "CONFIRMED", Content: func(p client.PullRequest) string { return itoa(p.Confirmed) }},
			{Header: "FIXED", Content: func(p client.PullRequest) string { return itoa(p.Fixed) }},
			{Header: "STILL-PRESENT", Content: func(p client.PullRequest) string { return itoa(p.StillPresent) }},
			{Header: "HOLDS", Content: func(p client.PullRequest) string { return itoa(p.Holds) }},
			{Header: "LATEST-REVIEW", Content: func(p client.PullRequest) string { return assistanthttp.FormatTime(p.LatestPublishedAt) }},
			{Header: "UNCONFIRMED", Visible: cmdio.WideOnly, Content: func(p client.PullRequest) string { return itoa(p.Unconfirmed) }},
			{Header: "RULED-OUT", Visible: cmdio.WideOnly, Content: func(p client.PullRequest) string { return itoa(p.RuledOut) }},
			{Header: "AWAITING", Visible: cmdio.WideOnly, Content: func(p client.PullRequest) string { return itoa(p.AwaitingReview) }},
			{Header: "UNKNOWN", Visible: cmdio.WideOnly, Content: func(p client.PullRequest) string { return itoa(p.Unknown) }},
			{Header: "LATEST-REVIEW-ID", Visible: cmdio.WideOnly, Content: func(p client.PullRequest) string { return p.LatestReviewID }},
		},
		Empty: func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "No pull requests in this window.")
			return err
		},
	}
}
