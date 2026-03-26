package k6

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/grafanactl/internal/format"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Name resolution helpers
// ---------------------------------------------------------------------------

// resolveProject resolves a <id-or-name> argument to a project ID.
func resolveProject(cmd *cobra.Command, client *Client, arg string) (int, error) {
	id, err := strconv.Atoi(arg)
	if err == nil {
		return id, nil
	}
	// Not numeric — look up by name.
	p, err := client.GetProjectByName(cmd.Context(), arg)
	if err != nil {
		return 0, err
	}
	return p.ID, nil
}

// resolveLoadTest resolves a load test by ID flag or by name+project-id.
func resolveLoadTest(cmd *cobra.Command, client *Client, idFlag, projectID int, nameArg string) (*LoadTest, error) {
	ctx := cmd.Context()
	if idFlag != 0 {
		return client.GetLoadTest(ctx, idFlag)
	}
	if nameArg == "" {
		return nil, errors.New("either a test name argument or --id is required")
	}
	if projectID != 0 {
		return client.GetLoadTestByName(ctx, projectID, nameArg)
	}
	// No project-id: scan all tests.
	all, err := client.ListAllLoadTests(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == nameArg {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("load test %q not found (use --project-id to narrow search)", nameArg)
}

// resolveLoadTestArg resolves a <id-or-name> argument to a LoadTest.
func resolveLoadTestArg(cmd *cobra.Command, client *Client, arg string, projectID int) (*LoadTest, error) {
	id, err := strconv.Atoi(arg)
	if err == nil {
		return client.GetLoadTest(cmd.Context(), id)
	}
	return resolveLoadTest(cmd, client, 0, projectID, arg)
}

// requireNameOrID returns the name arg or validates that --id was provided.
func requireNameOrID(idFlag int, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if idFlag != 0 {
		return "", nil
	}
	return "", errors.New("either a name argument or --id flag is required")
}

// resolveTestCreateInput gathers name, projectID and script from flags or file input.
func resolveTestCreateInput(cmd *cobra.Command, opts *testsCreateOpts) (string, int, string, error) {
	if opts.File != "" {
		return resolveTestCreateFromFile(cmd, opts)
	}
	return resolveTestCreateFromFlags(opts)
}

func resolveTestCreateFromFile(cmd *cobra.Command, opts *testsCreateOpts) (string, int, string, error) {
	data, err := readFileOrStdin(cmd, opts.File)
	if err != nil {
		return "", 0, "", fmt.Errorf("failed to read file: %w", err)
	}
	var lt LoadTest
	if err := decodeYAMLOrJSON(data, &lt); err != nil {
		return "", 0, "", fmt.Errorf("failed to parse input: %w", err)
	}
	name := opts.Name
	if lt.Name != "" {
		name = lt.Name
	}
	projectID := opts.ProjectID
	if lt.ProjectID != 0 {
		projectID = lt.ProjectID
	}
	if projectID == 0 {
		return "", 0, "", errors.New("--project-id is required")
	}
	return name, projectID, lt.Script, nil
}

func resolveTestCreateFromFlags(opts *testsCreateOpts) (string, int, string, error) {
	if opts.Name == "" {
		return "", 0, "", errors.New("--name is required when --filename is not provided")
	}
	if opts.Script == "" {
		return "", 0, "", errors.New("--script is required when --filename is not provided")
	}
	if opts.ProjectID == 0 {
		return "", 0, "", errors.New("--project-id is required")
	}
	scriptBytes, err := os.ReadFile(opts.Script)
	if err != nil {
		return "", 0, "", fmt.Errorf("failed to read script file: %w", err)
	}
	return opts.Name, opts.ProjectID, string(scriptBytes), nil
}

// readFileOrStdin reads a file path, or stdin when path is "-".
func readFileOrStdin(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path)
}

// decodeYAMLOrJSON decodes YAML or JSON data into the target.
func decodeYAMLOrJSON(data []byte, target any) error {
	codec := format.NewYAMLCodec()
	return codec.Decode(strings.NewReader(string(data)), target)
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
			crud, ns, err := NewTypedCRUDProject(ctx, loader)
			if err != nil {
				return err
			}
			typedObjs, err := crud.List(ctx)
			if err != nil {
				return err
			}

			// Extract projects from TypedObject
			projects := make([]Project, len(typedObjs))
			for i := range typedObjs {
				projects[i] = typedObjs[i].Spec
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

type projectsGetOpts struct {
	IO cmdio.Options
}

func (o *projectsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newProjectsGetCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &projectsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <id-or-name>",
		Short: "Get a single K6 project by ID or name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, ns, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			id, parseErr := strconv.Atoi(args[0])
			var p *Project
			if parseErr == nil {
				p, err = client.GetProject(ctx, id)
			} else {
				p, err = client.GetProjectByName(ctx, args[0])
			}
			if err != nil {
				return err
			}
			res, convErr := ToResource(*p, ns)
			if convErr != nil {
				return fmt.Errorf("failed to convert project to resource: %w", convErr)
			}
			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
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
			client, ns, err := authenticatedClient(ctx, loader)
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

type projectsUpdateOpts struct {
	File string
}

func (o *projectsUpdateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the project manifest (use - for stdin)")
}

func newProjectsUpdateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &projectsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <id-or-name>",
		Short: "Update a K6 project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			id, err := resolveProject(cmd, client, args[0])
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

			if err := client.UpdateProject(ctx, id, p.Name); err != nil {
				return fmt.Errorf("failed to update project: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated project %d", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newProjectsDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id-or-name>",
		Short: "Delete a K6 project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			id, err := resolveProject(cmd, client, args[0])
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
		Use:     "load-tests",
		Short:   "Manage K6 Cloud load tests.",
		Aliases: []string{"tests", "test"},
	}
	cmd.AddCommand(
		newTestsListCommand(loader),
		newTestsGetCommand(loader),
		newTestsCreateCommand(loader),
		newTestsUpdateCommand(loader),
		newTestsUpdateScriptCommand(loader),
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
			client, _, err := authenticatedClient(ctx, loader)
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

type testsGetOpts struct {
	IO        cmdio.Options
	ProjectID int
}

func (o *testsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.ProjectID, "project-id", 0, "Project ID (required when looking up by name)")
}

func newTestsGetCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <id-or-name>",
		Short: "Get a single K6 load test by ID or name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			// Resolve by ID or name.
			test, err := resolveLoadTestArg(cmd, client, args[0], opts.ProjectID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), test)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type testsCreateOpts struct {
	IO        cmdio.Options
	File      string
	Name      string
	Script    string
	ProjectID int
}

func (o *testsCreateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the test definition (JSON/YAML)")
	flags.StringVar(&o.Name, "name", "", "Test name (required when --filename not used)")
	flags.StringVar(&o.Script, "script", "", "Path to k6 script file (required when --filename not used)")
	flags.IntVar(&o.ProjectID, "project-id", 0, "Project ID (required when --filename not used)")
}

func newTestsCreateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new K6 load test.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			name, projectID, scriptContent, err := resolveTestCreateInput(cmd, opts)
			if err != nil {
				return err
			}

			test, err := client.CreateLoadTest(ctx, name, projectID, scriptContent)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Created load test %q (id=%d)", test.Name, test.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), test)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type testsUpdateOpts struct {
	File string
}

func (o *testsUpdateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the test definition (JSON/YAML)")
}

func newTestsUpdateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a K6 load test from a file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid load test ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			data, err := readFileOrStdin(cmd, opts.File)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var lt LoadTest
			if err := decodeYAMLOrJSON(data, &lt); err != nil {
				return fmt.Errorf("failed to parse input: %w", err)
			}

			if err := client.UpdateLoadTest(ctx, id, lt.Name, lt.Script); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated load test %d", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type testsUpdateScriptOpts struct {
	File string
}

func (o *testsUpdateScriptOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "k6 script file to upload")
}

func newTestsUpdateScriptCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testsUpdateScriptOpts{}
	cmd := &cobra.Command{
		Use:   "update-script <id>",
		Short: "Update the script of a K6 load test from a file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid load test ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			data, err := readFileOrStdin(cmd, opts.File)
			if err != nil {
				return fmt.Errorf("failed to read script file: %w", err)
			}

			if err := client.UpdateLoadTestScript(ctx, id, string(data)); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated script for load test %d", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
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
			client, _, err := authenticatedClient(ctx, loader)
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
// runs commands (backward-compat alias)
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
	IO        cmdio.Options
	ProjectID int
	TestID    int
}

func (o *runsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TestRunTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.ProjectID, "project-id", 0, "Project ID (required when looking up by name)")
	flags.IntVar(&o.TestID, "id", 0, "Load test ID (skip name lookup)")
}

func newRunsListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &runsListOpts{}
	cmd := &cobra.Command{
		Use:   "list [id-or-name]",
		Short: "List test runs for a load test.",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			var loadTestID int
			switch {
			case opts.TestID != 0:
				loadTestID = opts.TestID
			case len(args) == 1:
				id, parseErr := strconv.Atoi(args[0])
				if parseErr == nil {
					loadTestID = id
				} else {
					test, resolveErr := resolveLoadTest(cmd, client, 0, opts.ProjectID, args[0])
					if resolveErr != nil {
						return resolveErr
					}
					loadTestID = test.ID
				}
			default:
				return errors.New("either a load test ID/name argument or --id is required")
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
		Use:     "env-vars",
		Short:   "Manage K6 Cloud environment variables.",
		Aliases: []string{"envvars", "envvar", "env"},
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
			client, _, err := authenticatedClient(ctx, loader)
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

type envVarsCreateOpts struct {
	File string
}

func (o *envVarsCreateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the env var JSON (use - for stdin)")
}

func newEnvVarsCreateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &envVarsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a K6 environment variable from a file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
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
	opts.setup(cmd.Flags())
	return cmd
}

type envVarsUpdateOpts struct {
	File string
}

func (o *envVarsUpdateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the env var JSON (use - for stdin)")
}

func newEnvVarsUpdateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &envVarsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a K6 environment variable.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid env var ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
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
	opts.setup(cmd.Flags())
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
			client, _, err := authenticatedClient(ctx, loader)
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
// auth commands (token moved under auth parent)
// ---------------------------------------------------------------------------

func newAuthCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "K6 authentication commands.",
	}
	cmd.AddCommand(newTokenCommand(loader))
	return cmd
}

func newTokenCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the authenticated k6 API token.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), client.Token())
			return nil
		},
	}
	return cmd
}

// ---------------------------------------------------------------------------
// schedules commands
// ---------------------------------------------------------------------------

func newSchedulesCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "schedules",
		Short:   "Manage K6 Cloud schedules.",
		Aliases: []string{"schedule"},
	}
	cmd.AddCommand(
		newSchedulesListCommand(loader),
		newSchedulesGetCommand(loader),
		newSchedulesCreateCommand(loader),
		newSchedulesUpdateCommand(loader),
		newSchedulesDeleteCommand(loader),
	)
	return cmd
}

// ScheduleTableCodec renders schedules as a tabular table.
type ScheduleTableCodec struct{}

func (c *ScheduleTableCodec) Format() format.Format { return "table" }

func (c *ScheduleTableCodec) Encode(w io.Writer, v any) error {
	schedules, ok := v.([]Schedule)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Schedule")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tLOAD TEST\tSTARTS\tNEXT RUN\tDEACTIVATED")

	for _, s := range schedules {
		starts := s.Starts
		if len(starts) > 16 {
			starts = starts[:16]
		}
		if starts == "" {
			starts = "-"
		}
		nextRun := s.NextRun
		if len(nextRun) > 16 {
			nextRun = nextRun[:16]
		}
		if nextRun == "" {
			nextRun = "-"
		}
		deactivated := "-"
		if s.Deactivated {
			deactivated = "yes"
		}
		fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\n", s.ID, s.LoadTestID, starts, nextRun, deactivated)
	}
	return tw.Flush()
}

func (c *ScheduleTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

type schedulesListOpts struct {
	IO cmdio.Options
}

func (o *schedulesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ScheduleTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newSchedulesListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &schedulesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all K6 schedules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			schedules, err := client.ListSchedules(ctx)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), schedules)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type schedulesGetOpts struct {
	IO cmdio.Options
}

func (o *schedulesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newSchedulesGetCommand(loader CloudConfigLoader) *cobra.Command { //nolint:dupl // Structurally similar to newAllowedProjectsListCommand but different API calls.
	opts := &schedulesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single K6 schedule by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid schedule ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			schedule, err := client.GetSchedule(ctx, id)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), schedule)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type schedulesCreateOpts struct {
	LoadTestID int
	File       string
}

