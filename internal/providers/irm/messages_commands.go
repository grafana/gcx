package irm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type messageShareOpts struct {
	IO                 cmdio.Options
	Body               string
	BodyFile           string
	See                string
	DraftedByAgent     string
	DraftedByAgentName string
	DryRun             bool
	ExpectDigest       string
	flags              *pflag.FlagSet
}

func (o *messageShareOpts) setup(flags *pflag.FlagSet) {
	o.flags = flags
	flags.StringVar(&o.Body, "body", "", "Message to share with the incident; give exactly one of --body or --body-file")
	flags.StringVar(&o.BodyFile, "body-file", "", "Read the message from a file (- for standard input); trailing line breaks are removed")
	flags.StringVar(&o.See, "see", "", "Optional pointer appended as a See: line; give the pointer only, gcx adds the See: prefix")
	flags.StringVar(&o.DraftedByAgent, "drafted-by-agent", "", "Agent ID that drafted the message for the signed-in user; omit when the user wrote it")
	flags.StringVar(&o.DraftedByAgentName, "drafted-by-agent-name", "", "Agent display name; defaults to --drafted-by-agent")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Validate and preview the message body and See: line without sending it")
	flags.StringVar(&o.ExpectDigest, "expect-digest", "", "Refuse to send unless the message and destination match this digest from --dry-run")
	setupOpMemReadIO(&o.IO, flags)
}

func (o *messageShareOpts) Validate() error {
	if strings.TrimSpace(o.DraftedByAgentName) != "" && strings.TrimSpace(o.DraftedByAgent) == "" {
		exitCode := fail.ExitUsageError
		return &fail.DetailedError{Summary: "--drafted-by-agent-name requires --drafted-by-agent", ExitCode: &exitCode}
	}
	bodyFile := o.flags.Changed("body-file")
	switch {
	case bodyFile && o.flags.Changed("body"):
		return opMemUsage("use only one of --body or --body-file")
	case bodyFile && o.BodyFile == "":
		return opMemUsage("--body-file must not be empty; give a path, or - for standard input")
	case !bodyFile && !o.flags.Changed("body"):
		return opMemUsage("one of --body or --body-file is required")
	}
	if o.flags.Changed("expect-digest") && strings.TrimSpace(o.ExpectDigest) == "" {
		return opMemUsage("--expect-digest must not be blank; give the digest printed by --dry-run")
	}
	if o.See == "" {
		return nil
	}
	if strings.TrimSpace(o.See) == "" {
		return opMemUsage("--see must not be blank; omit it when there is no pointer")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(o.See)), "see:") {
		exitCode := fail.ExitUsageError
		return &fail.DetailedError{
			Summary:     "--see takes the pointer only; gcx adds the See: prefix",
			ExitCode:    &exitCode,
			Suggestions: []string{"Re-run with --see <pointer> without the See: prefix"},
		}
	}
	return nil
}

func (o *messageShareOpts) messageBody(stdin io.Reader) (string, error) {
	body := o.Body
	if o.flags.Changed("body-file") {
		var raw []byte
		var err error
		if o.BodyFile == "-" {
			raw, err = io.ReadAll(stdin)
		} else {
			raw, err = os.ReadFile(o.BodyFile)
		}
		if err != nil {
			exitCode := fail.ExitUsageError
			return "", &fail.DetailedError{Summary: fmt.Sprintf("cannot read --body-file %q", o.BodyFile), Details: err.Error(), ExitCode: &exitCode}
		}
		body = strings.TrimRight(string(raw), "\r\n")
	}
	if strings.TrimSpace(body) == "" {
		return "", opMemUsage("the message body must not be blank")
	}
	return body, nil
}

// messageEnvelope is what a share sends and where it sends it. The preview
// digest and the send check both hash this one value.
type messageEnvelope struct {
	Context       string        `json:"context"`
	Server        string        `json:"server"`
	ProxyEndpoint string        `json:"proxy_endpoint"`
	IncidentID    string        `json:"incident_id"`
	Body          string        `json:"body"`
	See           string        `json:"see,omitempty"`
	DraftedBy     *messageAgent `json:"drafted_by,omitempty"`
}

func (o *messageShareOpts) envelope(dest messageDestination, incidentID, body string) messageEnvelope {
	var draftedBy *messageAgent
	if id := strings.TrimSpace(o.DraftedByAgent); id != "" {
		name := strings.TrimSpace(o.DraftedByAgentName)
		if name == "" {
			name = id
		}
		draftedBy = &messageAgent{ID: id, DisplayName: name}
	}
	return messageEnvelope{Context: dest.Context, Server: dest.Server, ProxyEndpoint: dest.ProxyEndpoint, IncidentID: incidentID, Body: body, See: o.See, DraftedBy: draftedBy}
}

