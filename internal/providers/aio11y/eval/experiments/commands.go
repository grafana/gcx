package experiments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/commandutil"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromCommand(cmd, loader)
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
		newCheckCommand(loader),
		newListTrialsCommand(loader),
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
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
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
	o.IO.RegisterCustomCodec("table", &ScoresTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ScoresTableCodec{Wide: true})
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

// --- check ---

type checkOpts struct {
	IO                 cmdio.Options
	MinPassRate        float64
	MinVerdictCoverage float64
	OnUnknown          string
}

func (o *checkOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &CheckTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.Float64Var(&o.MinPassRate, "min-pass-rate", 0,
		"Lowest acceptable pass rate, 0..1 (required). Set to 0 to accept any measured rate; "+
			"a rate that cannot be measured is still graded by --on-unknown. "+
			"See --help for how pass_rate is measured.")
	flags.Float64Var(&o.MinVerdictCoverage, "min-verdict-coverage", 1,
		"Lowest acceptable share of test cases that produced a pass or fail verdict, 0..1. "+
			"Set to 0 to skip this check.")
	// One line rather than the indented block --on-error uses: cobra appends its
	// own (default "fail") to the last line, which would land beside the pass row
	// and read as though pass were the default.
	flags.StringVar(&o.OnUnknown, "on-unknown", onUnknownFail,
		"Verdict for a check that could not measure what it grades: fail (exit 4) or pass (exit 0).")
}

func (o *checkOpts) Validate(cmd *cobra.Command) error {
	if err := validateRateFlag(cmd, "--min-pass-rate", o.MinPassRate); err != nil {
		return err
	}
	if err := validateRateFlag(cmd, "--min-verdict-coverage", o.MinVerdictCoverage); err != nil {
		return err
	}
	if err := validateOnUnknown(o.OnUnknown); err != nil {
		return fail.NewCommandUsageError(cmd, "", err)
	}
	return o.IO.Validate()
}

// spec resolves the validated flags into the thresholds evaluateChecks applies.
func (o *checkOpts) spec() checkSpec {
	return checkSpec{
		MinPassRate:        o.MinPassRate,
		MinVerdictCoverage: o.MinVerdictCoverage,
		OnUnknown:          o.OnUnknown,
	}
}

// validateRateFlag rejects a rate outside 0 through 1. Go's float flag parser
// accepts NaN and Inf, and a range comparison alone catches neither, so this
// function rejects them explicitly.
func validateRateFlag(cmd *cobra.Command, name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return fail.NewCommandUsageError(cmd,
			fmt.Sprintf("invalid %s value %v: must be a finite value from 0 through 1, for example %s 0.9", name, value, name),
			nil)
	}
	return nil
}

func newCheckCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &checkOpts{}
	cmd := &cobra.Command{
		Use:   "check <run-id>",
		Short: "Grade a finished experiment against quality thresholds for CI.",
		Long: `Compare a finished experiment against quality thresholds and exit non-zero
when the experiment falls short, so a CI job fails on a regression.

The main threshold applies to the report's pass_rate: of the test cases that
produced a verdict, the share that passed on the first completed attempt. The
denominator counts test cases, not trials. For the all-trials figure, read
rows[].summary.trial_pass_rate from get-report -o json.

A test case whose trials all failed produces no verdict, so it changes neither
side of that fraction. Take a run where every trial failed for 90 of its 100
test cases: if the surviving 10 all passed, the report says 100%.
--min-verdict-coverage rejects that run by requiring a minimum share of the
suite to have produced a verdict. It defaults to 1, the whole suite.

Exit codes:
  0  every threshold met
  1  general error, including an experiment that has not finished yet
  2  a required flag is missing, or a flag value is invalid
  4  the run missed a threshold; or the experiment finished with status failed
     or canceled; or a check could not measure what it grades while
     --on-unknown=fail (the default)

A check that cannot measure what it grades reports the verdict unknown, and
--on-unknown decides those runs: fail (the default) exits 4, and pass exits 0.
Two cases reach it. An experiment whose evaluators emit only reward or numeric
scores produces no pass or fail verdict, so there is no pass rate and no
coverage to measure. A server that does not report the size of the suite leaves
coverage unmeasurable on its own.`,
		Example: `  # Fail the build when fewer than 90% of test cases pass
  gcx agento11y experiments check <run-id> --min-pass-rate 0.9

  # Machine-readable verdict for a CI step summary
  gcx agento11y experiments check <run-id> --min-pass-rate 0.9 -o json

  # Accept a run where a fifth of the test cases produced no verdict
  gcx agento11y experiments check <run-id> --min-pass-rate 0.9 --min-verdict-coverage 0.8

  # Reward-scored experiment: no pass or fail verdict exists, so do not fail on it
  gcx agento11y experiments check <run-id> --min-pass-rate 0.9 --on-unknown pass`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd); err != nil {
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

			// Refuse to grade an unfinished experiment: exit 1 and write no
			// check document. See checkStatusError for why.
			if err := checkStatusError(args[0], reportExperiment(report).Status); err != nil {
				return err
			}

			return emitCheckResult(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, evaluateChecks(report, opts.spec()))
		},
	}
	opts.setup(cmd.Flags())
	// Both calls fail only for a flag that does not exist, which cannot happen
	// right after setup bound it.
	if err := cmd.MarkFlagRequired("min-pass-rate"); err != nil {
		panic(err)
	}
	if err := cmd.RegisterFlagCompletionFunc("on-unknown", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{onUnknownFail, onUnknownPass}, cobra.ShellCompDirectiveNoFileComp
	}); err != nil {
		panic(err)
	}
	return cmd
}