func (o *schedulesCreateOpts) setup(flags *pflag.FlagSet) {
	flags.IntVar(&o.LoadTestID, "load-test-id", 0, "Load test ID (required)")
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the schedule request (JSON/YAML)")
}

func newSchedulesCreateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &schedulesCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a K6 schedule from a file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.LoadTestID == 0 {
				return errors.New("--load-test-id is required")
			}
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			data, err := readFileOrStdin(cmd, opts.File)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var req ScheduleRequest
			if err := decodeYAMLOrJSON(data, &req); err != nil {
				return fmt.Errorf("failed to parse input: %w", err)
			}

			schedule, err := client.CreateSchedule(ctx, opts.LoadTestID, req)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Created schedule %d for load test %d", schedule.ID, opts.LoadTestID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type schedulesUpdateOpts struct {
	File string
}

func (o *schedulesUpdateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the schedule request (JSON/YAML)")
}

func newSchedulesUpdateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &schedulesUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a K6 schedule from a file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid schedule ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			data, err := readFileOrStdin(cmd, opts.File)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var req ScheduleRequest
			if err := decodeYAMLOrJSON(data, &req); err != nil {
				return fmt.Errorf("failed to parse input: %w", err)
			}

			if _, err := client.UpdateScheduleByID(ctx, id, req); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated schedule %d", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newSchedulesDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <load-test-id>",
		Short: "Delete the schedule for a K6 load test.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid load test ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			if err := client.DeleteScheduleByLoadTest(ctx, id); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deleted schedule for load test %d", id)
			return nil
		},
	}
	return cmd
}

// ---------------------------------------------------------------------------
// load-zones commands
// ---------------------------------------------------------------------------

func newLoadZonesCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "load-zones",
		Short:   "Manage K6 private load zones.",
		Aliases: []string{"load-zone", "lz"},
	}
	cmd.AddCommand(
		newLoadZonesListCommand(loader),
		newLoadZonesCreateCommand(loader),
		newLoadZonesDeleteCommand(loader),
		newAllowedProjectsCommand(loader),
		newAllowedLoadZonesCommand(loader),
	)
	return cmd
}

// LoadZoneTableCodec renders load zones as a tabular table.
type LoadZoneTableCodec struct{}

func (c *LoadZoneTableCodec) Format() format.Format { return "table" }

func (c *LoadZoneTableCodec) Encode(w io.Writer, v any) error {
	zones, ok := v.([]LoadZone)
	if !ok {
		return errors.New("invalid data type for table codec: expected []LoadZone")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tK6 LOAD ZONE ID")

	for _, z := range zones {
		k6ID := z.K6LoadZoneID
		if k6ID == "" {
			k6ID = "-"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\n", z.ID, z.Name, k6ID)
	}
	return tw.Flush()
}

func (c *LoadZoneTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

type loadZonesListOpts struct {
	IO cmdio.Options
}

func (o *loadZonesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &LoadZoneTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newLoadZonesListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &loadZonesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all K6 load zones.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			zones, err := client.ListLoadZones(ctx)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), zones)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type loadZonesCreateOpts struct {
	Name       string
	ProviderID string
	CPU        string
	Memory     string
	Image      string
}

func (o *loadZonesCreateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Name, "name", "", "Load zone name (must be unique in your org)")
	flags.StringVar(&o.ProviderID, "provider-id", "", "Provider ID for the load zone")
	flags.StringVar(&o.CPU, "cpu", "2", "CPU limit for load zone pods")
	flags.StringVar(&o.Memory, "memory", "1Gi", "Memory limit for load zone pods")
	flags.StringVar(&o.Image, "image", "grafana/k6:latest", "k6 runner image")
}

func newLoadZonesCreateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &loadZonesCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Register a Private Load Zone.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.Name == "" {
				return errors.New("--name is required")
			}
			if opts.ProviderID == "" {
				return errors.New("--provider-id is required")
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			req := PLZCreateRequest{
				ProviderID:   opts.ProviderID,
				K6LoadZoneID: opts.Name,
				PodTiers:     PLZPodTiers{CPU: opts.CPU, Memory: opts.Memory},
				Config:       PLZConfig{LoadRunnerImage: opts.Image},
			}
			resp, err := client.CreateLoadZone(ctx, req)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Registered load zone %q", resp.Name)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newLoadZonesDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Deregister a Private Load Zone.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			if err := client.DeleteLoadZone(ctx, args[0]); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deregistered load zone %q", args[0])
			return nil
		},
	}
	return cmd
}

