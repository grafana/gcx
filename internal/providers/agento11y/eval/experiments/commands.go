package experiments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/commandutil"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := agento11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the experiments command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "experiments",
		Short: "Manage eval experiment runs.",
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newCancelCommand(loader),
		newListScoresCommand(loader),
		newGetReportCommand(loader),
		newListTrialsCommand(loader),
		newPullCommand(loader),
		newTestSuitesCommand(loader),
		newTrialsCommand(loader),
	)
	return cmd
}

func readDataFile[T any](path string, stdin io.Reader) (*T, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var out T
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	default:
		jsonErr := json.Unmarshal(data, &out)
		if jsonErr != nil {
			var yamlOut T
			if yamlErr := yaml.Unmarshal(data, &yamlOut); yamlErr != nil {
				return nil, fmt.Errorf("parsing %s as JSON or YAML: %w", path, errors.Join(jsonErr, yamlErr))
			}
			out = yamlOut
		}
	}
	return &out, nil
}

func exactArgsWithSuggestion(expected int, usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) == expected {
			return nil
		}
		return fmt.Errorf("expected format: %s (received %d args)", usage, len(args))
	}
}

// --- list ---

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, Table())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of experiments to return (0 for no limit)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List experiments.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.List(cmd.Context(), int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <run-id>",
		Short: "Get a single experiment by run ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			exp, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), exp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- create ---

type createOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the experiment create payload (use - for stdin)")
}

func (o *createOpts) Validate() error {
	if strings.TrimSpace(o.File) == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func readExperimentFile(path string, stdin io.Reader) (*Experiment, error) {
	exp, err := readDataFile[Experiment](path, stdin)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(exp.Name) == "" {
		return nil, fmt.Errorf("parsing %s: name is required", path)
	}
	return exp, nil
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new experiment from a JSON or YAML file.",
		Example: `  # Create from a YAML file.
  gcx agento11y experiments create -f experiment.yaml

  # Create from stdin.
  cat experiment.json | gcx agento11y experiments create -f -`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			exp, err := readExperimentFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			created, err := client.Create(cmd.Context(), exp)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Experiment %s created", created.ID())
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- update ---

type updateOpts struct {
	IO          cmdio.Options
	Name        string
	Description string
	Tags        []string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "New experiment name")
	flags.StringVar(&o.Description, "description", "", "New experiment description; pass an empty string to clear")
	flags.StringSliceVar(&o.Tags, "tag", nil, "Experiment tag (repeatable or comma-separated; replaces all tags)")
}

// newUpdateCommand sends a true partial PATCH using pointer fields gated by
// cmd.Flags().Changed(...). Only fields the user explicitly sets are sent on the
// wire. Tags replace the full tag set when --tag is present; pass --tag "" to
// clear tags. Status and error are intentionally not exposed — they are
// server-managed lifecycle fields; use `cancel` for the one user-driven transition.
func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <run-id>",
		Short: "Update an experiment's mutable fields.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			req := &UpdateRequest{}
			if cmd.Flags().Changed("name") {
				name := opts.Name
				req.Name = &name
			}
			if cmd.Flags().Changed("description") {
				description := opts.Description
				req.Description = &description
			}
			if cmd.Flags().Changed("tag") {
				tags := opts.Tags
				req.Tags = &tags
			}
			if req.Name == nil && req.Description == nil && req.Tags == nil {
				return errors.New("--name, --description, or --tag is required")
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			updated, err := client.Update(cmd.Context(), args[0], req)
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Experiment %s updated", updated.ID())
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	cmd.InitDefaultHelpFlag()
	flags := cmd.Flags()
	flags.SortFlags = false
	opts.setup(flags)
	return cmd
}

// --- cancel ---

type cancelOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *cancelOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	// The cancel result is a SingleMutation document through the codec
	// system: the human text default stays silent (the receipt goes to
	// stderr, as it always has); agent mode and explicit -o json/yaml get
	// the structured document.
	o.IO.RegisterCustomCodec("text", commandutil.SilentTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newCancelCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &cancelOpts{}
	cmd := &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a running experiment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Cancel experiment %s?", args[0]))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			if err := client.Cancel(cmd.Context(), args[0]); err != nil {
				return err
			}
			return emitCancelReceipt(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, args[0])
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// emitCancelReceipt writes the stderr receipt and the stdout result document
// for a completed cancel call. Split from RunE so the output contract is
// testable without a live plugin API.
func emitCancelReceipt(stdout, stderr io.Writer, opts *cancelOpts, runID string) error {
	cmdio.Success(stderr, "Experiment %s canceled", runID)
	result := cmdio.NewSingleMutation("canceled", cmdio.MutationTarget{Kind: "experiment", ID: runID})
	return opts.IO.Encode(stdout, result)
}

// --- list-scores ---

type scoresOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *scoresOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, ScoresTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of scores to return (0 for no limit)")
}

func newListScoresCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &scoresOpts{}
	cmd := &cobra.Command{
		Use:   "list-scores <run-id>",
		Short: "List scores produced by an experiment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListScores(cmd.Context(), args[0], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get-report ---

type reportOpts struct {
	IO cmdio.Options
}

func (o *reportOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &ReportTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newGetReportCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &reportOpts{}
	cmd := &cobra.Command{
		Use:   "get-report <run-id>",
		Short: "Get the aggregate report for an experiment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			report, err := client.GetReport(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), report)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- test-suites ---

func newTestSuitesCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "test-suites",
		Aliases: []string{"suites"},
		Short:   "Manage experiment test suites.",
	}
	cmd.AddCommand(
		newSuitesListCommand(loader),
		newSuitesGetCommand(loader),
		newSuitesCreateCommand(loader),
		newSuitesUpdateCommand(loader),
		newSuiteVersionsCommand(loader),
		newSuiteCasesCommand(loader),
	)
	return cmd
}

type suitesListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *suitesListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, SuitesTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of test suites to return (0 for no limit)")
}

func newSuitesListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &suitesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List test suites.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListSuites(cmd.Context(), int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newSuitesGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <suite-id>",
		Short: "Get a single test suite.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			suite, err := client.GetSuite(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), suite)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type suiteCreateOpts struct {
	IO          cmdio.Options
	File        string
	SuiteID     string
	Name        string
	Description string
	Tags        []string
}

func (o *suiteCreateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the test suite create payload (use - for stdin)")
	flags.StringVar(&o.SuiteID, "suite-id", "", "Stable test suite id")
	flags.StringVar(&o.Name, "name", "", "Test suite name")
	flags.StringVar(&o.Description, "description", "", "Test suite description")
	flags.StringSliceVar(&o.Tags, "tag", nil, "Test suite tag (repeatable or comma-separated)")
}

func newSuitesCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &suiteCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a test suite.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			var suite *TestSuite
			var err error
			if opts.File != "" {
				suite, err = readDataFile[TestSuite](opts.File, cmd.InOrStdin())
				if err != nil {
					return err
				}
				if strings.TrimSpace(suite.Name) == "" {
					return fmt.Errorf("parsing %s: name is required", opts.File)
				}
			} else {
				if strings.TrimSpace(opts.Name) == "" {
					return errors.New("--filename/-f or --name is required")
				}
				suite = &TestSuite{SuiteID: opts.SuiteID, Name: opts.Name, Description: opts.Description, Tags: opts.Tags}
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			created, err := client.CreateSuite(cmd.Context(), suite)
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Test suite %s created", created.SuiteID)
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type suiteUpdateOpts struct {
	IO          cmdio.Options
	Name        string
	Description string
	Tags        []string
}

func (o *suiteUpdateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "New test suite name")
	flags.StringVar(&o.Description, "description", "", "New test suite description; pass an empty string to clear")
	flags.StringSliceVar(&o.Tags, "tag", nil, "Test suite tag (repeatable or comma-separated; replaces all tags)")
}

func newSuitesUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &suiteUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <suite-id>",
		Short: "Update a test suite.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			req := &UpdateTestSuiteRequest{}
			if cmd.Flags().Changed("name") {
				name := opts.Name
				req.Name = &name
			}
			if cmd.Flags().Changed("description") {
				description := opts.Description
				req.Description = &description
			}
			if cmd.Flags().Changed("tag") {
				tags := opts.Tags
				req.Tags = &tags
			}
			if req.Name == nil && req.Description == nil && req.Tags == nil {
				return errors.New("--name, --description, or --tag is required")
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			updated, err := client.UpdateSuite(cmd.Context(), args[0], req)
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Test suite %s updated", updated.SuiteID)
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newSuiteVersionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions",
		Short: "Manage test suite versions.",
	}
	cmd.AddCommand(newSuiteVersionCreateCommand(loader), newSuiteVersionPublishCommand(loader))
	return cmd
}

type suiteVersionCreateOpts struct {
	IO         cmdio.Options
	Changelog  string
	EmptyDraft bool
}

func (o *suiteVersionCreateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Changelog, "changelog", "", "Version changelog")
	flags.BoolVar(&o.EmptyDraft, "empty-draft", false, "Create an empty draft instead of cloning the latest published version")
}

func newSuiteVersionCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &suiteVersionCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create <suite-id>",
		Short: "Create a draft test suite version.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			version, err := client.CreateSuiteVersion(cmd.Context(), args[0], &CreateTestSuiteVersionRequest{Changelog: opts.Changelog, EmptyDraft: opts.EmptyDraft})
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Test suite version %s/%s created", version.SuiteID, version.Version)
			return opts.IO.Encode(cmd.OutOrStdout(), version)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newSuiteVersionPublishCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "publish <suite-id> <version>",
		Short: "Publish a draft test suite version.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			version, err := client.PublishSuiteVersion(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Test suite version %s/%s published", version.SuiteID, version.Version)
			return opts.IO.Encode(cmd.OutOrStdout(), version)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newSuiteCasesCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cases",
		Short: "Manage test cases in a suite version.",
	}
	cmd.AddCommand(
		newCasesListCommand(loader),
		newCasesGetCommand(loader),
		newCasesUpsertCommand(loader),
		newCasesUpdateCommand(loader),
		newCasesDeleteCommand(loader),
	)
	return cmd
}

type casesListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *casesListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, CasesTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of test cases to return (0 for no limit)")
}

func newCasesListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &casesListOpts{}
	cmd := &cobra.Command{
		Use:   "list <suite-id> <version>",
		Short: "List test cases in a suite version.",
		Args:  exactArgsWithSuggestion(2, "gcx agento11y experiments test-suites cases list <suite-id> <version>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListCases(cmd.Context(), args[0], args[1], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newCasesGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <suite-id> <version> <test-case-id>",
		Short: "Get a single test case.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			tc, err := client.GetCase(cmd.Context(), args[0], args[1], args[2])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), tc)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type fileOpts struct {
	IO   cmdio.Options
	File string
}

func (o *fileOpts) setup(flags *pflag.FlagSet, description string) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", description)
}

func (o *fileOpts) Validate() error {
	if strings.TrimSpace(o.File) == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newCasesUpsertCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &fileOpts{}
	cmd := &cobra.Command{
		Use:   "upsert <suite-id> <version>",
		Short: "Create or replace a test case from a JSON or YAML file.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			tc, err := readDataFile[TestCase](opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			out, err := client.UpsertCase(cmd.Context(), args[0], args[1], tc)
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Test case %s upserted", out.TestCaseID)
			return opts.IO.Encode(cmd.OutOrStdout(), out)
		},
	}
	opts.setup(cmd.Flags(), "File containing the test case payload (use - for stdin)")
	return cmd
}

func newCasesUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &fileOpts{}
	cmd := &cobra.Command{
		Use:   "update <suite-id> <version> <test-case-id>",
		Short: "Update a test case from a JSON or YAML file.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			patch, err := readDataFile[map[string]any](opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			out, err := client.PatchCase(cmd.Context(), args[0], args[1], args[2], *patch)
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Test case %s updated", out.TestCaseID)
			return opts.IO.Encode(cmd.OutOrStdout(), out)
		},
	}
	opts.setup(cmd.Flags(), "File containing the test case patch payload (use - for stdin)")
	return cmd
}

type deleteCaseOpts struct {
	Force bool
}