// emitCheckResult writes the single check document through the codec system,
// then maps a non-passing verdict to the partial-failure exit code so the
// process exit agrees with the verdict in the payload.
//
// The document is already on stdout, so the error is an EmittedError: the
// top-level reporter honors the code and writes nothing more, which keeps the
// one-JSON-value contract intact. EmittedError also suppresses the reporter's
// stderr rendering, hence the explicit warn line.
//
// An encode failure returns the write error instead, because the stream is
// already broken.
func emitCheckResult(stdout, stderr io.Writer, opts *checkOpts, result CheckResult) error {
	if err := opts.IO.Encode(stdout, result); err != nil {
		return err
	}
	if result.Verdict == CheckVerdictPass {
		return nil
	}
	summary := checkFailureSummary(result)
	cmdio.EmitWarn(stderr, summary)
	return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New(summary))
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
	o.IO.RegisterCustomCodec("table", &SuitesTableCodec{})
	o.IO.RegisterCustomCodec("wide", &SuitesTableCodec{Wide: true})
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
	o.IO.RegisterCustomCodec("table", &CasesTableCodec{})
	o.IO.RegisterCustomCodec("wide", &CasesTableCodec{Wide: true})
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
	o.IO.RegisterCustomCodec("table", &TrialsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &TrialsTableCodec{Wide: true})
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
	o.IO.RegisterCustomCodec("table", &ArtifactsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ArtifactsTableCodec{Wide: true})
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

// SuitesTableCodec renders []TestSuite rows.
type SuitesTableCodec struct {
	Wide bool
}

func (c *SuitesTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *SuitesTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]TestSuite)
	if !ok {
		return errors.New("invalid data type for suites table codec: expected []TestSuite")
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("SUITE-ID", "NAME", "LATEST", "VERSIONS", "TAGS", "CREATED", "UPDATED", "DESCRIPTION")
	} else {
		t = style.NewTable("SUITE-ID", "NAME", "LATEST", "VERSIONS", "TAGS", "CREATED")
	}
	for _, suite := range items {
		latest := suite.LatestVersion
		if latest == "" {
			latest = "-"
		}
		if c.Wide {
			t.Row(suite.SuiteID, suite.Name, latest, strconv.Itoa(len(suite.Versions)), formatTags(suite.Tags), aio11yhttp.FormatTime(suite.CreatedAt), aio11yhttp.FormatTime(suite.UpdatedAt), aio11yhttp.Truncate(suite.Description, 60))
		} else {
			t.Row(suite.SuiteID, suite.Name, latest, strconv.Itoa(len(suite.Versions)), formatTags(suite.Tags), aio11yhttp.FormatTime(suite.CreatedAt))
		}
	}
	return t.Render(w)
}