// ---------------------------------------------------------------------------
// allowed-projects sub-commands (under load-zones)
// ---------------------------------------------------------------------------

// AllowedProjectTableCodec renders allowed projects as a tabular table.
type AllowedProjectTableCodec struct{}

func (c *AllowedProjectTableCodec) Format() format.Format { return "table" }

func (c *AllowedProjectTableCodec) Encode(w io.Writer, v any) error {
	projects, ok := v.([]AllowedProject)
	if !ok {
		return errors.New("invalid data type for table codec: expected []AllowedProject")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME")
	for _, p := range projects {
		name := p.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(tw, "%d\t%s\n", p.ID, name)
	}
	return tw.Flush()
}

func (c *AllowedProjectTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newAllowedProjectsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allowed-projects",
		Short: "Manage projects allowed to use a load zone.",
	}
	cmd.AddCommand(
		newAllowedProjectsListCommand(loader),
		newAllowedProjectsUpdateCommand(loader),
	)
	return cmd
}

type allowedProjectsListOpts struct {
	IO cmdio.Options
}

func (o *allowedProjectsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &AllowedProjectTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newAllowedProjectsListCommand(loader CloudConfigLoader) *cobra.Command { //nolint:dupl // Structurally similar to newAllowedLoadZonesListCommand but different API calls.
	opts := &allowedProjectsListOpts{}
	cmd := &cobra.Command{
		Use:   "list <load-zone-id>",
		Short: "List projects allowed to use a load zone.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid load zone ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			projects, err := client.ListAllowedProjects(ctx, id)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), projects)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type allowedProjectsUpdateOpts struct {
	File string
}

func (o *allowedProjectsUpdateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing project IDs (JSON array)")
}

func newAllowedProjectsUpdateCommand(loader CloudConfigLoader) *cobra.Command { //nolint:dupl // Structurally similar to newAllowedLoadZonesUpdateCommand but different API calls.
	opts := &allowedProjectsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <load-zone-id>",
		Short: "Update projects allowed to use a load zone.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid load zone ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			data, err := readFileOrStdin(cmd, opts.File)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var projectIDs []int
			if err := json.Unmarshal(data, &projectIDs); err != nil {
				return fmt.Errorf("failed to parse project IDs (expected JSON array of ints): %w", err)
			}

			if err := client.UpdateAllowedProjects(ctx, id, projectIDs); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated allowed projects for load zone %d", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// allowed-load-zones sub-commands (under load-zones)
// ---------------------------------------------------------------------------

// AllowedLoadZoneTableCodec renders allowed load zones as a tabular table.
type AllowedLoadZoneTableCodec struct{}

func (c *AllowedLoadZoneTableCodec) Format() format.Format { return "table" }

func (c *AllowedLoadZoneTableCodec) Encode(w io.Writer, v any) error {
	zones, ok := v.([]AllowedLoadZone)
	if !ok {
		return errors.New("invalid data type for table codec: expected []AllowedLoadZone")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME")
	for _, z := range zones {
		name := z.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(tw, "%d\t%s\n", z.ID, name)
	}
	return tw.Flush()
}

func (c *AllowedLoadZoneTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newAllowedLoadZonesCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allowed-load-zones",
		Short: "Manage load zones allowed for a project.",
	}
	cmd.AddCommand(
		newAllowedLoadZonesListCommand(loader),
		newAllowedLoadZonesUpdateCommand(loader),
	)
	return cmd
}

type allowedLoadZonesListOpts struct {
	IO cmdio.Options
}

func (o *allowedLoadZonesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &AllowedLoadZoneTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newAllowedLoadZonesListCommand(loader CloudConfigLoader) *cobra.Command { //nolint:dupl // Structurally similar to newAllowedProjectsListCommand but different API calls.
	opts := &allowedLoadZonesListOpts{}
	cmd := &cobra.Command{
		Use:   "list <project-id>",
		Short: "List load zones allowed for a project.",
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
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}
			zones, err := client.ListAllowedLoadZones(ctx, id)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), zones)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type allowedLoadZonesUpdateOpts struct {
	File string
}

func (o *allowedLoadZonesUpdateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing load zone IDs (JSON array)")
}

func newAllowedLoadZonesUpdateCommand(loader CloudConfigLoader) *cobra.Command { //nolint:dupl // Structurally similar to newAllowedProjectsUpdateCommand but different API calls.
	opts := &allowedLoadZonesUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <project-id>",
		Short: "Update load zones allowed for a project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid project ID: %w", err)
			}
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			data, err := readFileOrStdin(cmd, opts.File)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			var loadZoneIDs []int
			if err := json.Unmarshal(data, &loadZoneIDs); err != nil {
				return fmt.Errorf("failed to parse load zone IDs (expected JSON array of ints): %w", err)
			}

			if err := client.UpdateAllowedLoadZones(ctx, id, loadZoneIDs); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated allowed load zones for project %d", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// testrun commands
// ---------------------------------------------------------------------------

func newTestrunCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "test-run",
		Short:   "Manage k6 TestRun CRD manifests.",
		Aliases: []string{"testrun"},
	}
	cmd.AddCommand(
		newTestrunEmitCommand(loader),
		newTestrunStatusCommand(loader),
		newTestrunRunsCommand(loader),
	)
	return cmd
}

