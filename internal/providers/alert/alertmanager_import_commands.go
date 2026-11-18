package alert

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The Alertmanager convert API is in public preview behind a feature toggle,
// so every command in the alertmanager-imports subtree is experimental. See
// docs/design/experimental-commands.md.
const experimentalPreamble = `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.`

// promoteDefinition defines the domain verb promote, as
// docs/design/command-naming.md requires for a domain verb.
const promoteDefinition = `To promote a staged configuration is to merge it permanently into the Grafana
Alertmanager configuration. Grafana adds its routing tree, receivers, time
intervals, templates and inhibition rules as editable Grafana-managed
resources, with the renames that the result shows, and removes the staged
configuration. After a promotion, the imported resources are normal Grafana
resources. You cannot revert a promotion: to remove the resources, delete them
one by one.`

// alertmanagerImportKind is the mutation target kind of an imported
// Alertmanager configuration.
const alertmanagerImportKind = "alertmanager-import"

// Mutation actions of the import commands.
const (
	alertmanagerImportValidated = "validated"
	alertmanagerImportStaged    = "staged"
	alertmanagerImportPromoted  = "promoted"
	alertmanagerImportDeleted   = "deleted"
)

func experimentalAnnotations() map[string]string {
	return map[string]string{agent.AnnotationStability: agent.StabilityExperimental}
}

// alertmanagerImportsCommands returns the alertmanager-imports command group.
func alertmanagerImportsCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alertmanager-imports",
		Short: "[experimental] Import Alertmanager configurations into Grafana-managed alerting.",
		Long: experimentalPreamble + `

Import a Prometheus, Mimir or Cortex Alertmanager configuration into
Grafana-managed alerting with Grafana's Alertmanager convert API.

An import has two stages. First, Grafana stages the configuration under an
identifier and runs it next to the Grafana configuration. Its resources are
read-only. Then you promote the staged configuration, and Grafana merges it
permanently into the Grafana configuration. You can delete a staged
configuration.

` + promoteDefinition + `

Grafana keeps one staged configuration per organization. The API requires the
alertingImportAlertmanagerAPI feature toggle.`,
		Annotations: experimentalAnnotations(),
	}
	cmd.AddCommand(
		newAlertmanagerImportUpsertCommand(loader),
		newAlertmanagerImportGetCommand(loader),
		newAlertmanagerImportPromoteCommand(loader),
		newAlertmanagerImportDeleteCommand(loader),
	)
	return cmd
}

// alertmanagerImportResult is the result of an import or a promotion.
type alertmanagerImportResult struct {
	cmdio.SingleMutation `json:",inline" yaml:",inline"`

	Identifier      string                 `json:"identifier" yaml:"identifier"`
	Promoted        bool                   `json:"promoted" yaml:"promoted"`
	Stats           AlertmanagerMergeStats `json:"stats" yaml:"stats"`
	RenameResources AlertmanagerRenames    `json:"rename_resources" yaml:"rename_resources"`
}

// newAlertmanagerImportResult builds the result from the server response. It
// gives empty collections instead of null, so the result shape is stable.
func newAlertmanagerImportResult(action, identifier string, merge *AlertmanagerMergeResult) alertmanagerImportResult {
	result := alertmanagerImportResult{
		SingleMutation: cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: alertmanagerImportKind, Name: identifier}),
		Identifier:     identifier,
		Promoted:       action == alertmanagerImportPromoted,
	}
	result.DryRun = action == alertmanagerImportValidated
	if merge.Stats != nil {
		result.Stats = *merge.Stats
	}
	if merge.RenameResources != nil {
		result.RenameResources = *merge.RenameResources
	}

	stats := &result.Stats
	for _, list := range []*[]string{
		&stats.AddedReceivers, &stats.AddedTimeIntervals, &stats.AddedTemplates, &stats.AddedInhibitionRules,
	} {
		if *list == nil {
			*list = []string{}
		}
	}
	renames := &result.RenameResources
	for _, names := range []*map[string]string{&renames.Receivers, &renames.TimeIntervals} {
		if *names == nil {
			*names = map[string]string{}
		}
	}
	return result
}

// alertmanagerImportTextCodec renders an import or promotion result for
// humans: the outcome, the added resources, and the renamed resources.
type alertmanagerImportTextCodec struct{}

func (alertmanagerImportTextCodec) Format() format.Format { return "text" }