func (c *SuitesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// CasesTableCodec renders []TestCase rows.
type CasesTableCodec struct {
	Wide bool
}

func (c *CasesTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *CasesTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]TestCase)
	if !ok {
		return errors.New("invalid data type for cases table codec: expected []TestCase")
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("TEST-CASE-ID", "NAME", "CATEGORY", "TAGS", "SUITE", "VERSION", "CREATED", "UPDATED", "DESCRIPTION")
	} else {
		t = style.NewTable("TEST-CASE-ID", "NAME", "CATEGORY", "TAGS", "SUITE", "VERSION")
	}
	for _, tc := range items {
		name := tc.Name
		if name == "" {
			name = "-"
		}
		category := tc.Category
		if category == "" {
			category = "-"
		}
		if c.Wide {
			t.Row(tc.TestCaseID, name, category, formatTags(tc.Tags), tc.SuiteID, tc.SuiteVersion, aio11yhttp.FormatTime(tc.CreatedAt), aio11yhttp.FormatTime(tc.UpdatedAt), aio11yhttp.Truncate(tc.Description, 60))
		} else {
			t.Row(tc.TestCaseID, name, category, formatTags(tc.Tags), tc.SuiteID, tc.SuiteVersion)
		}
	}
	return t.Render(w)
}

func (c *CasesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// TrialsTableCodec renders []TestCaseTrial rows.
type TrialsTableCodec struct {
	Wide bool
}

func (c *TrialsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TrialsTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]TestCaseTrial)
	if !ok {
		return errors.New("invalid data type for trials table codec: expected []TestCaseTrial")
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("TRIAL-ID", "EXPERIMENT-ID", "TEST-CASE-ID", "ATTEMPT", "STATUS", "CONVERSATION", "TRACE", "DURATION-MS", "CREATED", "COMPLETED", "ERROR")
	} else {
		t = style.NewTable("TRIAL-ID", "EXPERIMENT-ID", "TEST-CASE-ID", "ATTEMPT", "STATUS", "CONVERSATION", "TRACE")
	}
	for _, trial := range items {
		conversation := trial.ConversationID
		if conversation == "" {
			conversation = "-"
		}
		trace := trial.TraceID
		if trace == "" {
			trace = "-"
		}
		status := trial.Status
		if status == "" {
			status = "-"
		}
		if c.Wide {
			duration := "-"
			if trial.DurationMS != nil {
				duration = strconv.FormatInt(*trial.DurationMS, 10)
			}
			completed := "-"
			if trial.CompletedAt != nil {
				completed = aio11yhttp.FormatTime(*trial.CompletedAt)
			}
			t.Row(trial.TrialID, trial.ExperimentID, trial.TestCaseID, strconv.Itoa(trial.Attempt), status, conversation, trace, duration, aio11yhttp.FormatTime(trial.CreatedAt), completed, aio11yhttp.Truncate(trial.Error, 40))
		} else {
			t.Row(trial.TrialID, trial.ExperimentID, trial.TestCaseID, strconv.Itoa(trial.Attempt), status, conversation, trace)
		}
	}
	return t.Render(w)
}

func (c *TrialsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ArtifactsTableCodec renders []Artifact rows.
type ArtifactsTableCodec struct {
	Wide bool
}

func (c *ArtifactsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ArtifactsTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]Artifact)
	if !ok {
		return errors.New("invalid data type for artifacts table codec: expected []Artifact")
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ARTIFACT-ID", "NAME", "KIND", "MIME", "PARENT-KIND", "PARENT-ID", "SIZE", "CREATED")
	} else {
		t = style.NewTable("ARTIFACT-ID", "NAME", "KIND", "MIME", "SIZE")
	}
	for _, artifact := range items {
		mime := artifact.Mime
		if mime == "" {
			mime = "-"
		}
		size := "-"
		if artifact.SizeBytes > 0 {
			size = strconv.FormatInt(artifact.SizeBytes, 10)
		}
		if c.Wide {
			t.Row(artifact.ArtifactID, artifact.Name, artifact.Kind, mime, artifact.ParentKind, artifact.ParentID, size, aio11yhttp.FormatTime(artifact.CreatedAt))
		} else {
			t.Row(artifact.ArtifactID, artifact.Name, artifact.Kind, mime, size)
		}
	}
	return t.Render(w)
}

