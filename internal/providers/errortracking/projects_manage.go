package errortracking

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type projectsWriteRequest struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	SourceRepoURL *string `json:"sourceRepoUrl,omitempty"`
}

const noDryRun = "No --dry-run."

func checkName(cmd *cobra.Command, name string) error {
	if strings.TrimSpace(name) == "" {
		return usage(cmd, "--name must not be empty")
	}
	if n := utf8.RuneCountInString(name); n > 255 {
		return usage(cmd, fmt.Sprintf("--name is %d characters; use at most 255", n))
	}
	return nil
}

func checkRepoURL(cmd *cobra.Command, raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return usage(cmd, fmt.Sprintf("--repo-url %q: use an http(s) URL", raw))
	}
	return nil
}

func (c *client) nameTaken(ctx context.Context, err error, kind, name string) error {
	status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
	if !ok || status.Status != http.StatusConflict {
		return err
	}
	var id int64
	switch kind {
	case "project":
		if projects, lerr := c.listProjects(ctx); lerr == nil {
			for _, p := range projects {
				if strings.EqualFold(p.Name, name) {
					id = p.ID
				}
			}
		}
	case "heatmap":
		if heatmaps, lerr := c.listHeatmaps(ctx); lerr == nil {
			for _, h := range heatmaps {
				if strings.EqualFold(h.Name, name) {
					id = h.ID
				}
			}
		}
	}
	if id == 0 {
		return err
	}
	return &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryResourceConflict,
		Details:     fmt.Sprintf("a %s named %q already exists (ID %d)", kind, printable(name), id),
		Suggestions: []string{fmt.Sprintf("See it: gcx error-tracking %ss get %d", kind, id)},
	}
}

type projectsCreateOpts struct {
	IO                         cmdio.Options
	Name, Description, RepoURL string
}

func (o *projectsCreateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Name, "name", "", "Project name, at most 255 characters (required)")
	_ = cobra.MarkFlagRequired(flags, "name")
	flags.StringVar(&o.Description, "description", "", "Project description")
	flags.StringVar(&o.RepoURL, "repo-url", "", "http(s) URL of the source repository")
	bindText(&o.IO, flags, mutationText{})
}

func (o *projectsCreateOpts) Validate(cmd *cobra.Command) error {
	o.Name = strings.TrimSpace(o.Name)
	if err := checkName(cmd, o.Name); err != nil {
		return err
	}
	if err := checkRepoURL(cmd, o.RepoURL); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func newProjectsCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &projectsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an Error Tracking project.",
		Long: `Create an Error Tracking project. The result names the new project's ID,
which --project and projects get take.

Names are unique in the stack. ` + noDryRun,
		Example: `  # Create a project.
  gcx error-tracking projects create --name checkout-api

  # With a description and the repository its stack frames link to.
  gcx et projects create --name checkout-api --description "Checkout service" --repo-url https://git.example.com/acme/checkout

  # As an agent: the new ID only.
  gcx error-tracking projects create --name checkout-api -o json --jq .target.id`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			in := projectsWriteRequest{Name: &opts.Name}
			if opts.Description != "" {
				in.Description = &opts.Description
			}
			if opts.RepoURL != "" {
				in.SourceRepoURL = &opts.RepoURL
			}
			var p project
			if err := c.do(ctx, http.MethodPost, "projects", nil, in, &p); err != nil {
				return c.nameTaken(ctx, err, "project", opts.Name)
			}
			m := c.mutation("created", "Project", p.ID, true)
			m.Target.Name, m.Project = p.Name, nil
			m.text = "Created " + p.shown()
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "--name <name> -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type projectsUpdateOpts struct {
	IO                         cmdio.Options
	Project                    projectRef
	Name, Description, RepoURL string
	req                        projectsWriteRequest
}

func (o *projectsUpdateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Name, "name", "", "New project name, at most 255 characters")
	flags.StringVar(&o.Description, "description", "", "New description; empty clears it")
	flags.StringVar(&o.RepoURL, "repo-url", "", "New http(s) source repository URL; empty clears it")
	bindText(&o.IO, flags, mutationText{})
}

func (o *projectsUpdateOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	flags := cmd.Flags()
	if flags.Changed("name") {
		o.Name = strings.TrimSpace(o.Name)
		if err := checkName(cmd, o.Name); err != nil {
			return err
		}
	}
	if err := checkRepoURL(cmd, o.RepoURL); err != nil {
		return err
	}
	set := func(flag string, v string, to **string) {
		if flags.Changed(flag) {
			*to = &v
		}
	}
	set("name", o.Name, &o.req.Name)
	set("description", o.Description, &o.req.Description)
	set("repo-url", o.RepoURL, &o.req.SourceRepoURL)
	if o.req == (projectsWriteRequest{}) {
		return usage(cmd, "pass at least one of --name, --description or --repo-url")
	}
	return validIO(cmd, &o.IO)
}

func newProjectsUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &projectsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <project>",
		Short: "Change an Error Tracking project's name, description or repository.",
		Long: `Change an Error Tracking project's name, description or repository URL.

Only the flags you pass change; pass an empty value to clear the description
or the repository. The result names what changed, and a rename shows the old
and new names. ` + noDryRun,
		Example: `  # Rename a project.
  gcx error-tracking projects update 314 --name checkout

  # Set the repository and clear the description.
  gcx et projects update checkout-api --repo-url https://git.example.com/acme/checkout --description ""

  # As an agent: change the description, read back what changed.
  gcx error-tracking projects update 314 --description "Checkout service" -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Project.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			oldName := opts.Project.name
			if opts.req.Name != nil && oldName == "" {
				old, err := c.getProject(ctx, id)
				if err != nil {
					return err
				}
				oldName = old.Name
			}
			var p project
			if err := c.do(ctx, http.MethodPatch, routePath("projects", id), nil, opts.req, &p); err != nil {
				if opts.req.Name != nil {
					return c.nameTaken(ctx, err, "project", *opts.req.Name)
				}
				return err
			}
			c.knowProject(p.ID, p.Name)
			m := c.mutation("updated", "Project", p.ID, true)
			m.Target.Name, m.Project = p.Name, nil
			var parts []string
			if opts.req.Description != nil {
				parts = append(parts, "description")
			}
			if opts.req.SourceRepoURL != nil {
				parts = append(parts, "repository")
			}
			switch {
			case opts.req.Name != nil && oldName != p.Name:
				m.text = fmt.Sprintf("Renamed project %d: %s → %s", p.ID, oldName, p.Name)
				if len(parts) > 0 {
					m.text += "; changed its " + strings.Join(parts, " and ")
				}
			case len(parts) > 0:
				m.text = fmt.Sprintf("Changed the %s of %s", strings.Join(parts, " and "), p.shown())
			default:
				m.Changed = new(false)
				m.text = capital(p.shown()) + " already has that name; nothing changed"
			}
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<project> --description <text> -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type projectsDeleteOpts struct {
	IO      cmdio.Options
	Project projectRef
	Force   bool
}

func (o *projectsDeleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Delete without asking")
	bindText(&o.IO, flags, mutationText{})
}

func (o *projectsDeleteOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func clearDeletedDefault(cmd *cobra.Command, loader *providers.ConfigLoader, id int64, m *projectDeleteMutation) {
	if _, fromEnv := os.LookupEnv(projectEnv); fromEnv {
		cmdio.EmitWarn(cmd.ErrOrStderr(), projectEnv+" is set, so the saved default project was not checked; if it is this project, clear it: gcx error-tracking projects use --unset")
		return
	}
	v, err := storedDefault(cmd.Context(), loader)
	if err != nil {
		cmdio.EmitWarn(cmd.ErrOrStderr(), "could not read the saved default project, so it was not checked; if it is this project, clear it: gcx error-tracking projects use --unset ("+err.Error()+")")
		return
	}
	if v != strconv.FormatInt(id, 10) {
		return
	}
	if err := loader.SaveProviderConfig(cmd.Context(), providerName, "project", ""); err != nil {
		cmdio.EmitWarn(cmd.ErrOrStderr(), "could not clear the saved default project: "+err.Error())
		return
	}
	m.text += "; cleared it as the default project"
	m.ClearedDefault = true
}

func newProjectsDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &projectsDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <project>",
		Short: "Delete an Error Tracking project and everything in it.",
		Long: `Delete an Error Tracking project with all its errors, events, releases and
timelines; it cannot be undone. Its ingest key stops working at once, so
SDKs still sending to it get refused.

gcx asks first; --force skips the question, agent mode needs --force, and
declining exits 5. When the project is the saved default, delete clears the
default and the JSON result says clearedDefault: true; later commands then
need --project. When ` + projectEnv + ` is set or the saved
default cannot be read, delete leaves the default alone, says so on stderr and
the result has no clearedDefault. ` + noDryRun,
		Example: `  # Delete a project, after a prompt.
  gcx error-tracking projects delete 314

  # Delete by name.
  gcx et projects delete checkout-api --force

  # As an agent: delete without a prompt, as scripts and agents must.
  gcx error-tracking projects delete 314 --force -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Project.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			force, err := deleteBypass(cmd, opts.Force)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			path := routePath("projects", id)
			if !force || opts.Project.name == "" {
				p, err := c.getProject(ctx, id)
				if err != nil {
					return err
				}
				c.knowProject(p.ID, p.Name)
			}
			if !force {
				if err := confirm(cmd, fmt.Sprintf("Delete Error Tracking %s with all its errors and events?", c.projectShown()),
					"delete cancelled", fmt.Sprintf("The prompt was declined; %s was not deleted.", c.projectShown())); err != nil {
					return err
				}
			}
			if err := c.do(ctx, http.MethodDelete, path, nil, nil, nil); err != nil {
				return err
			}
			m := projectDeleteMutation{mutationBase: c.mutation("deleted", "Project", id, true)}
			m.Target.Name, m.Project = opts.Project.name, nil
			m.text = "Deleted " + c.projectShown()
			clearDeletedDefault(cmd, loader, id, &m)
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<project> --force -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