func (alertmanagerImportTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (alertmanagerImportTextCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(alertmanagerImportResult)
	if !ok {
		return fmt.Errorf("alertmanager import: unsupported type %T", v)
	}

	added := "Added resources"
	switch r.Action {
	case alertmanagerImportValidated:
		cmdio.Success(w, "Dry run: Alertmanager configuration %q is valid. Nothing was saved.", r.Identifier)
		added = "Resources that the import would add"
	case alertmanagerImportPromoted:
		cmdio.Success(w, "Promoted Alertmanager configuration %q into the Grafana configuration", r.Identifier)
	default:
		cmdio.Success(w, "Staged Alertmanager configuration %q", r.Identifier)
	}

	fmt.Fprintf(w, "\n%s:\n", added)
	rows := []struct {
		label string
		names []string
	}{
		{"Route", nonEmpty(r.Stats.AddedRoute)},
		{"Receivers", r.Stats.AddedReceivers},
		{"Time intervals", r.Stats.AddedTimeIntervals},
		{"Templates", r.Stats.AddedTemplates},
		{"Inhibition rules", r.Stats.AddedInhibitionRules},
	}
	for _, row := range rows {
		fmt.Fprintf(w, "  %-18s%s\n", row.label+":", joinNames(row.names))
	}

	fmt.Fprintln(w, "\nRenamed resources (old -> new):")
	renamed := writeRenames(w, "receiver", r.RenameResources.Receivers)
	renamed = writeRenames(w, "time interval", r.RenameResources.TimeIntervals) || renamed
	if !renamed {
		fmt.Fprintln(w, "  none")
	}
	return nil
}

// writeRenames writes one line per renamed resource, sorted by the old name,
// and reports whether it wrote a line.
func writeRenames(w io.Writer, kind string, renames map[string]string) bool {
	for _, oldName := range slices.Sorted(maps.Keys(renames)) {
		fmt.Fprintf(w, "  %s %s -> %s\n", kind, oldName, renames[oldName])
	}
	return len(renames) > 0
}

func nonEmpty(name string) []string {
	if name == "" {
		return nil
	}
	return []string{name}
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// ---------------------------------------------------------------------------
// upsert
// ---------------------------------------------------------------------------

type alertmanagerImportUpsertOpts struct {
	IO           cmdio.Options
	File         string
	Templates    []string
	Identifier   string
	DryRun       bool
	Promote      bool
	ForceReplace bool
	Force        bool
}

func (o *alertmanagerImportUpsertOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", alertmanagerImportTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "",
		"File containing the Alertmanager configuration or the Mimir/Cortex user configuration (YAML/JSON, use - for stdin)")
	flags.StringArrayVar(&o.Templates, "template", nil,
		"Template file to import; the template name is the file name (repeatable; directories are not accepted)")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Validate the configuration and report the changes without saving them")
	flags.BoolVar(&o.Promote, "promote", false,
		"Merge the configuration permanently into the Grafana configuration instead of staging it (cannot be undone)")
	flags.BoolVar(&o.ForceReplace, "force-replace", false, "Replace a staged configuration that has another identifier")
	flags.BoolVar(&o.Force, "force", false, "Skip the confirmation prompt of --promote")
}

func (o *alertmanagerImportUpsertOpts) Validate(cmd *cobra.Command) error {
	if o.File == "" {
		return gcxerrors.NewCommandUsageError(cmd, "--filename / -f is required (use - to read from stdin)", nil)
	}
	if err := validateAlertmanagerImportIdentifier(cmd, o.Identifier); err != nil {
		return err
	}
	return o.IO.Validate()
}

func (o *alertmanagerImportUpsertOpts) action() string {
	switch {
	case o.DryRun:
		return alertmanagerImportValidated
	case o.Promote:
		return alertmanagerImportPromoted
	default:
		return alertmanagerImportStaged
	}
}

func newAlertmanagerImportUpsertCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &alertmanagerImportUpsertOpts{}
	cmd := &cobra.Command{
		Use:   "upsert [IDENTIFIER]",
		Short: "[experimental] Stage or replace an imported Alertmanager configuration.",
		Long: experimentalPreamble + `

Stage an Alertmanager configuration for import into Grafana-managed alerting
under the identifier (default "imported"). The identifier is also the name of
the imported routing tree. If a staged configuration has the same identifier,
Grafana replaces it.

The input is one of these formats:
  - A Mimir or Cortex user configuration, with alertmanager_config (the
    Alertmanager configuration as a string) and optional template_files. This
    is the response of the Mimir Alertmanager API GET /api/v1/alerts.
  - A plain Alertmanager configuration (route, receivers, ...). gcx sends the
    file content unchanged.

Use --template to add a template file. Repeat the flag to add more files. Each
template is named after its file. A template name must occur only one time.

This command takes the same configuration and template files as
"mimirtool alertmanager load", for example the files that
"mimirtool alertmanager get --output-dir DIR" writes (DIR/config.yaml and
the template files next to it). Pass each template file in DIR, but not
DIR/config.yaml.

Grafana renames incoming receivers and time intervals whose names are already
in use, and the result shows each rename. Use --dry-run to see the changes
before you save. Use --promote to merge the configuration permanently into the
Grafana configuration in one step. You cannot undo a promotion, so --promote
without --dry-run asks for confirmation. Use --force to skip the prompt; agent
mode requires it, and so does input from stdin.`,
		Example: `  # Validate a plain Alertmanager configuration without saving it
  gcx alert alertmanager-imports upsert -f alertmanager.yaml --template templates/email.tmpl --dry-run

  # Stage the configuration under the identifier mimir-prod with two templates
  gcx alert alertmanager-imports upsert mimir-prod -f alertmanager.yaml \
    --template templates/email.tmpl --template templates/slack.tmpl

  # The files come from mimirtool alertmanager get --output-dir out
  gcx alert alertmanager-imports upsert mimir-prod -f out/config.yaml --template out/email.tmpl --template out/slack.tmpl

  # Stage a Mimir user configuration read from stdin
  curl -s -H "X-Scope-OrgID: <tenant>" https://mimir.example.com/api/v1/alerts \
    | gcx alert alertmanager-imports upsert mimir-prod -f -

  # Replace a staged configuration that has another identifier
  gcx alert alertmanager-imports upsert mimir-prod -f alertmanager.yaml --force-replace

  # Stage and promote in one step, without a confirmation prompt
  gcx alert alertmanager-imports upsert mimir-prod -f alertmanager.yaml --promote --force`,
		Args:        cobra.MaximumNArgs(1),
		Annotations: experimentalAnnotations(),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Identifier = defaultAlertmanagerImportIdentifier
			if len(args) == 1 {
				opts.Identifier = args[0]
			}
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			userConfig, err := readAlertmanagerUserConfig(opts.File, cmd.InOrStdin(), opts.Templates)
			if err != nil {
				return err
			}
			if opts.Promote && !opts.DryRun {
				// The confirmation exchange is a diagnostic, not the result.
				ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
					promotePrompt(opts.Identifier))
				if err != nil {
					return err
				}
				if !ok {
					return cancelledAlertmanagerImport("upsert --promote", opts.Identifier)
				}
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			merge, err := client.ImportAlertmanagerConfig(ctx, userConfig, AlertmanagerImportOptions{
				Identifier:   opts.Identifier,
				DryRun:       opts.DryRun,
				Promote:      opts.Promote,
				ForceReplace: opts.ForceReplace,
			})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), newAlertmanagerImportResult(opts.action(), opts.Identifier, merge))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

