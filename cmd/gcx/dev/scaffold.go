package dev

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/charmbracelet/huh"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/strcase"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

type scaffoldOpts struct {
	ProjectName  string
	GoModulePath string
}

func (opts *scaffoldOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.ProjectName, "project", "p", "", "Project name.")
	flags.StringVar(&opts.GoModulePath, "go-module-path", "", "Go module path.")
}

func scaffoldCmd() *cobra.Command {
	opts := &scaffoldOpts{}

	cmd := &cobra.Command{
		Use:   "scaffold",
		Args:  cobra.NoArgs,
		Short: "Scaffold a new Grafana resources-as-code project",
		Long:  "Scaffold a new Go project pre-configured for managing Grafana resources as code. Generates a module with example dashboards, a deploy workflow, and gcx configuration.",
		Example: `
	# Interactive scaffolding (prompts for project name and Go module path):
	gcx dev scaffold

	# Non-interactive with flags:
	gcx dev scaffold --project my-dashboards --go-module-path github.com/example/my-dashboards
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Interactivity guard (mirrors the login command): the huh form
			// is only for real terminals. Agents and pipes cannot answer a
			// TUI — fail fast with the exact flags to pass instead of
			// rendering an interactive form into a captured stream.
			if missing := missingScaffoldFlags(opts); len(missing) > 0 && !isInteractiveTerminal() {
				return &fail.UsageError{Message: fmt.Sprintf(
					"missing %s: interactive prompts require a terminal. Pass the flags explicitly, e.g. gcx dev scaffold --project my-dashboards --go-module-path github.com/example/my-dashboards",
					strings.Join(missing, " and "),
				)}
			}

			if err := askMissingOpts(opts); err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					cmdio.Info(cmd.OutOrStdout(), "Aborted.")
					// "Aborted." is the complete output of a cancelled run;
					// EmittedError carries the user-cancelled exit code (5)
					// without printing a second error rendering.
					return gcxerrors.NewEmittedError(gcxerrors.ExitCancelled, err)
				}

				return err
			}

			destinationRoot := strcase.ToKebabCase(opts.ProjectName)
			if err := scaffoldProject(destinationRoot, opts); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Project scaffolded in %s.", destinationRoot)

			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// missingScaffoldFlags lists the required flags not yet provided, in the
// order the interactive form would ask for them.
func missingScaffoldFlags(opts *scaffoldOpts) []string {
	var missing []string
	if opts.ProjectName == "" {
		missing = append(missing, "--project")
	}
	if opts.GoModulePath == "" {
		missing = append(missing, "--go-module-path")
	}
	return missing
}

// isInteractiveTerminal reports whether the process can run an interactive
// prompt: stdin is a real terminal and no agent is driving the CLI. Same
// guard as the login command — agent mode requires explicit flags even on a
// TTY, so an agent never hangs on (or garbles its transcript with) a TUI.
func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && !agent.IsAgentMode()
}

func requiredField(name string) func(s string) error {
	return func(s string) error {
		if s == "" {
			return fmt.Errorf("%s is required", name)
		}

		return nil
	}
}

func askMissingOpts(opts *scaffoldOpts) error {
	var missingFields []huh.Field

	if opts.ProjectName == "" {
		missingFields = append(missingFields, huh.NewInput().
			Title("Project name").
			Description("Name of the project.\nExample: observability-as-code").
			Validate(requiredField("project name")).
			Value(&opts.ProjectName),
		)
	}

	if opts.GoModulePath == "" {
		missingFields = append(missingFields, huh.NewInput().
			Title("Go module path").
			Description("Example: github.com/example/mymodule").
			Validate(requiredField("go module path")).
			Value(&opts.GoModulePath),
		)
	}

	if len(missingFields) == 0 {
		return nil
	}

	form := huh.NewForm(
		huh.NewGroup(missingFields...).Title("Scaffolding parameters"),
	)

	return form.Run()
}

func scaffoldProject(destinationRoot string, opts *scaffoldOpts) error {
	templatesRoot := "templates/scaffold"
	tmpl := template.New("").Option("missingkey=error")

	err := fs.WalkDir(templatesFS, templatesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		templateName := strings.TrimPrefix(strings.TrimPrefix(path, templatesRoot), "/")
		fileName := filepath.Join(destinationRoot, strings.TrimSuffix(templateName, ".tmpl"))

		fileHandle, err := templatesFS.Open(path)
		if err != nil {
			return err
		}
		defer fileHandle.Close()

		contents, err := io.ReadAll(fileHandle)
		if err != nil {
			return err
		}

		fileTmpl, err := tmpl.Parse(string(contents))
		if err != nil {
			return err
		}

		if err := ensureDirectory(filepath.Dir(fileName)); err != nil {
			return err
		}

		targetFile, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		defer targetFile.Close()

		return fileTmpl.Execute(targetFile, map[string]any{
			"Input": opts,
		})
	})
	if err != nil {
		return err
	}

	return nil
}

func ensureDirectory(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0744)
	}

	return nil
}
