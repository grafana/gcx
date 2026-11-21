package errortracking

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &Provider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

type Provider struct{}

func (p *Provider) Name() string { return providerName }

func (p *Provider) ShortDesc() string { return "Manage Grafana Error Tracking resources." }

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{{Name: "project"}}
}

func (p *Provider) Validate(_ map[string]string) error { return nil }

func (p *Provider) TypedRegistrations() []adapter.Registration { return nil }

func (p *Provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	root := &cobra.Command{
		Use:     "error-tracking",
		Aliases: []string{"et"},
		Short:   p.ShortDesc(),
		Long: `Manage Grafana Error Tracking projects, the errors and events their SDKs
send, and heatmaps (for browser errors from Frontend Observability, use gcx
frontend). Start with projects list, then errors list --project <id> and errors
inspect <error>; projects use remembers a project so --project can be left out.
Projects and heatmaps come as resources with their fields under spec (--json
spec.id,spec.name), and every other list has flat items (--json id,message).`,
		Example: `  # The projects and their IDs.
  gcx error-tracking projects list

  # One project's unresolved errors.
  gcx error-tracking errors list --project 314

  # One error with its stack trace, activity and assignees.
  gcx error-tracking errors inspect 42 --project 314

  # Remember the project, then leave --project out.
  gcx et projects use 314
  gcx et errors list`,
	}
	loader.BindFlags(root.PersistentFlags())

	errorsCmd := group("errors", "Find, read and triage the errors of an Error Tracking project.",
		newErrorsListCommand(loader), newErrorsGetCommand(loader),
		newErrorsUpdateCommand(loader), newErrorsTimelineCommand(loader),
		newErrorsInspectCommand(loader),
		group("events", "Read the events an error received.",
			newErrorsListEventsCommand(loader), newErrorsGetEventCommand(loader)),
		group("fields", "List the custom fields an error's events carry.",
			newErrorsListFieldsCommand(loader)),
		group("activity", "Read what happened to an error: status changes and comments.",
			newErrorsListActivityCommand(loader)),
		group("comments", "Comment on an error, and edit or delete your comments.",
			newCommentCommand(loader, "create"), newCommentCommand(loader, "update"), newCommentCommand(loader, "delete")),
		group("assignees", "List, add and remove the users assigned to an error"+needsLogin+".",
			newErrorsListAssigneesCommand(loader), newErrorsAssigneeCommand(loader, true), newErrorsAssigneeCommand(loader, false)))
	errorsCmd.Aliases = []string{"error"}
	errorsCmd.Long = errorsCmd.Short + `

errors assignees list, add and delete, errors update --reviewed and the
errors list flags --mine, --assigned and --assignee need a signed-in Grafana
user (gcx login), as does users list; a service account token is refused
before the request.`

	root.AddCommand(
		group("projects", "Manage Error Tracking projects.",
			newProjectsListCommand(loader), newProjectsGetCommand(loader), newProjectsUseCommand(loader),
			newProjectsCreateCommand(loader), newProjectsUpdateCommand(loader),
			newProjectsDeleteCommand(loader), newProjectsTimelineCommand(loader),
			group("ingest-keys", "Rotate a project's ingest key, or stop the previous one.",
				newIngestKeyCommand(loader, true), newIngestKeyCommand(loader, false))),
		errorsCmd,
		group("raw-events", "Check what an SDK sent to an Error Tracking project.",
			newRawEventsListCommand(loader), newRawEventsGetCommand(loader)),
		group("releases", "List the releases an Error Tracking project saw, and what broke in one.",
			newReleasesListCommand(loader), newReleasesStatusCommand(loader)),
		group("environments", "List the environments an Error Tracking project saw.",
			newEnvironmentsListCommand(loader)),
		group("transactions", "List the transactions an Error Tracking project's errors happened in.",
			newTransactionsListCommand(loader)),
		group("fields", "List the custom fields an Error Tracking project's events carry.",
			newFieldsListCommand(loader), newFieldsListValuesCommand(loader)),
		group("users", "List the Grafana users of an Error Tracking project.",
			newUsersListCommand(loader)),
		group("heatmaps", "Manage Error Tracking heatmaps: event counts by one custom field across a set of projects.",
			newHeatmapsListCommand(loader), newHeatmapsGetCommand(loader),
			newHeatmapsCreateCommand(loader), newHeatmapsUpdateCommand(loader),
			newHeatmapsDeleteCommand(loader), newHeatmapsReportCommand(loader),
			newHeatmapsBreakdownCommand(loader)),
	)
	agent.MarkExperimental(root)
	return []*cobra.Command{root}
}

func group(use, short string, cmds ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Long: short}
	cmd.AddCommand(cmds...)
	return cmd
}
