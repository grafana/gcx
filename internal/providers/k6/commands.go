package k6

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"

	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/format"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// getAPIDomain extracts the --api-domain flag from the command hierarchy.
func getAPIDomain(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("api-domain"); f != nil && f.Changed {
		return f.Value.String()
	}
	// Walk up to find the persistent flag on the parent.
	for p := cmd.Parent(); p != nil; p = p.Parent() {
		if f := p.Flags().Lookup("api-domain"); f != nil && f.Changed {
			return f.Value.String()
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// projects commands
// ---------------------------------------------------------------------------

func newProjectsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Short:   "Manage K6 Cloud projects.",
		Aliases: []string{"project", "proj"},
	}
	cmd.AddCommand(
		newProjectsListCommand(loader),
		newProjectsGetCommand(loader),
		newProjectsCreateCommand(loader),
		newProjectsUpdateCommand(loader),
		newProjectsDeleteCommand(loader),
	)
	return cmd
}

type projectsListOpts struct {
	IO cmdio.Options
}

func (o *projectsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ProjectTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ProjectTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newProjectsListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &projectsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List K6 Cloud projects.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, ns, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			projects, err := client.ListProjects(ctx)
			if err != nil {
				return err
			}

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), projects)
			}
			var objs []unstructured.Unstructured
			for _, p := range projects {
				res, err := ToResource(p, ns)
				if err != nil {
					return fmt.Errorf("failed to convert project %d to resource: %w", p.ID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}
			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ProjectTableCodec renders projects as a tabular table.
type ProjectTableCodec struct {
	Wide bool
}

func (c *ProjectTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ProjectTableCodec) Encode(w io.Writer, v any) error {
	projects, ok := v.([]Project)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Project")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tNAME\tDEFAULT\tFOLDER UID\tCREATED\tUPDATED")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tDEFAULT\tCREATED")
	}

	for _, p := range projects {
		created := p.Created
		if len(created) > 16 {
			created = created[:16]
		}
		if created == "" {
			created = "-"
		}

		isDefault := "-"
		if p.IsDefault {
			isDefault = "yes"
		}

		if c.Wide {
			updated := p.Updated
			if len(updated) > 16 {
				updated = updated[:16]
			}
			if updated == "" {
				updated = "-"
			}
			folderUID := p.GrafanaFolderUID
			if folderUID == "" {
				folderUID = "-"
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, isDefault, folderUID, created, updated)
		} else {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", p.ID, p.Name, isDefault, created)
		}
	}
	return tw.Flush()
}

func (c *ProjectTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newProjectsGetCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &struct{ IO cmdio.Options }{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single K6 project by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid project ID: %w", err)
			}
			client, ns, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			p, err := client.GetProject(ctx, id)
			if err != nil {
				return err
			}
			res, err := ToResource(*p, ns)
			if err != nil {
				return fmt.Errorf("failed to convert project to resource: %w", err)
			}
			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.IO.DefaultFormat("yaml")
	opts.IO.BindFlags(cmd.Flags())
	return cmd
}

type projectsCreateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *projectsCreateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the project manifest (use - for stdin)")
}

func newProjectsCreateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &projectsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new K6 project from a file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			client, ns, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}

			var reader io.Reader
			if opts.File == "-" {
				reader = cmd.InOrStdin()
			} else {
				f, err := os.Open(opts.File)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", opts.File, err)
				}
				defer f.Close()
				reader = f
			}

			yamlCodec := format.NewYAMLCodec()
			var obj unstructured.Unstructured
			if err := yamlCodec.Decode(reader, &obj); err != nil {
				return fmt.Errorf("failed to parse input: %w", err)
			}

			res, err := resources.FromUnstructured(&obj)
			if err != nil {
				return fmt.Errorf("failed to build resource from input: %w", err)
			}

			p, err := FromResource(res)
			if err != nil {
				return fmt.Errorf("failed to convert resource to project: %w", err)
			}

			created, err := client.CreateProject(ctx, p.Name)
			if err != nil {
				return fmt.Errorf("failed to create project: %w", err)
			}

			createdRes, err := ToResource(*created, ns)
			if err != nil {
				return fmt.Errorf("failed to convert created project to resource: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Created project %q (id=%d)", created.Name, created.ID)
			createdObj := createdRes.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &createdObj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newProjectsUpdateCommand(loader CloudConfigLoader) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a K6 project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid project ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}

			var reader io.Reader
			if file == "-" {
				reader = cmd.InOrStdin()
			} else {
				f, err := os.Open(file)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", file, err)
				}
				defer f.Close()
				reader = f
			}

			yamlCodec := format.NewYAMLCodec()
			var obj unstructured.Unstructured
			if err := yamlCodec.Decode(reader, &obj); err != nil {
				return fmt.Errorf("failed to parse input: %w", err)
			}

			res, err := resources.FromUnstructured(&obj)
			if err != nil {
				return fmt.Errorf("failed to build resource from input: %w", err)
			}

			p, err := FromResource(res)
			if err != nil {
				return fmt.Errorf("failed to convert resource to project: %w", err)
			}

			if err := client.UpdateProject(ctx, id, p.Name); err != nil {
				return fmt.Errorf("failed to update project: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated project %d", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "filename", "f", "", "File containing the project manifest (use - for stdin)")
	return cmd
}

func newProjectsDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a K6 project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid project ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			if err := client.DeleteProject(ctx, id); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deleted project %d", id)
			return nil
		},
	}
	return cmd
}

// ---------------------------------------------------------------------------
// tests commands
// ---------------------------------------------------------------------------

func newTestsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tests",
		Short:   "Manage K6 Cloud load tests.",
		Aliases: []string{"test"},
	}
	cmd.AddCommand(
		newTestsListCommand(loader),
		newTestsGetCommand(loader),
		newTestsDeleteCommand(loader),
	)
	return cmd
}

type testsListOpts struct {
	IO        cmdio.Options
	ProjectID int
}

func (o *testsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &LoadTestTableCodec{})
	o.IO.RegisterCustomCodec("wide", &LoadTestTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.ProjectID, "project-id", 0, "Filter by project ID")
}

func newTestsListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List K6 Cloud load tests.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			tests, err := client.ListLoadTests(ctx)
			if err != nil {
				return err
			}
			if opts.ProjectID != 0 {
				var filtered []LoadTest
				for _, t := range tests {
					if t.ProjectID == opts.ProjectID {
						filtered = append(filtered, t)
					}
				}
				tests = filtered
			}
			return opts.IO.Encode(cmd.OutOrStdout(), tests)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// LoadTestTableCodec renders load tests as a tabular table.
type LoadTestTableCodec struct {
	Wide bool
}

func (c *LoadTestTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *LoadTestTableCodec) Encode(w io.Writer, v any) error {
	tests, ok := v.([]LoadTest)
	if !ok {
		return errors.New("invalid data type for table codec: expected []LoadTest")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tNAME\tPROJECT\tCREATED\tUPDATED")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tPROJECT\tCREATED")
	}

	for _, t := range tests {
		created := t.Created
		if len(created) > 16 {
			created = created[:16]
		}
		if created == "" {
			created = "-"
		}

		if c.Wide {
			updated := t.Updated
			if len(updated) > 16 {
				updated = updated[:16]
			}
			if updated == "" {
				updated = "-"
			}
			fmt.Fprintf(tw, "%d\t%s\t%d\t%s\t%s\n", t.ID, t.Name, t.ProjectID, created, updated)
		} else {
			fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n", t.ID, t.Name, t.ProjectID, created)
		}
	}
	return tw.Flush()
}

func (c *LoadTestTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newTestsGetCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &struct{ IO cmdio.Options }{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single K6 load test by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid load test ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			test, err := client.GetLoadTest(ctx, id)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), test)
		},
	}
	opts.IO.DefaultFormat("json")
	opts.IO.BindFlags(cmd.Flags())
	return cmd
}

func newTestsDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a K6 load test.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid load test ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			if err := client.DeleteLoadTest(ctx, id); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deleted load test %d", id)
			return nil
		},
	}
	return cmd
}

// ---------------------------------------------------------------------------
// runs commands
// ---------------------------------------------------------------------------

func newRunsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "runs",
		Short:   "Manage K6 test runs.",
		Aliases: []string{"run"},
	}
	cmd.AddCommand(newRunsListCommand(loader))
	return cmd
}

type runsListOpts struct {
	IO cmdio.Options
}

