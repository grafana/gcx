package k6

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

const (
	defaultRunTraceLimit = 500
	defaultRunWait       = 15 * time.Minute
	runWaitPollInterval  = 5 * time.Second
	maxRunArtifactBytes  = 100 << 20
)

const defaultRunTraceQuery = `{ name = "iteration" && span.test.iteration.number >= 0 && span.test.vu >= 0 && span.test.scenario != "" }`

const experimentalRunDiagnosticsPreamble = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

func markExperimentalRunDiagnostics(cmd *cobra.Command) *cobra.Command {
	cmd.Short = "[experimental] " + cmd.Short
	cmd.Long = experimentalRunDiagnosticsPreamble + "\n\n" + cmd.Long
	cmd.Annotations = map[string]string{agent.AnnotationStability: agent.StabilityExperimental}
	return cmd
}

type runTracesTableCodec struct{}

func (runTracesTableCodec) Format() format.Format { return "table" }
func (runTracesTableCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(*RunTracesResponse)
	if !ok {
		return fmt.Errorf("run traces table: got %T", value)
	}
	if len(result.Traces) == 0 {
		_, err := fmt.Fprintln(w, "No traces")
		return err
	}
	table := style.NewTable("TRACE ID", "ROOT SERVICE", "ROOT TRACE", "DURATION (MS)", "START (NS)")
	for _, trace := range result.Traces {
		table.Row(trace.TraceID, trace.RootServiceName, trace.RootTraceName, fmt.Sprint(trace.DurationMS), trace.StartTimeUnixNano)
	}
	return table.Render(w)
}
func (runTracesTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type runArtifactsTableCodec struct{}

func (runArtifactsTableCodec) Format() format.Format { return "table" }
func (runArtifactsTableCodec) Encode(w io.Writer, value any) error {
	names, ok := value.([]string)
	if !ok {
		return fmt.Errorf("run artifacts table: got %T", value)
	}
	if len(names) == 0 {
		_, err := fmt.Fprintln(w, "No artifacts")
		return err
	}
	table := style.NewTable("PATH")
	for _, name := range names {
		table.Row(name)
	}
	return table.Render(w)
}
func (runArtifactsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

func newRunsTracesCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use: "traces", Short: "Inspect browser traces for k6 test runs.",
		Long: "Search and get browser traces that belong to k6 test runs.",
	}
	cmd.AddCommand(newRunsTracesListCommand(loader), newRunsTracesGetCommand(loader))
	return markExperimentalRunDiagnostics(cmd)
}

func newRunsTracesListCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO    cmdio.Options
		Query string
		Limit int
	}
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "list <run-id>",
		Short: "List browser traces for a k6 test run.",
		Long:  "List browser iteration traces for one k6 test run. Use --query to supply a narrower TraceQL expression.",
		Example: "  gcx k6 runs traces list 12345\n" +
			"  gcx k6 runs traces list 12345 --query '{ span.test.scenario = \"ui\" && span.test.vu = 1 }' --limit 50 -o json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			query := strings.TrimSpace(opts.Query)
			if query == "" {
				return errors.New("invalid --query: expected a non-empty TraceQL expression")
			}
			if opts.Limit <= 0 {
				return fmt.Errorf("invalid --limit %d: expected a positive integer", opts.Limit)
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			run, err := client.GetTestRun(cmd.Context(), runID)
			if err != nil {
				return err
			}
			start, end, err := defaultRunLogTimeRange(run, time.Now())
			if err != nil {
				return err
			}
			result, err := client.ListRunTraces(cmd.Context(), runID, RunTracesRequest{
				Query: query, Start: start.Add(-time.Minute).Unix(), End: end.Add(time.Minute).Unix(), Limit: opts.Limit,
			})
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			if len(result.Traces) >= opts.Limit {
				cmdio.EmitWarn(cmd.ErrOrStderr(), fmt.Sprintf("trace search returned the --limit value (%d); use a narrower --query or increase --limit", opts.Limit))
			}
			return nil
		},
	}
	setupDataOutput(&opts.IO, cmd.Flags(), runTracesTableCodec{})
	cmd.Flags().StringVar(&opts.Query, "query", defaultRunTraceQuery, "TraceQL expression for trace search")
	cmd.Flags().IntVar(&opts.Limit, "limit", defaultRunTraceLimit, "Maximum number of traces to return")
	return markExperimentalRunDiagnostics(cmd)
}