func (c *ArtifactsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// TableCodec renders []Experiment rows.
type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]Experiment)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Experiment")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("EXPERIMENT-ID", "NAME", "STATUS", "SUITE", "VERSION", "TAGS", "SCORES", "CREATED", "COMPLETED", "DESCRIPTION", "ERROR")
	} else {
		t = style.NewTable("EXPERIMENT-ID", "NAME", "STATUS", "SUITE", "VERSION", "TAGS", "SCORES", "CREATED")
	}

	for _, exp := range items {
		id := exp.ID()
		scores := strconv.Itoa(exp.ScoreCount)
		suite := exp.SuiteID
		if suite == "" {
			suite = "-"
		}
		version := exp.SuiteVersion
		if version == "" {
			version = "-"
		}
		status := exp.Status
		if status == "" {
			status = "-"
		}
		tags := formatTags(exp.Tags)
		if c.Wide {
			completed := "-"
			if exp.CompletedAt != nil {
				completed = aio11yhttp.FormatTime(*exp.CompletedAt)
			}
			t.Row(id, exp.Name, status, suite, version, tags, scores, aio11yhttp.FormatTime(exp.CreatedAt), completed, aio11yhttp.Truncate(exp.Description, 40), aio11yhttp.Truncate(exp.Error, 40))
		} else {
			t.Row(id, exp.Name, status, suite, version, tags, scores, aio11yhttp.FormatTime(exp.CreatedAt))
		}
	}
	return t.Render(w)
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	return strings.Join(tags, ", ")
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ScoresTableCodec renders []ScoreItem rows.
type ScoresTableCodec struct {
	Wide bool
}

func (c *ScoresTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ScoresTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]ScoreItem)
	if !ok {
		return errors.New("invalid data type for scores table codec: expected []ScoreItem")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("SCORE-ID", "EVALUATOR", "KEY", "VALUE", "PASSED", "GENERATION", "EXPLANATION", "CREATED")
	} else {
		t = style.NewTable("SCORE-ID", "EVALUATOR", "KEY", "VALUE", "PASSED", "GENERATION")
	}

	for _, s := range items {
		passed := "-"
		if s.Passed != nil {
			if *s.Passed {
				passed = "true"
			} else {
				passed = "false"
			}
		}
		value := s.Value.Display()
		key := s.ScoreKey
		if key == "" {
			key = "-"
		}
		gen := s.GenerationID
		if gen == "" {
			gen = "-"
		}
		evaluator := s.EvaluatorID
		if evaluator == "" {
			evaluator = "-"
		}
		if c.Wide {
			t.Row(s.ScoreID, evaluator, key, value, passed, gen, aio11yhttp.Truncate(s.Explanation, 40), aio11yhttp.FormatTime(s.CreatedAt))
		} else {
			t.Row(s.ScoreID, evaluator, key, value, passed, gen)
		}
	}
	return t.Render(w)
}