func (o *runsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TestRunTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newRunsListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &runsListOpts{}
	cmd := &cobra.Command{
		Use:   "list <load-test-id>",
		Short: "List test runs for a load test.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			loadTestID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid load test ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			runs, err := client.ListTestRuns(ctx, loadTestID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), runs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// TestRunTableCodec renders test runs as a tabular table.
type TestRunTableCodec struct{}

func (c *TestRunTableCodec) Format() format.Format { return "table" }

func (c *TestRunTableCodec) Encode(w io.Writer, v any) error {
	runs, ok := v.([]TestRunStatus)
	if !ok {
		return errors.New("invalid data type for table codec: expected []TestRunStatus")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTEST ID\tSTATUS\tRESULT\tCREATED\tENDED")

	for _, r := range runs {
		created := r.Created
		if len(created) > 16 {
			created = created[:16]
		}
		if created == "" {
			created = "-"
		}
		ended := r.Ended
		if len(ended) > 16 {
			ended = ended[:16]
		}
		if ended == "" {
			ended = "-"
		}
		result := resultStatusString(r.ResultStatus)
		fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\n", r.ID, r.LoadTestID, r.Status, result, created, ended)
	}
	return tw.Flush()
}

func (c *TestRunTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func resultStatusString(status int) string {
	switch status {
	case 0:
		return "pending"
	case 1:
		return "passed"
	case 2:
		return "failed"
	default:
		return strconv.Itoa(status)
	}
}

// ---------------------------------------------------------------------------
// envvars commands
// ---------------------------------------------------------------------------

func newEnvVarsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "envvars",
		Short:   "Manage K6 Cloud environment variables.",
		Aliases: []string{"envvar", "env"},
	}
	cmd.AddCommand(
		newEnvVarsListCommand(loader),
		newEnvVarsCreateCommand(loader),
		newEnvVarsUpdateCommand(loader),
		newEnvVarsDeleteCommand(loader),
	)
	return cmd
}

type envVarsListOpts struct {
	IO cmdio.Options
}

func (o *envVarsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &EnvVarTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newEnvVarsListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &envVarsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List K6 environment variables.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			envVars, err := client.ListEnvVars(ctx)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), envVars)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// EnvVarTableCodec renders environment variables as a tabular table.
type EnvVarTableCodec struct{}

func (c *EnvVarTableCodec) Format() format.Format { return "table" }

func (c *EnvVarTableCodec) Encode(w io.Writer, v any) error {
	envVars, ok := v.([]EnvVar)
	if !ok {
		return errors.New("invalid data type for table codec: expected []EnvVar")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tVALUE\tDESCRIPTION")

	for _, e := range envVars {
		desc := e.Description
		if desc == "" {
			desc = "-"
		}
		value := e.Value
		if len(value) > 40 {
			value = value[:37] + "..."
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", e.ID, e.Name, value, desc)
	}
	return tw.Flush()
}

func (c *EnvVarTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newEnvVarsCreateCommand(loader CloudConfigLoader) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a K6 environment variable from a file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}

			var reader io.Reader
			if file == "-" {
				reader = cmd.InOrStdin()
			} else {
				f, err := os.Open(file)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", file, err)
				}
				defer f.Close()
				reader = f
			}

			var ev EnvVar
			codec := format.NewJSONCodec()
			if err := codec.Decode(reader, &ev); err != nil {
				return fmt.Errorf("failed to parse input: %w", err)
			}

			created, err := client.CreateEnvVar(ctx, ev.Name, ev.Value, ev.Description)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Created env var %q (id=%d)", created.Name, created.ID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "filename", "f", "", "File containing the env var JSON (use - for stdin)")
	return cmd
}

func newEnvVarsUpdateCommand(loader CloudConfigLoader) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a K6 environment variable.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid env var ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}

			var reader io.Reader
			if file == "-" {
				reader = cmd.InOrStdin()
			} else {
				f, err := os.Open(file)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", file, err)
				}
				defer f.Close()
				reader = f
			}

			var ev EnvVar
			codec := format.NewJSONCodec()
			if err := codec.Decode(reader, &ev); err != nil {
				return fmt.Errorf("failed to parse input: %w", err)
			}

			if err := client.UpdateEnvVar(ctx, id, ev.Name, ev.Value, ev.Description); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated env var %d", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "filename", "f", "", "File containing the env var JSON (use - for stdin)")
	return cmd
}

func newEnvVarsDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a K6 environment variable.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid env var ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			if err := client.DeleteEnvVar(ctx, id); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deleted env var %d", id)
			return nil
		},
	}
	return cmd
}

// ---------------------------------------------------------------------------
// token command
// ---------------------------------------------------------------------------

func newTokenCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the authenticated k6 API token.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader, getAPIDomain(cmd))
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), client.Token())
			return nil
		},
	}
	return cmd
}
