package errortracking

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

const (
	needsLogin = " (needs a user login)"
)

func humanSuggestions(read string, editor bool) []string {
	signIn := "Sign in as yourself: gcx login"
	if editor {
		signIn = "Sign in as yourself, with the Editor or Admin role: gcx login"
	}
	return []string{
		signIn,
		"If GRAFANA_TOKEN is set in your environment it overrides the login: unset it for this command",
		"As a service account you can still read " + read,
	}
}

func humanOnly(err error, editor bool) error {
	status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
	if !ok || status.Status != http.StatusForbidden {
		return err
	}
	role := "Viewer, Editor or Admin"
	if editor {
		role = "Editor or Admin"
	}
	suggestions := []string{
		"Ask an organization admin for the " + role + " role in this organization",
		"Use your own login, not a service account token: gcx login",
		"If GRAFANA_TOKEN is set in your environment it overrides the login: unset it for this command",
		"Check what your login holds: gcx setup status",
	}
	return &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryAuthorizationFailed,
		Details:     status.ServerMessage,
		Suggestions: suggestions,
		ExitCode:    new(gcxerrors.ExitAuthFailure),
	}
}

func humanGuard(cmd *cobra.Command, what string, editor bool, errID string) func(*client) error {
	return func(c *client) error {
		if !c.robot {
			return nil
		}
		who := "a signed-in Grafana user"
		if editor {
			who += " (Editor or Admin)"
		}
		project := projectOf(cmd)
		read := "the errors: " + et("errors", "list", "--project", project)
		if errID != "" {
			read = "the error: " + et("errors", "get", printable(errID), "--project", project)
		}
		return &gcxerrors.DetailedError{
			Summary:     gcxerrors.SummaryAuthorizationFailed,
			Details:     fmt.Sprintf("%s needs %s; this context uses a service account token", what, who),
			Suggestions: humanSuggestions(read, editor),
			ExitCode:    new(gcxerrors.ExitAuthFailure),
		}
	}
}

type errorTarget struct {
	IO      cmdio.Options
	Project projectRef
	id      int64
}

func (o *errorTarget) Validate(cmd *cobra.Command, arg string) error {
	var err error
	if o.id, err = idArg(cmd, arg); err != nil {
		return err
	}
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func (o *errorTarget) connect(ctx context.Context, loader *providers.ConfigLoader, guard func(*client) error) (*client, string, error) {
	c, project, err := connect(ctx, loader, &o.Project, guard)
	if err != nil {
		return nil, "", err
	}
	return c, routePath("projects", project, "errors", o.id), nil
}
