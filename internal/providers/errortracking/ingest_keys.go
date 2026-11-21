package errortracking

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type ingestKeyOpts struct {
	IO      cmdio.Options
	Project projectRef
	Grace   string
	Force   bool
	body    map[string]string
}

func (o *ingestKeyOpts) setup(flags *pflag.FlagSet, rotate bool) {
	if rotate {
		flags.StringVar(&o.Grace, "grace", "", "How long the replaced key keeps working: 0s (stop it at once) to 7d; without it, the server picks the grace period")
	}
	flags.BoolVar(&o.Force, "force", false, "Go ahead without asking")
	bindText(&o.IO, flags)
}

func (o *ingestKeyOpts) Validate(cmd *cobra.Command, rotate bool) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if rotate {
		if err := o.grace(cmd); err != nil {
			return err
		}
	}
	return validIO(cmd, &o.IO)
}

func (o *ingestKeyOpts) grace(cmd *cobra.Command) error {
	if err := nonEmpty(cmd, "grace"); err != nil {
		return err
	}
	if cmd.Flags().Changed("grace") {
		d, err := shared.ParseDuration(strings.TrimSpace(o.Grace))
		if err != nil || d < 0 || d > 168*time.Hour {
			return usage(cmd, fmt.Sprintf("--grace %q: use a duration from 0s to 7d", o.Grace))
		}
		o.body = map[string]string{"grace": d.String()}
	}
	return nil
}

// previousKeyActive reports whether the previous ingest key still accepts events.
func previousKeyActive(until *string) bool {
	if until == nil {
		return false
	}
	t, ok := parseTime(*until)
	return !ok || t.After(time.Now())
}

func newIngestKeyCommand(loader *providers.ConfigLoader, rotate bool) *cobra.Command {
	opts := &ingestKeyOpts{}
	cmd := &cobra.Command{
		Use:   "rotate <project>",
		Short: "Give an Error Tracking project a new ingest key and DSN.",
		Long: `Give an Error Tracking project a new ingest key and DSN; the replaced key
keeps working for --grace (0s stops it at once; without it, the server
picks the grace period). The result says until when the old key works; the key
and DSN are not printed, and projects get --json spec.dsn reads the new DSN. gcx asks first; --force
or GCX_AUTO_APPROVE skips the question, agent mode needs one of them, and
declining exits 5. ` + noDryRun,
		Example: `  # Rotate, keeping the old key for a day.
  gcx error-tracking projects ingest-keys rotate 314 --grace 24h

  # A leaked key: rotate and stop the old one at once, without a prompt.
  gcx et projects ingest-keys rotate checkout-api --grace 0s --force

  # As an agent: rotate, then read the new DSN.
  gcx error-tracking projects ingest-keys rotate 314 --force -o json --jq .previousIngestKeyUntil
  gcx error-tracking projects get 314 --json spec.dsn`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Project.value = args[0]
			if err := opts.Validate(cmd, rotate); err != nil {
				return err
			}
			force, err := deleteBypass(cmd, opts.Force)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Project)
			if err != nil {
				return err
			}
			if !rotate || !force {
				before, err := c.getProject(ctx, id)
				if err != nil {
					return err
				}
				c.knowProject(before.ID, before.Name)
				if !rotate && !previousKeyActive(before.PreviousIngestKeyUntil) {
					m := c.mutation("deleted", "IngestKey", 0, false)
					m.text = capital(c.projectShown()) + " has no previous ingest key; nothing to stop"
					return emit(cmd, &opts.IO, m)
				}
			}
			path := routePath("projects", id, "ingestKey", "revokePrevious")
			question, summary := "Stop the previous ingest key of Error Tracking %s now? SDKs still sending with it get refused.", "delete cancelled"
			if rotate {
				path = routePath("projects", id, "ingestKey", "rotate")
				question, summary = "Rotate the ingest key of Error Tracking %s? SDKs need its new DSN.", "rotation cancelled"
			}
			if !force {
				if err := confirm(cmd, fmt.Sprintf(question, c.projectShown()), summary,
					fmt.Sprintf("The prompt was declined; the ingest keys of %s did not change.", c.projectShown())); err != nil {
					return err
				}
			}
			var body any
			if opts.body != nil {
				body = opts.body
			}
			var p project
			if err := c.do(ctx, http.MethodPost, path, nil, body, &p); err != nil {
				return err
			}
			c.knowProject(p.ID, p.Name)
			if !rotate {
				m := c.mutation("deleted", "IngestKey", 0, true)
				m.text = "Stopped the previous ingest key of " + c.projectShown()
				return emit(cmd, &opts.IO, m)
			}
			m := ingestKeyMutation{mutationBase: c.mutation("rotated", "IngestKey", 0, true)}
			m.text = "Rotated the ingest key of " + c.projectShown()
			if until := optTime(p.PreviousIngestKeyUntil); until != "" {
				m.PreviousIngestKeyUntil = until
				m.text += fmt.Sprintf("; the old key works until %s (%s)", minute(until), age(until))
			} else {
				m.text += "; the old key stopped working at once"
			}
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<project> --grace 1h --force -o json"},
	}
	if !rotate {
		cmd.Annotations[agent.AnnotationLLMHint] = "<project> --force -o json"
		cmd.Use = "delete-previous <project>"
		cmd.Short = "Stop an Error Tracking project's previous ingest key at once."
		cmd.Long = `Stop the ingest key the last rotation replaced, before its grace period
ends, for example when it leaked; the current key and DSN do not change and
are not printed. A project with no previous key is left as it is, and the
result says changed: false.

gcx asks first; --force or GCX_AUTO_APPROVE skips the question, agent mode
needs one of them, and declining exits 5. ` + noDryRun
		cmd.Example = `  # Stop the previous key, after a prompt.
  gcx error-tracking projects ingest-keys delete-previous 314

  # Without a prompt, as scripts and agents must.
  gcx et projects ingest-keys delete-previous checkout-api --force

  # As an agent: stop it and check whether anything changed.
  gcx error-tracking projects ingest-keys delete-previous 314 --force -o json --jq .changed`
	}
	opts.setup(cmd.Flags(), rotate)
	return cmd
}