type testrunEmitOpts struct {
	ProjectID   int
	Namespace   string
	TokenSecret string
	Parallelism int
	ID          int
	EmitSecret  bool
	Apply       bool
}

func (o *testrunEmitOpts) setup(flags *pflag.FlagSet) {
	flags.IntVar(&o.ProjectID, "project-id", 0, "k6 Cloud project ID")
	flags.StringVar(&o.Namespace, "namespace", "k6-tests", "Kubernetes namespace for emitted manifests")
	flags.StringVar(&o.TokenSecret, "token-secret", "grafana-k6-token", "Secret name for the Grafana Cloud token")
	flags.IntVar(&o.Parallelism, "parallelism", 1, "Number of parallel k6 runner pods")
	flags.IntVar(&o.ID, "id", 0, "Load test ID (skip name lookup)")
	flags.BoolVar(&o.EmitSecret, "emit-secret", false, "Include Secret manifest stub in output")
	flags.BoolVar(&o.Apply, "apply", false, "Apply ConfigMap and TestRun manifests via kubectl")
}

func newTestrunEmitCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testrunEmitOpts{}
	cmd := &cobra.Command{
		Use:   "emit [test-name]",
		Short: "Fetch a k6 Cloud test and emit Kubernetes TestRun CRD manifests.",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			testName, err := requireNameOrID(opts.ID, args)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			test, err := resolveLoadTest(cmd, client, opts.ID, opts.ProjectID, testName)
			if err != nil {
				return err
			}
			script, err := client.GetLoadTestScript(ctx, test.ID)
			if err != nil {
				return err
			}

			manifests := testrunK8sManifests(opts.Namespace, opts.TokenSecret, test.Name, script, opts.Parallelism, opts.ProjectID, opts.EmitSecret)
			fmt.Fprint(cmd.OutOrStdout(), manifests)

			if opts.Apply {
				applyManifests := testrunK8sManifests(opts.Namespace, opts.TokenSecret, test.Name, script, opts.Parallelism, opts.ProjectID, false)
				kubectl := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
				kubectl.Stdin = strings.NewReader(applyManifests)
				kubectl.Stdout = cmd.ErrOrStderr()
				kubectl.Stderr = cmd.ErrOrStderr()
				if err := kubectl.Run(); err != nil {
					return fmt.Errorf("kubectl apply failed: %w", err)
				}
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type testrunStatusOpts struct {
	ProjectID int
	ID        int
}

func (o *testrunStatusOpts) setup(flags *pflag.FlagSet) {
	flags.IntVar(&o.ProjectID, "project-id", 0, "k6 Cloud project ID (required when using name lookup)")
	flags.IntVar(&o.ID, "id", 0, "Load test ID (skip name lookup)")
}

func newTestrunStatusCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testrunStatusOpts{}
	cmd := &cobra.Command{
		Use:   "status [test-name]",
		Short: "Show the most recent test run status for a k6 load test.",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			testName, err := requireNameOrID(opts.ID, args)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			test, err := resolveLoadTest(cmd, client, opts.ID, opts.ProjectID, testName)
			if err != nil {
				return err
			}
			runs, err := client.ListTestRuns(ctx, test.ID)
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				return fmt.Errorf("no test runs found for load test %d", test.ID)
			}

			run := runs[0]
			resultStr := resultStatusString(run.ResultStatus)
			fmt.Fprintf(cmd.OutOrStdout(), "Run ID:  %d\nStatus:  %s\nResult:  %s\nCreated: %s\nEnded:   %s\n",
				run.ID, run.Status, resultStr, run.Created, run.Ended)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newTestrunRunsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Query k6 Cloud test run history.",
	}
	cmd.AddCommand(newTestrunRunsListCommand(loader))
	return cmd
}