func validTraceID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func newRunsTracesGetCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{
		Use:   "get <run-id> <trace-id>",
		Short: "Get a browser trace for a k6 test run.",
		Long:  "Get one full OTLP browser trace. Use the trace ID from gcx k6 runs traces list.",
		Example: "  gcx k6 runs traces get 12345 0123456789abcdef0123456789abcdef\n" +
			"  gcx k6 runs traces get 12345 0123456789abcdef0123456789abcdef --json batches",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			if !validTraceID(args[1]) {
				return fmt.Errorf("invalid trace ID %q: expected 32 hexadecimal characters", args[1])
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			result, err := client.GetRunTrace(cmd.Context(), runID, strings.ToLower(args[1]))
			if err != nil {
				return err
			}
			return opts.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.BindFlags(cmd.Flags())
	return markExperimentalRunDiagnostics(cmd)
}

func newRunsArtifactsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use: "artifacts", Short: "List and download browser artifacts for k6 test runs.",
		Long: "List and download browser screenshots that belong to k6 test runs.",
	}
	cmd.AddCommand(newRunsArtifactsListCommand(loader), newRunsArtifactsDownloadCommand(loader))
	return markExperimentalRunDiagnostics(cmd)
}

func newRunsArtifactsListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{
		Use:   "list <run-id>",
		Short: "List browser artifacts for a k6 test run.",
		Long:  "List screenshot paths for one k6 browser test run.",
		Example: "  gcx k6 runs artifacts list 12345\n" +
			"  gcx k6 runs artifacts list 12345 -o json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			result, err := client.ListRunArtifacts(cmd.Context(), runID)
			if err != nil {
				return err
			}
			sort.Strings(result)
			return opts.Encode(cmd.OutOrStdout(), result)
		},
	}
	setupDataOutput(opts, cmd.Flags(), runArtifactsTableCodec{})
	return markExperimentalRunDiagnostics(cmd)
}

type runArtifactsDownloadOpts struct {
	OutputDir string
}

func (o *runArtifactsDownloadOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.OutputDir, "output-dir", "d", "", "Directory for downloaded artifacts (default k6-run-<run-id>-artifacts)")
}

func newRunsArtifactsDownloadCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &runArtifactsDownloadOpts{}
	cmd := &cobra.Command{
		Use:   "download <run-id> [artifact-path...]",
		Short: "Download browser artifacts for a k6 test run.",
		Long:  "Discover and download browser screenshots for one k6 test run. With no artifact paths, download all discovered artifacts. Existing files are not overwritten.",
		Example: "  gcx k6 runs artifacts download 12345\n" +
			"  gcx k6 runs artifacts download 12345 --output-dir ./screenshots\n" +
			"  gcx k6 runs artifacts download 12345 '12345/files/screenshots/screenshots/home.png'",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("output-dir") && strings.TrimSpace(opts.OutputDir) == "" {
				return errors.New("invalid --output-dir: expected a non-empty directory path")
			}
			outputDir := opts.OutputDir
			if outputDir == "" {
				outputDir = fmt.Sprintf("k6-run-%d-artifacts", runID)
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			available, err := client.ListRunArtifacts(cmd.Context(), runID)
			if err != nil {
				return err
			}
			names, err := selectRunArtifacts(available, args[1:])
			if err != nil {
				return err
			}
			downloads, err := client.SignRunArtifactDownloads(cmd.Context(), runID, names)
			if err != nil {
				return err
			}
			receipt, downloadErr := downloadRunArtifacts(cmd.Context(), runID, outputDir, names, downloads, httputils.NewDefaultClient(cmd.Context()))
			if downloadErr != nil && receipt.Summary.Succeeded == 0 {
				return downloadErr
			}
			if err := cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				cmdio.Success(w, "Downloaded %d k6 run artifacts to %s", receipt.Summary.Succeeded, receipt.Dir)
				return nil
			}); err != nil {
				return err
			}
			if downloadErr != nil {
				for _, failure := range receipt.Failures {
					cmdio.EmitWarn(cmd.ErrOrStderr(), failure.Error)
				}
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, downloadErr)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return markExperimentalRunDiagnostics(cmd)
}

func selectRunArtifacts(available, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return append([]string(nil), available...), nil
	}
	availableSet := make(map[string]struct{}, len(available))
	for _, name := range available {
		availableSet[name] = struct{}{}
	}
	selected := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		if _, ok := availableSet[name]; !ok {
			return nil, fmt.Errorf("artifact %q was not found for this run; run 'gcx k6 runs artifacts list <run-id>'", name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		selected = append(selected, name)
	}
	return selected, nil
}

func artifactOutputPath(runID int, outputDir, name string) (string, error) {
	prefix := strconv.Itoa(runID) + "/files/"
	if !strings.HasPrefix(name, prefix) {
		return "", fmt.Errorf("artifact path %q does not start with %q", name, prefix)
	}
	relative := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(name, prefix)))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path %q is unsafe", name)
	}
	return filepath.Join(outputDir, relative), nil
}

