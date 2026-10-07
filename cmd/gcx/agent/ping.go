package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/agentping"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type pingOpts struct {
	IO      cmdio.Options
	Message agentping.Message
}

func (o *pingOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &pingTextCodec{})
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Message.Text, "text", "", "Notification text as plain text (required)")
	flags.StringVar(&o.Message.Body, "body", "", "Additional notification body as plain text (optional)")
	flags.StringVar(&o.Message.Inbox, "inbox", "agents", "Notification inbox (only agents is supported)")
	flags.StringVar(&o.Message.Title, "title", "Agent ping", "Notification title as plain text")
	flags.StringVar(&o.Message.Host, "host", "", "Source machine name as plain text (default: local hostname)")
	flags.StringVar(&o.Message.Agent, "agent-name", "gcx", "Name of the agent sending the notification, as plain text")
}

func (o *pingOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Message.Inbox != "agents" {
		return fmt.Errorf("invalid --inbox %q: only agents is supported; use --inbox agents", o.Message.Inbox)
	}
	for _, field := range []struct{ name, value string }{
		{"--text", o.Message.Text}, {"--title", o.Message.Title},
		{"--host", o.Message.Host}, {"--agent-name", o.Message.Agent},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s must not be empty; use gcx agent ping --text 'Ready for review'", field.name)
		}
	}
	return nil
}

func pingCommand() *cobra.Command {
	opts := &pingOpts{}
	configOpts := &cmdconfig.Options{}
	cmd := &cobra.Command{
		Use:   "ping",
		Short: "[experimental] Send a notification to your paired phone",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Send a one-way notification to the authenticated user's paired Grafana mobile app.
Set the payload fields with --text (required), --body, --title, --host, and
--agent-name. All message fields are plain text strings. --text and --body set
separate JSON fields; an omitted body is not sent.
Agent metadata requires the Agents inbox, so --inbox defaults to agents (the
only supported value). The backend uses text as the body when body is omitted.
The --agent-name flag sets the JSON agent field; the global --agent flag enables
agent mode. The command constructs and escapes the JSON request automatically.
Sign in with gcx login using your user identity, and connect the mobile app to
the same Grafana stack and user. gcx cloud login and service account tokens do
not provide the user identity required by this endpoint.

Success means Grafana accepted the request, not that the phone received or read
it. This command does not wait for a reply.`,
		Example: `  gcx agent ping --text "Tests passed. Ready for review."
  gcx agent ping --text "Which deployment target should I use?" --title "Input needed"
  gcx agent ping --text "Build finished" --body "All tests passed. Return to the session for details." --agent-name codex --context my-context`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationStability: agent.StabilityExperimental,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("host") {
				host, err := os.Hostname()
				if err != nil {
					return fmt.Errorf("resolve hostname: %w; supply --host explicitly", err)
				}
				opts.Message.Host = host
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			cfg, err := configOpts.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			if err := agentping.Send(cmd.Context(), cfg, opts.Message); err != nil {
				return err
			}
			result := cmdio.NewSingleMutation("accepted", cmdio.MutationTarget{Kind: "mobile_notification", Name: "self"})
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	configOpts.BindFlags(cmd.PersistentFlags())
	opts.setup(cmd.Flags())
	return cmd
}

type pingTextCodec struct{}

func (*pingTextCodec) Format() format.Format { return "text" }

func (*pingTextCodec) Decode(io.Reader, any) error {
	return errors.New("ping text codec does not support decoding")
}

func (*pingTextCodec) Encode(w io.Writer, value any) error {
	if _, ok := value.(cmdio.SingleMutation); !ok {
		return errors.New("invalid data type for ping text codec: expected SingleMutation")
	}
	_, err := fmt.Fprintln(w, "Ping accepted by Grafana")
	return err
}