type testrunRunsListOpts struct {
	IO        cmdio.Options
	ProjectID int
	ID        int
}

func (o *testrunRunsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TestRunTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.ProjectID, "project-id", 0, "k6 Cloud project ID (required when using name lookup)")
	flags.IntVar(&o.ID, "id", 0, "Load test ID (skip name lookup)")
}

func newTestrunRunsListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testrunRunsListOpts{}
	cmd := &cobra.Command{
		Use:   "list [test-name]",
		Short: "List all test runs for a k6 load test.",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			testName, err := requireNameOrID(opts.ID, args)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := authenticatedClient(ctx, loader)
			if err != nil {
				return err
			}

			test, err := resolveLoadTest(cmd, client, opts.ID, opts.ProjectID, testName)
			if err != nil {
				return err
			}
			runs, err := client.ListTestRuns(ctx, test.ID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), runs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// testrun K8s manifest helpers
// ---------------------------------------------------------------------------

// slugifyK8sName converts a test name to a valid RFC 1123 DNS label for Kubernetes metadata.name.
func slugifyK8sName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	inHyphen := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			inHyphen = false
		} else if !inHyphen {
			b.WriteRune('-')
			inHyphen = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if len(result) > 63 {
		result = strings.TrimRight(result[:63], "-")
	}
	return result
}

// injectCloudOptions appends or injects an options.cloud block into a k6 script so the k6
// Operator routes the run to the correct project.
func injectCloudOptions(script string, projectID int, name string) string {
	if strings.Contains(script, "options.cloud") ||
		(strings.Contains(script, "cloud:") && strings.Contains(script, "projectID:")) {
		return script
	}

	cloudBlock := fmt.Sprintf("  cloud: {\n    projectID: %d,\n    name: %q,\n  },\n", projectID, name)

	re := regexp.MustCompile(`(export\s+(?:const|let)\s+options\s*=\s*\{)`)
	if loc := re.FindStringIndex(script); loc != nil {
		insertAt := loc[1]
		return script[:insertAt] + "\n" + cloudBlock + script[insertAt:]
	}

	return script + fmt.Sprintf("\n\nexport const options = {\n%s};\n", cloudBlock)
}

// testrunK8sManifests generates Kubernetes YAML for a k6 TestRun and its script ConfigMap.
func testrunK8sManifests(namespace, secretName, testName, script string, parallelism, projectID int, emitSecret bool) string {
	k8sName := slugifyK8sName(testName)
	script = injectCloudOptions(script, projectID, testName)
	var indented strings.Builder
	for line := range strings.SplitSeq(script, "\n") {
		indented.WriteString("    ")
		indented.WriteString(line)
		indented.WriteString("\n")
	}
	var result strings.Builder
	if emitSecret {
		fmt.Fprintf(&result, `---
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
stringData:
  token: "<YOUR_GRAFANA_STACK_SERVICE_ACCOUNT_TOKEN>"
`, secretName, namespace)
	} else {
		fmt.Fprintf(&result, "# NOTE: ensure Secret %q exists in namespace %q with key \"token\"\n", secretName, namespace)
	}
	fmt.Fprintf(&result, `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s-script
  namespace: %s
  annotations:
    k6.io/test-name: %q
data:
  test.js: |
%s---
apiVersion: k6.io/v1alpha1
kind: TestRun
metadata:
  name: %s
  namespace: %s
  annotations:
    k6.io/test-name: %q
spec:
  parallelism: %d
  script:
    configMap:
      name: %s-script
      file: test.js
  arguments: --out cloud
  token: %s
`, k8sName, namespace, testName, indented.String(), k8sName, namespace, testName, parallelism, k8sName, secretName)
	return result.String()
}