func downloadRunArtifacts(
	ctx context.Context,
	runID int,
	outputDir string,
	names []string,
	downloads []RunArtifactDownload,
	httpClient *http.Client,
) (cmdio.ArtifactReceipt, error) {
	receipt := cmdio.NewArtifactReceipt("downloaded", "png")
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		return receipt, fmt.Errorf("resolve artifact output directory %q: %w", outputDir, err)
	}
	receipt.Dir = absDir
	if len(names) == 0 {
		return receipt, nil
	}
	byName := make(map[string]string, len(downloads))
	for _, download := range downloads {
		byName[download.Name] = download.PreSignedURL
	}
	type result struct {
		name string
		file cmdio.ArtifactFile
		err  error
	}
	results := make([]result, len(names))
	group := new(errgroup.Group)
	group.SetLimit(10)
	for i, name := range names {
		group.Go(func() error {
			results[i].name = name
			path, pathErr := artifactOutputPath(runID, absDir, name)
			if pathErr != nil {
				results[i].err = pathErr
				return pathErr
			}
			rawURL, ok := byName[name]
			if !ok || rawURL == "" {
				results[i].err = fmt.Errorf("artifact download URL is missing for %q", name)
				return results[i].err
			}
			if downloadErr := downloadRunArtifact(ctx, httpClient, rawURL, path); downloadErr != nil {
				results[i].err = fmt.Errorf("download artifact %q: %w", name, downloadErr)
				return results[i].err
			}
			results[i].file = cmdio.ArtifactFile{Path: path, Kind: "screenshot"}
			return nil
		})
	}
	_ = group.Wait()
	var failures []error
	for _, result := range results {
		if result.err != nil {
			failures = append(failures, result.err)
			receipt.Failures = append(receipt.Failures, cmdio.MutationFailure{
				Target: cmdio.MutationTarget{Kind: "k6-run-artifact", Name: result.name}, Error: result.err.Error(),
			})
			continue
		}
		receipt.Files = append(receipt.Files, result.file)
	}
	receipt.Summary = cmdio.MutationSummary{Succeeded: len(receipt.Files), Failed: len(receipt.Failures)}
	return receipt, errors.Join(failures...)
}

func downloadRunArtifact(ctx context.Context, httpClient *http.Client, rawURL, outputPath string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("invalid signed download URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create output file %q: %w", outputPath, err)
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(outputPath)
		}
	}()
	written, err := io.Copy(file, io.LimitReader(response.Body, maxRunArtifactBytes+1))
	if err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	if written > maxRunArtifactBytes {
		return fmt.Errorf("artifact exceeds the %d-byte download limit", maxRunArtifactBytes)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close output file: %w", err)
	}
	keep = true
	return nil
}

type runsWaitOpts struct {
	IO           cmdio.Options
	Timeout      time.Duration
	pollInterval time.Duration
}

func (o *runsWaitOpts) setup(flags *pflag.FlagSet) {
	setupDataOutput(&o.IO, flags, testRunTableCodec{})
	flags.DurationVar(&o.Timeout, "timeout", defaultRunWait, "Maximum time to wait for a terminal run result")
}

func (o *runsWaitOpts) effectivePollInterval() time.Duration {
	if o.pollInterval > 0 {
		return o.pollInterval
	}
	return runWaitPollInterval
}

func newRunsWaitCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &runsWaitOpts{}
	cmd := &cobra.Command{
		Use:   "wait <run-id>",
		Short: "Wait for a k6 test run and metric processing to finish.",
		Long:  "Poll a k6 test run until it completes or is aborted. The command waits through the processing_metrics status and exits with an error when the final result did not pass.",
		Example: "  gcx k6 runs wait 12345\n" +
			"  gcx k6 runs wait 12345 --timeout 30m -o json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.Timeout <= 0 {
				return fmt.Errorf("invalid --timeout %q: expected a positive duration such as 15m", opts.Timeout)
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			run, err := waitForRun(cmd.Context(), client, runID, opts.Timeout, opts.effectivePollInterval())
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), run); err != nil {
				return err
			}
			if run.Status == "completed" && run.Result != nil && *run.Result == "passed" {
				return nil
			}
			result := "unavailable"
			if run.Result != nil {
				result = *run.Result
			}
			failure := fmt.Errorf("k6 test run %d reached status %q with result %q", run.ID, run.Status, result)
			cmdio.EmitWarn(cmd.ErrOrStderr(), failure.Error())
			return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, failure)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type testRunGetter interface {
	GetTestRun(ctx context.Context, runID int) (*TestRun, error)
}

func waitForRun(
	ctx context.Context, client testRunGetter, runID int, timeout, pollInterval time.Duration,
) (*TestRun, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		run, err := client.GetTestRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		if run.Status == "completed" || run.Status == "aborted" {
			return run, nil
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return nil, fmt.Errorf("timeout after %s waiting for k6 test run %d; last status was %q", timeout, runID, run.Status)
		case <-timer.C:
		}
	}
}