type alertmanagerImportGetOpts struct {
	IO         cmdio.Options
	tableCodec *alertmanagerImportTableCodec
	wideCodec  *alertmanagerImportTableCodec
}

func (o *alertmanagerImportGetOpts) setup(flags *pflag.FlagSet) {
	// JSON and YAML keep the Mimir user configuration shape. The table codecs
	// render the same sanitized data for inspection: Grafana masks secrets and
	// removes the global section (GetSanitizedAlertmanagerConfigYAML).
	o.tableCodec = &alertmanagerImportTableCodec{}
	o.wideCodec = &alertmanagerImportTableCodec{wide: true}
	o.IO.RegisterCustomCodec("table", o.tableCodec)
	o.IO.RegisterCustomCodec("wide", o.wideCodec)
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *alertmanagerImportGetOpts) Validate() error {
	return o.IO.Validate()
}

func newAlertmanagerImportGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &alertmanagerImportGetOpts{}
	cmd := &cobra.Command{
		Use:   "get [IDENTIFIER]",
		Short: "[experimental] Get a staged Alertmanager configuration.",
		Long: experimentalPreamble + `

Get the staged Alertmanager configuration that has the identifier (default
"imported"). The default table summarizes its routes, receivers, time intervals,
inhibition rules and templates. Use -o yaml or -o json to inspect the sanitized
Mimir user configuration fields: alertmanager_config and template_files.

The output is for inspection only. Do not use it as the input of upsert:
Grafana replaces each secret with <secret> and removes the global section of
the Alertmanager configuration, so an import of the output would lose them.`,
		Example: `  # Summarize the staged configuration with the default identifier
  gcx alert alertmanager-imports get

  # Inspect the sanitized configuration mimir-prod as YAML
  gcx alert alertmanager-imports get mimir-prod -o yaml

  # Get the sanitized configuration as JSON
  gcx alert alertmanager-imports get mimir-prod -o json`,
		Args:        cobra.MaximumNArgs(1),
		Annotations: experimentalAnnotations(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			identifier := defaultAlertmanagerImportIdentifier
			if len(args) == 1 {
				identifier = args[0]
			}
			if err := validateAlertmanagerImportIdentifier(cmd, identifier); err != nil {
				return err
			}
			opts.tableCodec.identifier = identifier
			opts.wideCodec.identifier = identifier
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			userConfig, err := client.GetImportedAlertmanagerConfig(ctx, identifier)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), userConfig)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// promote
// ---------------------------------------------------------------------------

type alertmanagerImportPromoteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *alertmanagerImportPromoteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	o.IO.RegisterCustomCodec("text", alertmanagerImportTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *alertmanagerImportPromoteOpts) Validate() error {
	return o.IO.Validate()
}

func newAlertmanagerImportPromoteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &alertmanagerImportPromoteOpts{}
	cmd := &cobra.Command{
		Use:   "promote IDENTIFIER",
		Short: "[experimental] Merge a staged Alertmanager configuration into the Grafana configuration.",
		Long: experimentalPreamble + `

Promote the staged Alertmanager configuration that has the identifier.

` + promoteDefinition + `

Because you cannot undo a promotion, the command asks for confirmation. Use
--force to skip the prompt; agent mode requires it.`,
		Example: `  # Promote the staged configuration mimir-prod
  gcx alert alertmanager-imports promote mimir-prod

  # Promote without a confirmation prompt
  gcx alert alertmanager-imports promote mimir-prod --force`,
		Args:        cobra.ExactArgs(1),
		Annotations: experimentalAnnotations(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			identifier := args[0]
			if err := validateAlertmanagerImportIdentifier(cmd, identifier); err != nil {
				return err
			}
			// The confirmation exchange is a diagnostic, not the result.
			ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				promotePrompt(identifier))
			if err != nil {
				return err
			}
			if !ok {
				return cancelledAlertmanagerImport("promote", identifier)
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			merge, err := client.PromoteImportedAlertmanagerConfig(ctx, identifier)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), newAlertmanagerImportResult(alertmanagerImportPromoted, identifier, merge))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

type alertmanagerImportDeleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *alertmanagerImportDeleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{line: func(m cmdio.SingleMutation) string {
		return fmt.Sprintf("Deleted staged Alertmanager configuration %q", m.Target.Name)
	}})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *alertmanagerImportDeleteOpts) Validate() error {
	return o.IO.Validate()
}

func newAlertmanagerImportDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &alertmanagerImportDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete IDENTIFIER",
		Short: "[experimental] Delete a staged Alertmanager configuration.",
		Long: experimentalPreamble + `

Delete the staged Alertmanager configuration that has the identifier. The
Grafana configuration does not change. If no staged configuration has the
identifier, the command also succeeds. The command asks for confirmation. Use
--force to skip the prompt; agent mode requires it.`,
		Example: `  # Delete the staged configuration mimir-prod
  gcx alert alertmanager-imports delete mimir-prod

  # Delete without a confirmation prompt
  gcx alert alertmanager-imports delete mimir-prod --force`,
		Args:        cobra.ExactArgs(1),
		Annotations: experimentalAnnotations(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			identifier := args[0]
			if err := validateAlertmanagerImportIdentifier(cmd, identifier); err != nil {
				return err
			}
			// The confirmation exchange is a diagnostic, not the result.
			ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete staged Alertmanager configuration %q?", identifier))
			if err != nil {
				return err
			}
			if !ok {
				return cancelledAlertmanagerImport("delete", identifier)
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			if err := client.DeleteImportedAlertmanagerConfig(ctx, identifier); err != nil {
				return err
			}
			result := cmdio.NewSingleMutation(alertmanagerImportDeleted,
				cmdio.MutationTarget{Kind: alertmanagerImportKind, Name: identifier})
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func validateAlertmanagerImportIdentifier(cmd *cobra.Command, identifier string) error {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return gcxerrors.NewCommandUsageError(cmd, "IDENTIFIER must not be empty", nil)
	}
	if identifier != trimmed {
		return gcxerrors.NewCommandUsageError(cmd, "IDENTIFIER must not have leading or trailing whitespace", nil)
	}
	return nil
}

// promotePrompt is the confirmation question of a promotion.
func promotePrompt(identifier string) string {
	return fmt.Sprintf("Promote Alertmanager configuration %q into the Grafana configuration? You cannot undo this.", identifier)
}

// cancelledAlertmanagerImport reports a declined confirmation with the
// cancelled exit code.
func cancelledAlertmanagerImport(operation, identifier string) error {
	return &gcxerrors.DetailedError{
		Summary:  operation + " cancelled",
		Details:  fmt.Sprintf("Confirmation prompt was declined; Alertmanager configuration %q was not changed.", identifier),
		ExitCode: new(gcxerrors.ExitCancelled),
	}
}