func (e messageEnvelope) digest() string {
	encoded, _ := json.Marshal(e) //nolint:errchkjson // strings and a struct of strings cannot fail to marshal
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (e messageEnvelope) preview() messagePreview {
	text := e.Body
	if e.See != "" {
		text += "\nSee: " + e.See
	}
	return messagePreview{Type: "gcx.irm.message_preview", SchemaVersion: "1", messageEnvelope: e, Preview: text, Digest: e.digest()}
}

type messagePreview struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	//nolint:embeddedstructfieldcheck // declared after type and schema_version so the discriminators serialize first
	messageEnvelope `yaml:",inline"`
	Preview         string `json:"preview"`
	Digest          string `json:"digest"`
}

type messageReceipt struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	IncidentID    string `json:"incident_id"`
	MessageID     string `json:"message_id"`
}

func newIncidentShareCmd(loader SharingClientLoader) *cobra.Command {
	opts := &messageShareOpts{}
	shared := &opMemSharedOpts{}
	cmd := &cobra.Command{
		Use:   "share <incident-id>",
		Short: "Share a message with an incident.",
		Long: `Save a message as an Incident status update. The Grafana user signed in to gcx
through OAuth is the author. When an agent sends the message on that user's
behalf after they approve it, --drafted-by-agent records which agent drafted it.
No operational model or model entities are required.

Requires an OAuth user login through gcx login and the experimental IRM sharing
API. Service accounts cannot share messages.

--dry-run validates the message and the login, sends nothing, and prints a
preview of the message body and optional See: line. The server attributes the
message to the signed-in user and to any agent named by --drafted-by-agent.

The preview also has a digest of the context, server, OAuth proxy endpoint,
incident, body, See: line and drafting agent. Pass it to --expect-digest on the
real send to send exactly what was previewed: if any of them changed, gcx sends
nothing and exits 2.

A real send has no idempotency key. After a failed or timed-out send, run
gcx irm incidents list-activity <incident-id> --kind keyUpdateAdded --limit 100
before sending again. The message may already have been saved.`,
		Example: `  gcx irm incidents share INC-1 --body "The service has recovered"
  gcx irm incidents share INC-1 --body "Database connections are exhausted" --see "dashboard:db"
  gcx irm incidents share INC-1 --body "Database connections are exhausted" --see "dashboard:db" --dry-run
  gcx irm incidents share INC-1 --body-file status-update.txt --dry-run
  gcx irm incidents share INC-1 --body-file status-update.txt --expect-digest sha256:<digest from --dry-run>
  gcx irm incidents share INC-1 --body "The deploy introduced the regression" --drafted-by-agent codex --drafted-by-agent-name Codex`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runIncidentShare(cmd, args[0], opts, loader, shared)
		},
	}
	shared.bind(cmd.Flags(), defaultShareTimeout)
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runIncidentShare(cmd *cobra.Command, incidentID string, opts *messageShareOpts, loader SharingClientLoader, shared *opMemSharedOpts) error {
	if err := validateOpMemIDInputs(shared, "incident-id", incidentID, opts); err != nil {
		return err
	}
	body, err := opts.messageBody(cmd.InOrStdin())
	if err != nil {
		return err
	}
	dest, err := loader.RequireSharingOAuth(cmd.Context())
	if err != nil {
		return err
	}
	envelope := opts.envelope(dest, incidentID, body)
	if digest := envelope.digest(); opts.ExpectDigest != "" && opts.ExpectDigest != digest {
		exitCode := fail.ExitUsageError
		return &fail.DetailedError{
			Summary:     "the message or its destination changed since the preview",
			Details:     "The --expect-digest value does not match the text, See: line, drafting agent, incident, context, server or OAuth proxy endpoint of this send. Nothing was sent.",
			ExitCode:    &exitCode,
			Suggestions: []string{"Run the command again with --dry-run, show the new preview for approval, and pass its digest to --expect-digest"},
		}
	}
	if opts.DryRun {
		return opts.IO.Encode(cmd.OutOrStdout(), envelope.preview())
	}
	api, err := loader.LoadMessages(cmd.Context(), shared.Timeout)
	if err != nil {
		return err
	}
	response, err := api.CreateMessage(cmd.Context(), createMessageRequest{
		IncidentID: envelope.IncidentID, DraftedBy: envelope.DraftedBy, Body: envelope.Body, See: envelope.See,
	})
	if err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(), messageReceipt{Type: "gcx.irm.message_receipt", SchemaVersion: "1", IncidentID: incidentID, MessageID: response.MessageID})
}
