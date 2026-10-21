package watchers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/watcher"
	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newLifecycleCommand(loader *providers.ConfigLoader, verb string) *cobra.Command {
	opts := &readOpts{}
	var description string
	switch verb {
	case "start":
		description = "Activate a calibrated Watcher with at least one enabled check. The server decides readiness and reports refusal reasons. Starting can immediately enqueue a scan and deliver configured notifications or investigations; event-only Watchers wait for their trigger. A failed start request can still leave monitoring active: inspect the returned lifecycle and status before retrying."
	case "pause":
		description = "Stop future Watcher scans without changing configuration or deleting history. An in-flight scan may finish and deliver configured notifications or investigations. Pausing does not cancel calibration; finish or cancel calibration in Grafana first."
	case "run":
		description = "Request one immediate scan without changing scheduled activation. Reports request acceptance or skipped work, never a completed assessment. The returned task ID is an enqueue identifier, not a run-history ID; exact correlation is unavailable, and the newest history entry must not be assumed to belong to this request. The server enforces lifecycle and readiness requirements. Requires run/create permission as well as any server-required write permission; editing permission alone may be insufficient. Configured notifications and investigations can run."
	case "archive":
		description = "Archive an explicitly paused Watcher while retaining configuration and history. Pause it first; gcx never calls pause implicitly. If it is calibrating, finish or cancel calibration in Grafana before pausing. The paused-state precheck and archive request are not atomic: another client's concurrent start can be stopped by archive. Archive may resolve active Grafana Alerting findings. An in-flight scan may still finish; notification cancellation is not guaranteed."
	case "unarchive":
		description = "Recover an archived Watcher without activating monitoring. This is archive recovery, not definition-version restore. The Watcher must be archived; recovery leaves it paused, clears automatic-stop archive intent and retains the stop deadline, which can still prevent a later start. Another client's concurrent start can supersede recovery; gcx reports the observed lifecycle and never pauses as a repair."
	}
	short := strings.ToUpper(verb[:1]) + verb[1:] + " an Assistant Watcher."
	if verb == "run" {
		short = "Request one immediate Assistant Watcher scan."
	}
	cmd := &cobra.Command{Use: verb + " WATCHER", Short: short, Long: description, Args: cobra.ExactArgs(1), Example: "  gcx assistant watchers " + verb + " checkout-health -o json", RunE: func(cmd *cobra.Command, args []string) error {
		opts.Ref = args[0]
		if err := opts.Validate(); err != nil {
			return err
		}
		client, _, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		return runLifecycle(cmd, client, opts, verb)
	}}
	markExperimental(cmd)
	cmd.Annotations[agent.AnnotationTokenCost] = "small"
	opts.setup(cmd.Flags())
	return cmd
}

type lifecycleResult struct {
	cmdio.SingleMutation

	Outcome     string `json:"outcome"`
	Lifecycle   string `json:"lifecycle,omitempty"`
	Archived    *bool  `json:"archived,omitempty"`
	Observation string `json:"observation,omitempty"`
	TaskID      string `json:"taskId,omitempty"`
	Correlation string `json:"correlation,omitempty"`
	Guidance    string `json:"guidance,omitempty"`
}

func runLifecycle(cmd *cobra.Command, client *clientwatchers.Client, opts *readOpts, verb string) error {
	item, err := watcher.Resolve(cmd.Context(), client, opts.Ref)
	if err != nil {
		return err
	}
	if verb == "archive" && (item.Status != "paused" || item.ArchivedAt != nil) {
		return errors.New("archive requires a non-archived, explicitly paused Watcher; use gcx assistant watchers pause WATCHER first")
	}
	if verb == "unarchive" && item.ArchivedAt == nil {
		return errors.New("unarchive requires an archived Watcher; inspect gcx assistant watchers status WATCHER")
	}
	action := verb + "d"
	switch verb {
	case "start":
		action = "started"
	case "run":
		action = "run-requested"
	}
	result := lifecycleResult{SingleMutation: cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: watcher.WatcherKind, Name: watcher.Watcher{Title: item.Name}.GetResourceName(), ID: item.ID}), Outcome: "accepted"}
	var observed *clientwatchers.Watcher
	switch verb {
	case "start":
		observed, err = client.Start(cmd.Context(), item.ID)
	case "pause":
		observed, err = client.Pause(cmd.Context(), item.ID)
	case "archive":
		observed, err = client.Archive(cmd.Context(), item.ID)
	case "unarchive":
		observed, err = client.Unarchive(cmd.Context(), item.ID)
	case "run":
		var request *clientwatchers.RunRequest
		request, err = client.RequestRun(cmd.Context(), item.ID)
		result.Correlation = "unavailable"
		result.Guidance = "Request acceptance is not scan completion. Inspect list-runs; task IDs cannot be correlated with history IDs."
		if err == nil {
			result.TaskID = request.TaskID
			if request.Skipped {
				result.Outcome = "skipped"
			}
		}
	}
	if observed != nil && observed.ID == item.ID {
		setLifecycleObservation(&result, observed, "response")
	}
	if err != nil {
		result.Outcome = "unknown"
		var apiErr *clientwatchers.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			result.Outcome = "rejected"
		}
		result.Error = err.Error()
		result.Guidance = "Inspect gcx assistant watchers status " + item.ID + " before retrying; a failed request may already have changed monitoring. No automatic retry or rollback was attempted."
	}
	if err != nil && verb != "run" {
		if result.Observation == "" {
			result.Observation = "unavailable"
		}
		readback, readErr := client.Get(cmd.Context(), item.ID)
		if readErr == nil {
			setLifecycleObservation(&result, readback, "readback")
		}
	}
	if encodeErr := opts.IO.Encode(cmd.OutOrStdout(), result); encodeErr != nil {
		return encodeErr
	}
	if err != nil {
		code := gcxerrors.ExitGeneralError
		if errors.Is(err, context.Canceled) {
			code = gcxerrors.ExitCancelled
		} else if errors.Is(err, clientwatchers.ErrPermissionDenied) {
			code = gcxerrors.ExitAuthFailure
		}
		return gcxerrors.NewEmittedError(code, err)
	}
	return nil
}