func (c *ScoresTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ReportTextCodec renders an *ExperimentReport (or ExperimentReport) as a
// human-readable summary with per-breakdown totals.
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
	run := reportExperiment(r)
	if run.ID() != "" {
		fmt.Fprintf(w, labelFmt, "Experiment:", run.ID())
	}
	if run.Name != "" {
		fmt.Fprintf(w, labelFmt, "Name:", run.Name)
	}
	if run.Status != "" {
		fmt.Fprintf(w, labelFmt, "Status:", run.Status)
	}
	s := r.Summary
	if s.TestCaseCount > 0 || s.TrialCount > 0 {
		fmt.Fprintf(w, labelFmt, "Test cases:", strconv.Itoa(s.TestCaseCount))
		fmt.Fprintf(w, labelFmt, "Trials:", strconv.Itoa(s.TrialCount))
		fmt.Fprintf(w, labelFmt, "Completed:", strconv.Itoa(s.CompletedCount))
		if s.FailedCount > 0 {
			fmt.Fprintf(w, labelFmt, "Failed:", strconv.Itoa(s.FailedCount))
		}
		if s.CanceledCount > 0 {
			fmt.Fprintf(w, labelFmt, "Canceled:", strconv.Itoa(s.CanceledCount))
		}
	}
	if s.NScores > 0 {
		fmt.Fprintf(w, labelFmt, "Scores:", strconv.Itoa(s.NScores))
	}
	if s.NConversations > 0 {
		fmt.Fprintf(w, labelFmt, "Conversations:", strconv.Itoa(s.NConversations))
	}
	if s.NGenerations > 0 {
		fmt.Fprintf(w, labelFmt, "Generations:", strconv.Itoa(s.NGenerations))
	}
	if s.PassRate != nil {
		fmt.Fprintf(w, labelFmt, "Pass rate:", formatRate(*s.PassRate))
	} else {
		fmt.Fprintf(w, labelFmt, "Pass rate:", "n/a (no pass verdict)")
	}
	if s.MeanScore > 0 {
		fmt.Fprintf(w, labelFmt, "Mean score:", fmt.Sprintf("%g", s.MeanScore))
	}
	if s.FinalScoreAvg != nil {
		fmt.Fprintf(w, labelFmt, "Final avg:", fmt.Sprintf("%g", *s.FinalScoreAvg))
	}
	if s.TotalCostUSD > 0 {
		fmt.Fprintf(w, labelFmt, "Cost:", fmt.Sprintf("$%.4f", s.TotalCostUSD))
	} else if s.TotalCost != nil {
		fmt.Fprintf(w, labelFmt, "Cost:", fmt.Sprintf("$%.4f", *s.TotalCost))
	}
	if s.TotalTokens != nil {
		fmt.Fprintf(w, labelFmt, "Tokens:", strconv.FormatInt(*s.TotalTokens, 10))
	}

	breakdowns := reportBreakdownRows(r.Breakdowns)
	if len(breakdowns) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Breakdowns:")
		for _, row := range breakdowns {
			b := row.breakdown
			key := b.Key
			if key == "" {
				key = "-"
			}
			fmt.Fprintf(w, "  %s/%s: count=%d", row.group, key, b.Count)
			if b.Count > 0 {
				fmt.Fprintf(w, " pass_rate=%.2f%% mean_score=%g", b.PassRate*100, b.MeanScore)
			}
			if b.TotalCostUSD > 0 {
				fmt.Fprintf(w, " cost=$%.4f", b.TotalCostUSD)
			}
			if b.TotalTokens > 0 {
				fmt.Fprintf(w, " tokens=%d", b.TotalTokens)
			}
			fmt.Fprintln(w)
		}
	}
	return nil
}

type reportBreakdownRow struct {
	group     string
	breakdown ExperimentReportBreakdown
}

func reportBreakdownRows(b ExperimentReportBreakdowns) []reportBreakdownRow {
	rows := []reportBreakdownRow{}
	add := func(group string, items []ExperimentReportBreakdown) {
		for _, item := range items {
			rows = append(rows, reportBreakdownRow{group: group, breakdown: item})
		}
	}
	add("task", b.ByTask)
	add("category", b.ByCategory)
	add("evaluator", b.ByEvaluator)
	add("score_key", b.ByScoreKey)
	add("evaluator_score_key", b.ByEvaluatorScoreKey)
	return rows
}

func (c *ReportTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// CheckTextCodec renders a check result as a compact summary followed by one
// line per check, so a human reading CI logs sees the verdict first and the
// evidence right below it.
type CheckTextCodec struct{}

func (c *CheckTextCodec) Format() format.Format {
	return "text"
}

func (c *CheckTextCodec) Encode(w io.Writer, v any) error {
	var r *CheckResult
	switch val := v.(type) {
	case *CheckResult:
		r = val
	case CheckResult:
		r = &val
	default:
		return errors.New("invalid data type for check text codec: expected *CheckResult")
	}
	if r == nil {
		return errors.New("invalid data type for check text codec: expected *CheckResult")
	}

	const labelFmt = "%-15s %s\n"
	if r.ExperimentID != "" {
		fmt.Fprintf(w, labelFmt, "Experiment:", r.ExperimentID)
	}
	if r.ExperimentStatus != "" {
		fmt.Fprintf(w, labelFmt, "Status:", r.ExperimentStatus)
	}
	fmt.Fprintf(w, labelFmt, "Quality check:", string(r.Verdict))
	if r.TestCaseCount > 0 {
		fmt.Fprintf(w, labelFmt, "Test cases:", strconv.Itoa(r.TestCaseCount))
	}

	if len(r.Checks) == 0 {
		return nil
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Checks:")
	for _, check := range r.Checks {
		fmt.Fprintf(w, "  %s\n", checkDetail(check))
	}
	return nil
}

func (c *CheckTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}
