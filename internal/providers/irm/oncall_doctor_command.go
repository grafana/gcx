package irm

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
)

// Verdict palette (mirrors the shared threshold colors).
var (
	colorOK    = lipgloss.Color("#7EB26D") // green
	colorBad   = lipgloss.Color("#E24D42") // red
	colorWarn  = lipgloss.Color("#EAB839") // amber
	colorMuted = lipgloss.Color("#8E8E8E") // gray
)

// paint colors text when styling is enabled, otherwise returns it unchanged.
func paint(text string, color lipgloss.Color, bold bool) string {
	if !style.IsStylingEnabled() {
		return text
	}
	s := lipgloss.NewStyle().Foreground(color)
	if bold {
		s = s.Bold(true)
	}
	return s.Render(text)
}

// ---------------------------------------------------------------------------
// doctor command — "check your own config vs the expected configuration"
//
// Resolves the current user via the plugin-proxy (GetCurrentUser on the active gcx
// context), then looks that user up in the org compliance evaluation. The evaluation
// itself lives only on the OnCall public API today, so doctor reuses the same TEST-ONLY
// direct transport as `compliance-rules evaluate` (--oncall-url/--oncall-token/--instance-id).
// ---------------------------------------------------------------------------

type doctorOpts struct {
	IO        cmdio.Options
	transport complianceTransport
	UserID    string
}

func newDoctorCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &doctorOpts{}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check whether your OnCall notification setup meets the org's compliance rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.transport.URL == "" {
				return errors.New("doctor requires --oncall-url (test-only) until the endpoint is exposed via the plugin proxy")
			}

			userID := opts.UserID
			if userID == "" {
				client, _, err := loader.LoadOnCallClient(cmd.Context())
				if err != nil {
					return fmt.Errorf("resolve current user: %w", err)
				}
				u, err := client.GetCurrentUser(cmd.Context())
				if err != nil {
					return fmt.Errorf("resolve current user: %w", err)
				}
				userID = u.PK
			}

			public := newPublicComplianceClient(opts.transport.URL, opts.transport.Token, opts.transport.InstanceID)
			eval, err := public.Evaluate(cmd.Context())
			if err != nil {
				return err
			}

			res := doctorResultFor(userID, eval)
			res.Self = opts.UserID == "" // resolved from the current user, not an explicit --user
			if users, uerr := public.ListUsers(cmd.Context()); uerr == nil {
				if info, ok := users[userID]; ok {
					res.Email = info.Email
					res.Username = info.Username
				}
			}
			return opts.IO.Encode(cmd.OutOrStdout(), res)
		},
	}
	registerComplianceCodecs(&opts.IO, cmd.Flags(), &doctorTextCodec{})
	opts.transport.bind(cmd.Flags())
	cmd.Flags().StringVar(&opts.UserID, "user", "", "OnCall user ID to check (defaults to the current user)")
	return cmd
}

// DoctorResult is one user's compliance verdict.
type DoctorResult struct {
	UserID     string   `json:"user_id"`
	Email      string   `json:"email,omitempty"`
	Username   string   `json:"username,omitempty"`
	Compliant  bool     `json:"compliant"`
	Found      bool     `json:"found"`
	Violations []string `json:"violations,omitempty"`
	Self       bool     `json:"-"` // true when resolved from the current user (drives "you" phrasing)
}

// name returns the best display name: email, then username, then the ID.
func (r DoctorResult) name() string {
	switch {
	case r.Email != "":
		return r.Email
	case r.Username != "":
		return r.Username
	default:
		return r.UserID
	}
}

// label renders "name (user_id)", preferring email, then username, then just the ID.
func (r DoctorResult) label() string {
	if n := r.name(); n != r.UserID {
		return fmt.Sprintf("%s (%s)", n, r.UserID)
	}
	return r.UserID
}

// doctorResultFor finds userID in the evaluation report. A user absent from both lists
// is reported as not found (the backend may omit users with no notification setup).
func doctorResultFor(userID string, e *ComplianceEvaluation) DoctorResult {
	if slices.Contains(e.Compliant, userID) {
		return DoctorResult{UserID: userID, Compliant: true, Found: true}
	}
	for _, u := range e.NonCompliant {
		if u.UserID == userID {
			return DoctorResult{UserID: userID, Compliant: false, Found: true, Violations: u.Violations}
		}
	}
	return DoctorResult{UserID: userID, Compliant: false, Found: false}
}

// doctorTextCodec renders a DoctorResult as a human verdict.
type doctorTextCodec struct{}

func (c *doctorTextCodec) Format() format.Format { return format.Format("text") }

func (c *doctorTextCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(DoctorResult)
	if !ok {
		return fmt.Errorf("text codec: unsupported value type %T (expected DoctorResult)", v)
	}

	subject := r.label()
	if r.Self {
		subject = "You"
		if n := r.name(); n != r.UserID {
			subject = "You (" + n + ")"
		}
	}

	switch {
	case !r.Found:
		head := "no notification setup found"
		if r.Self {
			head = "we couldn't find your notification setup"
		}
		fmt.Fprintln(w, paint("⚠ "+subject+" — "+head, colorWarn, true))
		detail := "This user doesn't appear in the org compliance report — they may have no notification policy at all."
		if r.Self {
			detail = "You don't appear in the org compliance report — you may have no notification policy at all. Set one up so pages can reach you."
		}
		fmt.Fprintln(w, paint("  "+detail, colorMuted, false))
		return nil

	case r.Compliant:
		msg := subject + " is compliant — ready to be paged."
		if r.Self {
			msg = "You're all set — ready to be paged."
		}
		fmt.Fprintln(w, paint("✓ "+msg, colorOK, true))
		fmt.Fprintln(w, paint("  Your notification setup meets all of the org's requirements.", colorMuted, false))
		return nil

	default:
		head := subject + " is NOT compliant with the org's notification rules:"
		if r.Self {
			head = "You're not fully reachable yet — your setup is missing some requirements:"
		}
		fmt.Fprintln(w, paint("✗ "+head, colorBad, true))
		for _, viol := range r.Violations {
			fmt.Fprintln(w, "  "+paint("•", colorBad, false)+" "+paint(friendlyViolation(viol), colorWarn, false))
		}
		if r.Self {
			fmt.Fprintln(w, paint("  Fix these in your OnCall notification settings so alerts reach you.", colorMuted, false))
		}
		return nil
	}
}

func (c *doctorTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// friendlyViolation replaces raw enum tokens (notify_by_*, channel names) inside a
// backend violation sentence with their friendly labels, leaving everything else intact.
func friendlyViolation(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		if l, ok := complianceLabels[f]; ok {
			fields[i] = l
		}
	}
	return strings.Join(fields, " ")
}