func setLifecycleObservation(result *lifecycleResult, item *clientwatchers.Watcher, source string) {
	result.Lifecycle = item.Status
	archived := item.ArchivedAt != nil
	result.Archived = &archived
	result.Observation = source
}

type historyOpts struct {
	readOpts

	Limit int
}

func (o *historyOpts) setup(flags *pflag.FlagSet) {
	o.readOpts.setup(flags)
	flags.IntVar(&o.Limit, "limit", 20, "Maximum newest run summaries to return (1-100); coverage reports whether more retained history exists")
}
func (o *historyOpts) Validate() error {
	if o.Limit < 1 || o.Limit > 100 {
		code := gcxerrors.ExitUsageError
		return &gcxerrors.DetailedError{Summary: "invalid run history limit", Details: "--limit must be between 1 and 100", ExitCode: &code}
	}
	return o.readOpts.Validate()
}

func newHistoryCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &historyOpts{}
	cmd := &cobra.Command{Use: "list-runs WATCHER", Short: "List bounded Assistant Watcher run summaries.", Long: "Read the newest retained run summaries visible to your configured identity, including execution status, assessment, timestamps and definition version. --limit defaults to 20 and accepts 1-100; gcx fetches an extra entry to report whether more history exists. Coverage is not an atomic snapshot and does not claim lifetime retention. Running, failed and unknown execution states never imply a healthy assessment. Task IDs from run requests cannot be correlated with these history IDs. Use Grafana for full run evidence.", Args: cobra.ExactArgs(1), Example: "  gcx assistant watchers list-runs checkout-health --limit 20 -o json", RunE: func(cmd *cobra.Command, args []string) error {
		opts.Ref = args[0]
		if err := opts.Validate(); err != nil {
			return err
		}
		client, _, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		return runHistory(cmd, client, opts)
	}}
	markExperimental(cmd)
	cmd.Annotations[agent.AnnotationTokenCost] = "medium"
	cmd.Annotations[agent.AnnotationLLMHint] = "Bounded run summaries; use --limit and --json status,assessment,startedAt to reduce output. Coverage is retained under field selection."
	opts.setup(cmd.Flags())
	return cmd
}

type historyCoverage struct {
	Scope    string `json:"scope"`
	Limit    int    `json:"limit"`
	Returned int    `json:"returned"`
	HasMore  bool   `json:"hasMore"`
	Snapshot bool   `json:"snapshot"`
}
type historyResult struct {
	Name        string               `json:"name"`
	ID          string               `json:"id"`
	Items       []clientwatchers.Run `json:"items"`
	Coverage    historyCoverage      `json:"coverage"`
	Correlation string               `json:"correlation"`
}

func (historyResult) ListItemsKey() string { return "items" }

func runHistory(cmd *cobra.Command, client *clientwatchers.Client, opts *historyOpts) error {
	item, err := watcher.Resolve(cmd.Context(), client, opts.Ref)
	if err != nil {
		return err
	}
	history, err := client.ListRuns(cmd.Context(), item.ID, opts.Limit)
	if err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(), historyResult{Name: watcher.Watcher{Title: item.Name}.GetResourceName(), ID: item.ID, Items: history.Items, Coverage: historyCoverage{Scope: "caller-visible-retained", Limit: opts.Limit, Returned: len(history.Items), HasMore: history.HasMore, Snapshot: false}, Correlation: "unavailable"})
}

func encodeLifecycle(dst io.Writer, result lifecycleResult) error {
	table := style.NewTable("OPERATION", "NAME", "ID", "REQUEST", "LIFECYCLE", "ARCHIVED", "OBSERVATION")
	archived := "unknown"
	if result.Archived != nil {
		archived = strconv.FormatBool(*result.Archived)
	}
	table.Row(result.Action, result.Target.Name, result.Target.ID, result.Outcome, result.Lifecycle, archived, result.Observation)
	if err := table.Render(dst); err != nil {
		return err
	}
	if result.TaskID != "" {
		if _, err := fmt.Fprintln(dst, "Enqueue task ID:", result.TaskID); err != nil {
			return err
		}
	}
	for _, message := range []string{result.Error, result.Guidance} {
		if message != "" {
			if _, err := fmt.Fprintln(dst, message); err != nil {
				return err
			}
		}
	}
	return nil
}

func encodeHistory(dst io.Writer, result historyResult) error {
	if _, err := fmt.Fprintf(dst, "Watcher: %s (ID %s)\n", result.Name, result.ID); err != nil {
		return err
	}
	table := style.NewTable("ID", "STATUS", "ASSESSMENT", "STARTED", "FINISHED", "VERSION")
	for _, item := range result.Items {
		finished := "-"
		if item.FinishedAt != nil {
			finished = item.FinishedAt.Format(time.RFC3339)
		}
		severity := item.Assessment
		if severity == "" {
			severity = "-"
		}
		table.Row(item.ID, item.Status, severity, item.StartedAt.Format(time.RFC3339), finished, strconv.FormatInt(item.DefinitionVersion, 10))
	}
	if err := table.Render(dst); err != nil {
		return err
	}
	_, err := fmt.Fprintf(dst, "Coverage: %d retained run summaries; more available: %t; not an atomic snapshot. Request/history correlation unavailable.\n", result.Coverage.Returned, result.Coverage.HasMore)
	return err
}
