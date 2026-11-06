package irm

import (
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type messageShareOpts struct {
	IO                 cmdio.Options
	Body               string
	See                string
	DraftedByAgent     string
	DraftedByAgentName string
}

func (o *messageShareOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Body, "body", "", "Message to share with the incident (required)")
	flags.StringVar(&o.See, "see", "", "Optional reference appended as a See: line")
	flags.StringVar(&o.DraftedByAgent, "drafted-by-agent", "", "Agent ID that drafted the message; empty means the authenticated user authored it")
	flags.StringVar(&o.DraftedByAgentName, "drafted-by-agent-name", "", "Agent display name; defaults to --drafted-by-agent")
	setupOpModelReadIO(&o.IO, flags)
}

func (o *messageShareOpts) Validate() error {
	if strings.TrimSpace(o.DraftedByAgentName) != "" && strings.TrimSpace(o.DraftedByAgent) == "" {
		exitCode := fail.ExitUsageError
		return &fail.DetailedError{Summary: "--drafted-by-agent-name requires --drafted-by-agent", ExitCode: &exitCode}
	}
	return requireOpModelFlag("body", o.Body)
}

type messageReceipt struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	IncidentID    string `json:"incident_id"`
	MessageID     string `json:"message_id"`
}

func newIncidentShareCmd(loader SharingClientLoader) *cobra.Command {
	opts := &messageShareOpts{}
	shared := &opModelSharedOpts{}
	cmd := &cobra.Command{
		Use:    "share <incident-id>",
		Hidden: true,
		Short:  "Share a message with an incident.",
		Long: `Save a message as an Incident status update attributed to the authenticated
Grafana user. No operational model or model entities are required.

Requires an OAuth user login through gcx login and the experimental IRM sharing
API. Service accounts cannot share messages.

Do not retry after an ambiguous timeout: the message may already have been
saved, and delivery has no idempotency key.`,
		Example: `  gcx irm incidents share INC-1 --body "The service has recovered"
  gcx irm incidents share INC-1 --body "Database connections are exhausted" --see "dashboard:db"
  gcx irm incidents share INC-1 --body "The deploy introduced the regression" --drafted-by-agent codex --drafted-by-agent-name Codex`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runIncidentShare(cmd, args[0], opts, loader, shared)
		},
	}
	shared.bind(cmd.Flags())
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runIncidentShare(cmd *cobra.Command, incidentID string, opts *messageShareOpts, loader SharingClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "incident-id", incidentID, opts); err != nil {
		return err
	}
	if err := loader.RequireSharingOAuth(cmd.Context()); err != nil {
		return err
	}
	api, err := loader.LoadMessages(cmd.Context(), shared.Timeout)
	if err != nil {
		return err
	}
	var draftedBy *messageAgent
	if id := strings.TrimSpace(opts.DraftedByAgent); id != "" {
		name := strings.TrimSpace(opts.DraftedByAgentName)
		if name == "" {
			name = id
		}
		draftedBy = &messageAgent{ID: id, DisplayName: name}
	}
	response, err := api.CreateMessage(cmd.Context(), createMessageRequest{
		IncidentID: incidentID, DraftedBy: draftedBy, Body: opts.Body, See: opts.See,
	})
	if err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(), messageReceipt{Type: "gcx.irm.message_receipt", SchemaVersion: "1", IncidentID: incidentID, MessageID: response.MessageID})
}
