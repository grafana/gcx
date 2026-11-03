// Package feedback wires the `gcx feedback` commands.
package feedback

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/feedback"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/telemetry"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

const experimentalLong = "This command is experimental. It may be removed, or its subcommands, " +
	"flags and responses may change without following the normal semantic versioning conventions."

// Command returns the `feedback` area.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "[experimental] Send feedback about gcx to the gcx team.",
		Long: experimentalLong + `

Tell the gcx team what got in your way: a missing command, an error that did not
say what to do next, or a flag you had to guess. Agents can send feedback too.`,
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
		},
	}
	cmd.AddCommand(sendCmd())
	return cmd
}

type sendOpts struct {
	IO     cmdio.Options
	Fields []string
	DryRun bool
}

func (o *sendOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &sendTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.StringArrayVar(&o.Fields, "field", nil, "Add a named field as key=value (repeatable; the last value for a key wins)")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Print the payload that would be sent, after scrubbing, and send nothing")
}

func (o *sendOpts) Validate() error {
	return o.IO.Validate()
}

func sendCmd() *cobra.Command {
	opts := &sendOpts{}
	cmd := &cobra.Command{
		Use:   "send [NOTE|-]",
		Short: "[experimental] Send a note and named fields to the gcx team.",
		Long: experimentalLong + `

Send a free-text note, a set of named fields, or both. Pass "-" to read the note
from stdin. Any field key is accepted. These are the ones the team reads first:

  type      bug, friction or feature
  command   the gcx command involved, for example "alerts list"
  model     the model driving the agent, if any
  task      what you were trying to do
  expected  what you expected to happen
  actual    what happened instead
  impact    what it cost you, for example "gave up" or "worked around it"

Do not include secrets or personal data. Before printing or sending, gcx replaces
tokens, auth headers, every URL (credentials included), stack hostnames, email
addresses, IP addresses and home directory paths with placeholders. Run with --dry-run to see
exactly what would be sent.

The note and fields are sent whenever you run this command. Metadata (gcx
version, OS, architecture, agent name and the anonymous device ID) is attached
only when usage statistics are enabled. No login or configured context is needed.`,
		Example: `  # Send a note
  gcx feedback send "The error didn't say which flag was wrong"

  # Read the note from stdin and classify it
  cat notes.md | gcx feedback send - --field type=friction

  # Describe a missing feature with named fields
  gcx feedback send "alerts list can't filter by label" \
    --field type=feature \
    --field command="alerts list" \
    --field task="find the alerts owned by one team" \
    --field expected="a --label flag" \
    --field actual="unknown flag: --label"

  # Preview the exact payload without sending it
  gcx feedback send "..." --dry-run`,
		Args: cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return runSend(cmd, opts, args)
		},
	}
	cmd.SetFlagErrorFunc(flagError)
	opts.setup(cmd.Flags())
	return cmd
}

// safeFlagError matches a pflag error that names a single plausible long
// flag. pflag quotes the whole token after "--", which for this command can
// be the note itself: `gcx feedback send "--my token is x"`.
var safeFlagError = regexp.MustCompile(`^(?:unknown flag|flag needs an argument): --[A-Za-z0-9][A-Za-z0-9-]*$`)

// flagError keeps flag errors that only name a flag and replaces the rest, so
// no part of the note is ever printed back.
func flagError(cmd *cobra.Command, err error) error {
	msg := err.Error()
	if safeFlagError.MatchString(msg) {
		return fail.NewCommandUsageError(cmd, msg, nil)
	}
	return fail.NewCommandUsageError(cmd,
		"could not parse the flags; to send a note that starts with '-', put -- before it", nil)
}