func (o *deleteCaseOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func newCasesDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteCaseOpts{}
	cmd := &cobra.Command{
		Use:   "delete <suite-id> <version> <test-case-id>",
		Short: "Delete a test case from a mutable suite version.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete test case %s from %s/%s?", args[2], args[0], args[1]))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			if err := client.DeleteCase(cmd.Context(), args[0], args[1], args[2]); err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Test case %s deleted", args[2])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- trials ---

func newTrialsCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trials",
		Short: "Manage experiment test case trials.",
	}
	cmd.AddCommand(
		newTrialsGetCommand(loader),
		newTrialsCreateCommand(loader),
		newTrialsUpdateCommand(loader),
		newTrialListScoresCommand(loader),
		newTrialListArtifactsCommand(loader),
	)
	return cmd
}

type trialsListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *trialsListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, TrialsTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of trials to return (0 for no limit)")
}

func newListTrialsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &trialsListOpts{}
	cmd := &cobra.Command{
		Use:   "list-trials <run-id>",
		Short: "List test case trials for an experiment.",
		Args:  exactArgsWithSuggestion(1, "gcx agento11y experiments list-trials <run-id>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListTrials(cmd.Context(), args[0], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newTrialsGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <trial-id>",
		Short: "Get a single test case trial.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			trial, err := client.GetTrial(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), trial)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newTrialMutationCommand[T any](
	loader *providers.ConfigLoader,
	use string,
	short string,
	fileHelp string,
	successVerb string,
	apply func(context.Context, *Client, string, *T) (*TestCaseTrial, error),
) *cobra.Command {
	opts := &fileOpts{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			payload, err := readDataFile[T](opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			out, err := apply(cmd.Context(), client, args[0], payload)
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Trial %s %s", out.TrialID, successVerb)
			return opts.IO.Encode(cmd.OutOrStdout(), out)
		},
	}
	opts.setup(cmd.Flags(), fileHelp)
	return cmd
}

func newTrialsCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	return newTrialMutationCommand[TestCaseTrial](
		loader,
		"create <run-id>",
		"Create or upsert a test case trial from a JSON or YAML file.",
		"File containing the trial payload (use - for stdin)",
		"created",
		func(ctx context.Context, client *Client, experimentID string, trial *TestCaseTrial) (*TestCaseTrial, error) {
			return client.CreateTrial(ctx, experimentID, trial)
		},
	)
}

func newTrialsUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	return newTrialMutationCommand[UpdateTrialRequest](
		loader,
		"update <trial-id>",
		"Update a test case trial from a JSON or YAML file.",
		"File containing the trial patch payload (use - for stdin)",
		"updated",
		func(ctx context.Context, client *Client, trialID string, req *UpdateTrialRequest) (*TestCaseTrial, error) {
			return client.UpdateTrial(ctx, trialID, req)
		},
	)
}

func newTrialListScoresCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &scoresOpts{}
	cmd := &cobra.Command{
		Use:   "list-scores <trial-id>",
		Short: "List scores for a test case trial.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListTrialScores(cmd.Context(), args[0], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type artifactsOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *artifactsOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, ArtifactsTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of artifacts to return (0 for no limit)")
}

func newTrialListArtifactsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &artifactsOpts{}
	cmd := &cobra.Command{
		Use:   "list-artifacts <trial-id>",
		Short: "List artifacts for a test case trial.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListTrialArtifacts(cmd.Context(), args[0], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codecs ---

func SuitesTable() cmdio.Table[TestSuite] {
	return cmdio.Table[TestSuite]{Columns: []cmdio.Column[TestSuite]{
		{Header: "SUITE-ID", Content: func(r TestSuite) string { return r.SuiteID }},
		{Header: "NAME", Content: func(r TestSuite) string { return r.Name }},
		{Header: "LATEST", Content: func(r TestSuite) string {
			if r.LatestVersion == "" {
				return "-"
			}
			return r.LatestVersion
		}},
		{Header: "VERSIONS", Content: func(r TestSuite) string { return strconv.Itoa(len(r.Versions)) }},
		{Header: "TAGS", Content: func(r TestSuite) string { return formatTags(r.Tags) }},
		{Header: "CREATED", Content: func(r TestSuite) string { return agento11yhttp.FormatTime(r.CreatedAt) }},
		{Header: "UPDATED", Visible: cmdio.WideOnly, Content: func(r TestSuite) string { return agento11yhttp.FormatTime(r.UpdatedAt) }},
		{Header: "DESCRIPTION", Visible: cmdio.WideOnly, Content: func(r TestSuite) string { return agento11yhttp.Truncate(r.Description, 60) }},
	}}
}

func CasesTable() cmdio.Table[TestCase] {
	return cmdio.Table[TestCase]{Columns: []cmdio.Column[TestCase]{
		{Header: "TEST-CASE-ID", Content: func(r TestCase) string { return r.TestCaseID }},
		{Header: "NAME", Content: func(r TestCase) string {
			if r.Name == "" {
				return "-"
			}
			return r.Name
		}},
		{Header: "CATEGORY", Content: func(r TestCase) string {
			if r.Category == "" {
				return "-"
			}
			return r.Category
		}},
		{Header: "TAGS", Content: func(r TestCase) string { return formatTags(r.Tags) }},
		{Header: "SUITE", Content: func(r TestCase) string { return r.SuiteID }},
		{Header: "VERSION", Content: func(r TestCase) string { return r.SuiteVersion }},
		{Header: "CREATED", Visible: cmdio.WideOnly, Content: func(r TestCase) string { return agento11yhttp.FormatTime(r.CreatedAt) }},
		{Header: "UPDATED", Visible: cmdio.WideOnly, Content: func(r TestCase) string { return agento11yhttp.FormatTime(r.UpdatedAt) }},
		{Header: "DESCRIPTION", Visible: cmdio.WideOnly, Content: func(r TestCase) string { return agento11yhttp.Truncate(r.Description, 60) }},
	}}
}

func TrialsTable() cmdio.Table[TestCaseTrial] {
	return cmdio.Table[TestCaseTrial]{Columns: []cmdio.Column[TestCaseTrial]{
		{Header: "TRIAL-ID", Content: func(r TestCaseTrial) string { return r.TrialID }},
		{Header: "EXPERIMENT-ID", Content: func(r TestCaseTrial) string { return r.ExperimentID }},
		{Header: "TEST-CASE-ID", Content: func(r TestCaseTrial) string { return r.TestCaseID }},
		{Header: "ATTEMPT", Content: func(r TestCaseTrial) string { return strconv.Itoa(r.Attempt) }},
		{Header: "STATUS", Content: func(r TestCaseTrial) string {
			if r.Status == "" {
				return "-"
			}
			return r.Status
		}},
		{Header: "CONVERSATION", Content: func(r TestCaseTrial) string {
			if r.ConversationID == "" {
				return "-"
			}
			return r.ConversationID
		}},
		{Header: "TRACE", Content: func(r TestCaseTrial) string {
			if r.TraceID == "" {
				return "-"
			}
			return r.TraceID
		}},
		{Header: "TOTAL-TOKENS", Visible: cmdio.WideOnly, Content: func(r TestCaseTrial) string {
			if r.TotalTokens == nil {
				return "-"
			}
			return strconv.FormatInt(*r.TotalTokens, 10)
		}},
		{Header: "DURATION-MS", Visible: cmdio.WideOnly, Content: func(r TestCaseTrial) string {
			if r.DurationMS == nil {
				return "-"
			}
			return strconv.FormatInt(*r.DurationMS, 10)
		}},
		{Header: "CREATED", Visible: cmdio.WideOnly, Content: func(r TestCaseTrial) string { return agento11yhttp.FormatTime(r.CreatedAt) }},
		{Header: "COMPLETED", Visible: cmdio.WideOnly, Content: func(r TestCaseTrial) string {
			if r.CompletedAt == nil {
				return "-"
			}
			return agento11yhttp.FormatTime(*r.CompletedAt)
		}},
		{Header: "ERROR", Visible: cmdio.WideOnly, Content: func(r TestCaseTrial) string { return agento11yhttp.Truncate(r.Error, 40) }},
	}}
}

func ArtifactsTable() cmdio.Table[Artifact] {
	return cmdio.Table[Artifact]{Columns: []cmdio.Column[Artifact]{
		{Header: "ARTIFACT-ID", Content: func(r Artifact) string { return r.ArtifactID }},
		{Header: "NAME", Content: func(r Artifact) string { return r.Name }},
		{Header: "KIND", Content: func(r Artifact) string { return r.Kind }},
		{Header: "MIME", Content: func(r Artifact) string {
			if r.Mime == "" {
				return "-"
			}
			return r.Mime
		}},
		{Header: "PARENT-KIND", Visible: cmdio.WideOnly, Content: func(r Artifact) string { return r.ParentKind }},
		{Header: "PARENT-ID", Visible: cmdio.WideOnly, Content: func(r Artifact) string { return r.ParentID }},
		{Header: "SIZE", Content: func(r Artifact) string {
			if r.SizeBytes > 0 {
				return strconv.FormatInt(r.SizeBytes, 10)
			}
			return "-"
		}},
		{Header: "CREATED", Visible: cmdio.WideOnly, Content: func(r Artifact) string { return agento11yhttp.FormatTime(r.CreatedAt) }},
	}}
}

func Table() cmdio.Table[Experiment] {
	return cmdio.Table[Experiment]{Columns: []cmdio.Column[Experiment]{
		{Header: "EXPERIMENT-ID", Content: func(r Experiment) string { return r.ID() }},
		{Header: "NAME", Content: func(r Experiment) string { return r.Name }},
		{Header: "STATUS", Content: func(r Experiment) string {
			if r.Status == "" {
				return "-"
			}
			return r.Status
		}},
		{Header: "SUITE", Content: func(r Experiment) string {
			if r.SuiteID == "" {
				return "-"
			}
			return r.SuiteID
		}},
		{Header: "VERSION", Content: func(r Experiment) string {
			if r.SuiteVersion == "" {
				return "-"
			}
			return r.SuiteVersion
		}},
		{Header: "TAGS", Visible: cmdio.WideOnly, Content: func(r Experiment) string { return formatTags(r.Tags) }},
		{Header: "TRIALS", Content: func(r Experiment) string {
			if r.Result == nil {
				return "-"
			}
			return strconv.Itoa(r.Result.TrialCount)
		}},
		{Header: "PASS", Content: func(r Experiment) string {
			if r.Result == nil || r.Result.PassRate == nil {
				return "-"
			}
			return fmt.Sprintf("%.2f%%", *r.Result.PassRate*100)
		}},
		{Header: "CREATED", Content: func(r Experiment) string { return agento11yhttp.FormatTime(r.CreatedAt) }},
		{Header: "COMPLETED", Visible: cmdio.WideOnly, Content: func(r Experiment) string {
			if r.CompletedAt == nil {
				return "-"
			}
			return agento11yhttp.FormatTime(*r.CompletedAt)
		}},
		{Header: "DESCRIPTION", Visible: cmdio.WideOnly, Content: func(r Experiment) string { return agento11yhttp.Truncate(r.Description, 40) }},
		{Header: "ERROR", Visible: cmdio.WideOnly, Content: func(r Experiment) string {
			// The rollup can fail on its own while the experiment succeeds.
			// TRIALS and PASS then render "-" like an experiment with no
			// trials, so without this fallback the stored reason never
			// reaches the table.
			failure := r.Error
			if failure == "" {
				failure = r.ResultError
			}
			return agento11yhttp.Truncate(failure, 40)
		}},
	}}
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	return strings.Join(tags, ", ")
}

func ScoresTable() cmdio.Table[ScoreItem] {
	return cmdio.Table[ScoreItem]{Columns: []cmdio.Column[ScoreItem]{
		{Header: "SCORE-ID", Content: func(r ScoreItem) string { return r.ScoreID }},
		{Header: "EVALUATOR", Content: func(r ScoreItem) string {
			if r.EvaluatorID == "" {
				return "-"
			}
			return r.EvaluatorID
		}},
		{Header: "KEY", Content: func(r ScoreItem) string {
			if r.ScoreKey == "" {
				return "-"
			}
			return r.ScoreKey
		}},
		{Header: "VALUE", Content: func(r ScoreItem) string { return r.Value.Display() }},
		{Header: "PASSED", Content: func(r ScoreItem) string {
			if r.Passed == nil {
				return "-"
			}
			return strconv.FormatBool(*r.Passed)
		}},
		{Header: "GENERATION", Content: func(r ScoreItem) string {
			if r.GenerationID == "" {
				return "-"
			}
			return r.GenerationID
		}},
		{Header: "EXPLANATION", Visible: cmdio.WideOnly, Content: func(r ScoreItem) string { return agento11yhttp.Truncate(r.Explanation, 40) }},
		{Header: "CREATED", Visible: cmdio.WideOnly, Content: func(r ScoreItem) string { return agento11yhttp.FormatTime(r.CreatedAt) }},
	}}
}

// ReportTextCodec renders an *ExperimentReport (or ExperimentReport) as a
// human-readable summary.
type ReportTextCodec struct{}

func (c *ReportTextCodec) Format() format.Format {
	return "text"
}

func (c *ReportTextCodec) Encode(w io.Writer, v any) error {
	var r *ExperimentReport
	switch val := v.(type) {
	case *ExperimentReport:
		r = val
	case ExperimentReport:
		r = &val
	default:
		return errors.New("invalid data type for report text codec: expected *ExperimentReport")
	}
	if r == nil {
		return errors.New("invalid data type for report text codec: expected *ExperimentReport")
	}

	const labelFmt = "%-15s %s\n"
	exp := r.Experiment
	if exp.ID() != "" {
		fmt.Fprintf(w, labelFmt, "Experiment:", exp.ID())
	}
	if exp.Name != "" {
		fmt.Fprintf(w, labelFmt, "Name:", exp.Name)
	}
	if exp.Status != "" {
		fmt.Fprintf(w, labelFmt, "Status:", exp.Status)
	}
	if exp.Error != "" {
		fmt.Fprintf(w, labelFmt, "Error:", exp.Error)
	}
	s := r.Summary
	fmt.Fprintf(w, labelFmt, "Test cases:", strconv.Itoa(s.TestCaseCount))
	fmt.Fprintf(w, labelFmt, "Trials:", strconv.Itoa(s.TrialCount))
	fmt.Fprintf(w, labelFmt, "Completed:", strconv.Itoa(s.CompletedCount))
	if s.FailedCount > 0 {
		fmt.Fprintf(w, labelFmt, "Failed:", strconv.Itoa(s.FailedCount))
	}
	if s.CanceledCount > 0 {
		fmt.Fprintf(w, labelFmt, "Canceled:", strconv.Itoa(s.CanceledCount))
	}

	passRate := "-"
	if s.PassRate != nil {
		passRate = fmt.Sprintf("%.2f%%", *s.PassRate*100)
	}
	fmt.Fprintf(w, labelFmt, "Pass rate:", passRate)

	if s.FinalScoreAvg != nil {
		fmt.Fprintf(w, labelFmt, "Final avg:", fmt.Sprintf("%g", *s.FinalScoreAvg))
	}

	cost := "-"
	if s.TotalCost != nil {
		cost = fmt.Sprintf("$%.4f%s", *s.TotalCost, coverageNote(s.CostCoverage))
	}
	fmt.Fprintf(w, labelFmt, "Cost:", cost)

	tokens := "-"
	if s.TotalTokens != nil {
		tokens = strconv.FormatInt(*s.TotalTokens, 10) + coverageNote(s.TokenCoverage)
	}
	fmt.Fprintf(w, labelFmt, "Tokens:", tokens)
	return nil
}

// coverageNote flags a total whose inputs were not all reported, so a reader
// does not take a low number for a complete one. The API pairs a token total
// with coverage "none" when it summed input and output tokens because no trial
// carried a total of its own. It returns an empty coverage on the progress
// snapshot of a running experiment.
func coverageNote(coverage string) string {
	if coverage == "" || coverage == "complete" {
		return ""
	}
	return " (coverage: " + coverage + ")"
}

func (c *ReportTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}