func runSend(cmd *cobra.Command, opts *sendOpts, args []string) error {
	data, err := collectData(cmd, opts.Fields, args)
	if err != nil {
		return err
	}

	mode := telemetry.ResolveMode(func() string {
		if d := internalconfig.LoadDiagnostics(cmd.Context()); d != nil {
			return d.Telemetry
		}
		return ""
	})

	payload, err := feedback.NewPayload(data, feedback.CollectMetadata(mode))
	if errors.Is(err, feedback.ErrEmpty) {
		return emptyError(cmd)
	}
	if err != nil {
		return err
	}
	body, err := payload.Encode()
	if tooLarge, ok := errors.AsType[*feedback.TooLargeError](err); ok {
		return fail.NewCommandUsageError(cmd, tooLarge.Error()+"; shorten the note or fields", nil)
	}
	if err != nil {
		return err
	}

	if opts.DryRun {
		return opts.IO.Encode(cmd.OutOrStdout(), payload)
	}

	if err := feedback.Send(cmd.Context(), feedback.Endpoint(), body); err != nil {
		return deliveryError(err, payload.FeedbackID)
	}

	result := cmdio.NewSingleMutation("sent", cmdio.MutationTarget{Kind: "feedback", ID: payload.FeedbackID})
	return opts.IO.Encode(cmd.OutOrStdout(), result)
}

// collectData merges the note and --field values into one map. Error
// messages never quote what the user typed: it is the feedback itself.
func collectData(cmd *cobra.Command, fields []string, args []string) (map[string]string, error) {
	data := make(map[string]string, len(fields)+1)
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fail.NewCommandUsageError(cmd, "--field must be in key=value form", nil)
		}
		data[strings.TrimSpace(key)] = value
	}

	if len(args) == 0 {
		return data, nil
	}
	if _, ok := data["note"]; ok {
		return nil, fail.NewCommandUsageError(cmd,
			"the note was given both as an argument and as --field note=; pass it once", nil)
	}

	note := args[0]
	if note == "-" {
		var err error
		if note, err = readStdin(cmd); err != nil {
			return nil, err
		}
	}
	data["note"] = note
	return data, nil
}

func readStdin(cmd *cobra.Command) (string, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return "", fail.NewCommandUsageError(cmd,
			"the note is \"-\" but stdin is a terminal; pipe the note in or pass it as an argument", nil)
	}
	// Reading one byte past the limit is enough to know the input is too
	// large, without buffering an unbounded stream.
	raw, err := io.ReadAll(io.LimitReader(in, feedback.MaxBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("read note from stdin: %w", err)
	}
	if len(raw) > feedback.MaxBodyBytes {
		return "", fail.NewCommandUsageError(cmd,
			fmt.Sprintf("the note on stdin is over the %d byte limit; shorten it", feedback.MaxBodyBytes), nil)
	}
	return strings.TrimSpace(string(raw)), nil
}

func emptyError(cmd *cobra.Command) error {
	return fail.NewCommandUsageError(cmd, "nothing to send; pass a note or at least one --field with a value", nil)
}

// deliveryError maps a send failure to the user-facing error. The feedback ID
// is included so a report can be matched to a stored row later.
func deliveryError(err error, feedbackID string) error {
	idLine := " Feedback ID: " + feedbackID + "."
	if rejected, ok := errors.AsType[*feedback.RejectedError](err); ok {
		details := fmt.Sprintf("Feedback could not be submitted (HTTP %d). Try again later.", rejected.Status)
		if rejected.Status == http.StatusTooManyRequests {
			details = "Feedback could not be submitted (HTTP 429): too many requests were sent. Try again later."
		}
		return &gcxerrors.DetailedError{
			Summary: gcxerrors.SummaryAPIError,
			Details: details + idLine,
		}
	}
	if unknown, ok := errors.AsType[*feedback.UnknownOutcomeError](err); ok {
		return &gcxerrors.DetailedError{
			Summary: gcxerrors.SummaryNetworkError,
			Details: "Could not confirm that the feedback was received. It may have been submitted. gcx did not retry." + idLine,
			Parent:  unknown.Cause,
		}
	}
	return err
}

// sendTextCodec renders the human output: a confirmation line after a send,
// or the indented payload for --dry-run.
type sendTextCodec struct{}

func (c *sendTextCodec) Format() format.Format { return "text" }

func (c *sendTextCodec) Decode(io.Reader, any) error {
	return errors.New("feedback text codec does not support decoding")
}

func (c *sendTextCodec) Encode(w io.Writer, value any) error {
	switch v := value.(type) {
	case cmdio.SingleMutation:
		cmdio.Success(w, "Feedback sent")
		return nil
	case feedback.Payload:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return err
		}
		_, err := w.Write(buf.Bytes())
		return err
	default:
		return fmt.Errorf("feedback text codec: unexpected type %T", value)
	}
}
