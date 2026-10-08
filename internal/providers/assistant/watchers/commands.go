// Package watchers exposes read-only Assistant Watcher commands.
package watchers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watcher"
	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const experimental = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

func markExperimental(cmd *cobra.Command) {
	cmd.Short = "[experimental] " + cmd.Short
	cmd.Long = experimental + "\n\n" + cmd.Long
	cmd.Annotations = map[string]string{agent.AnnotationStability: agent.StabilityExperimental}
}

// Commands returns the experimental read subtree.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "watchers", Short: "Inspect Assistant Watchers.", Long: "Read recurring telemetry-monitoring definitions and their current runtime observations. Use get for configuration and status for calibration, checks and assessments. These commands do not start monitoring or change Watchers.", Example: "  gcx assistant watchers list\n  gcx assistant watchers get checkout-health -o yaml\n  gcx assistant watchers status checkout-health -o json"}
	markExperimental(cmd)
	cmd.AddCommand(newListCommand(loader), newReadCommand(loader, false), newReadCommand(loader, true))
	return cmd
}

type listOpts struct {
	IO       cmdio.Options
	Archived bool
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags)
	flags.BoolVar(&o.Archived, "archived", false, "List archived Watchers only; default lists non-archived Watchers visible to the caller")
}
func (o *listOpts) Validate() error { return o.IO.Validate() }

type readOpts struct {
	IO  cmdio.Options
	Ref string
}

func (o *readOpts) setup(flags *pflag.FlagSet) { bindIO(&o.IO, flags) }
func (o *readOpts) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return errors.New("WATCHER must be a nonempty resource name or server ID; try gcx assistant watchers get checkout-health")
	}
	return o.IO.Validate()
}

func bindIO(opts *cmdio.Options, flags *pflag.FlagSet) {
	for _, f := range []format.Format{"text", "table", "wide"} {
		opts.RegisterCustomCodec(string(f), &tableCodec{formatName: f})
	}
	opts.DefaultFormat("text")
	opts.BindFlags(flags)
}

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*clientwatchers.Client, string, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, "", err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, "", err
	}
	return clientwatchers.NewClient(base), cfg.Namespace, nil
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{Use: "list", Short: "List Assistant Watcher definitions.", Long: "Fetch every page of Watcher definitions visible to your configured identity. By default, lists non-archived Watchers; --archived selects archived Watchers only. Coverage is permission-scoped and is not an atomic snapshot. Use get WATCHER for one definition and status WATCHER for runtime observations.", Args: cobra.NoArgs, Example: "  gcx assistant watchers list\n  gcx assistant watchers list --archived\n  gcx assistant watchers list -o json\n  gcx assistant watchers list -o wide", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		client, namespace, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		return runList(cmd, client, namespace, opts)
	}}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func newReadCommand(loader *providers.ConfigLoader, status bool) *cobra.Command {
	opts := &readOpts{}
	verb, short, long := "get", "Get an Assistant Watcher definition.", "Read a configuration-only manifest by resource name or server ID. Names are derived from titles; an ambiguous name reports candidate IDs. JSON and YAML match gcx resources get watchers/WATCHER. Secret values are never exported; configured secrets use preserve markers. Use status for runtime observations."
	if status {
		verb, short, long = "status", "Inspect an Assistant Watcher's runtime status.", "Read lifecycle, the last assessment, available run timestamps, estimated usage, audit and current version, calibration progress and calibrated checks. Lifecycle and health are separate observations; missing observations remain absent or unknown. Unsupported checks remain visible. This command does not request calibration or execute a run. Continue failed or incomplete calibration in Grafana."
	}
	cmd := &cobra.Command{Use: verb + " WATCHER", Short: short, Long: long, Args: cobra.ExactArgs(1), Example: "  gcx assistant watchers " + verb + " checkout-health\n  gcx assistant watchers " + verb + " checkout-health -o yaml\n  gcx assistant watchers " + verb + " example-id -o json", RunE: func(cmd *cobra.Command, args []string) error {
		opts.Ref = args[0]
		if err := opts.Validate(); err != nil {
			return err
		}
		client, namespace, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		if status {
			return runStatus(cmd, client, opts)
		}
		return runGet(cmd, client, namespace, opts)
	}}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

type coverage struct {
	Scope    string `json:"scope"`
	Archived bool   `json:"archived"`
	Complete bool   `json:"complete"`
	Snapshot bool   `json:"snapshot"`
}
type listResult struct {
	Items    []map[string]any `json:"items"`
	Coverage coverage         `json:"coverage"`
}

func runList(cmd *cobra.Command, client *clientwatchers.Client, namespace string, opts *listOpts) error {
	crud := watcher.NewTypedCRUDForClientArchived(client, namespace, opts.Archived)
	items, err := crud.List(cmd.Context(), 0)
	if err != nil {
		return err
	}
	result := listResult{Items: make([]map[string]any, 0, len(items)), Coverage: coverage{Scope: "caller-visible", Archived: opts.Archived, Complete: true, Snapshot: false}}
	for _, item := range items {
		u, err := crud.ToUnstructured(item.Spec)
		if err != nil {
			return err
		}
		result.Items = append(result.Items, u.Object)
	}
	return opts.IO.Encode(cmd.OutOrStdout(), result)
}

func runGet(cmd *cobra.Command, client *clientwatchers.Client, namespace string, opts *readOpts) error {
	crud := watcher.NewTypedCRUDForClient(client, namespace)
	obj, err := crud.Get(cmd.Context(), opts.Ref)
	if err != nil {
		return err
	}
	u, err := crud.ToUnstructured(obj.Spec)
	if err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(), u.Object)
}

type assessment struct {
	Severity string `json:"severity"`
}
type runs struct {
	LastObservedAt  *time.Time `json:"lastObservedAt,omitempty"`
	NextScheduledAt *time.Time `json:"nextScheduledAt,omitempty"`
}
type usage struct {
	EstimatedTokensPerRun  int64 `json:"estimatedTokensPerRun"`
	EstimatedTokensPerHour int64 `json:"estimatedTokensPerHour"`
	SampleSize             int   `json:"sampleSize"`
}
type audit struct {
	CreatedBy string     `json:"createdBy,omitempty"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}
type version struct {
	ID string `json:"id"`
}
type calibration struct {
	State        string                 `json:"state"`
	Availability string                 `json:"availability,omitempty"`
	Message      string                 `json:"message,omitempty"`
	Context      string                 `json:"context,omitempty"`
	StartedAt    *time.Time             `json:"startedAt,omitempty"`
	Checks       []clientwatchers.Check `json:"checks"`
}
type statusResult struct {
	Name        string      `json:"name"`
	ID          string      `json:"id"`
	Lifecycle   string      `json:"lifecycle"`
	ArchivedAt  *time.Time  `json:"archivedAt,omitempty"`
	Assessment  *assessment `json:"assessment,omitempty"`
	Calibration calibration `json:"calibration"`
	Runs        runs        `json:"runs"`
	Usage       *usage      `json:"usage,omitempty"`
	Audit       audit       `json:"audit"`
	Version     *version    `json:"version,omitempty"`
}

func runStatus(cmd *cobra.Command, client *clientwatchers.Client, opts *readOpts) error {
	item, err := watcher.Resolve(cmd.Context(), client, opts.Ref)
	if err != nil {
		return err
	}
	result := statusResult{Name: watcher.Watcher{Title: item.Name}.GetResourceName(), ID: item.ID, Lifecycle: item.Status, ArchivedAt: item.ArchivedAt, Runs: runs{LastObservedAt: item.LastRunAt, NextScheduledAt: item.NextRunAt}, Calibration: calibration{State: "unknown", Context: item.CalibrationContext, Checks: clientwatchers.ProjectChecks(item.Queries)}, Audit: audit{CreatedBy: item.CreatedBy}}
	if result.Lifecycle == "" {
		result.Lifecycle = "unknown"
	}
	if !item.CreatedAt.IsZero() {
		result.Audit.CreatedAt = &item.CreatedAt
	}
	if !item.UpdatedAt.IsZero() {
		result.Audit.UpdatedAt = &item.UpdatedAt
	}
	if item.LastRunAssessment != "" {
		result.Assessment = &assessment{Severity: item.LastRunAssessment}
	}
	if item.TokenConsumption != nil {
		result.Usage = &usage{EstimatedTokensPerRun: item.TokenConsumption.AveragePerRun, EstimatedTokensPerHour: item.TokenConsumption.EstimatedPerHour, SampleSize: item.TokenConsumption.SampleSize}
	}
	if item.DefinitionVersion > 0 {
		result.Version = &version{ID: strconv.FormatInt(item.DefinitionVersion, 10)}
	}
	progress, err := client.Calibration(cmd.Context(), item.ID)
	if err != nil {
		if cmd.Context().Err() != nil {
			return cmd.Context().Err()
		}
		result.Calibration.Availability = "failed"
		switch {
		case errors.Is(err, clientwatchers.ErrCapabilityUnavailable), errors.Is(err, clientwatchers.ErrNotFound):
			result.Calibration.Availability = "unavailable"
		case errors.Is(err, clientwatchers.ErrPermissionDenied):
			result.Calibration.Availability = "access-denied"
		}
		result.Calibration.Message = err.Error()
	} else {
		result.Calibration.State = progress.Status
		result.Calibration.Message = progress.Message
		result.Calibration.StartedAt = progress.StartedAt
		if progress.Status == "failed" || progress.Status == "needs_input" {
			result.Calibration.Message = "Continue calibration in Grafana. " + progress.Message
		}
	}
	return opts.IO.Encode(cmd.OutOrStdout(), result)
}

type tableCodec struct{ formatName format.Format }

func (c *tableCodec) Format() format.Format { return c.formatName }
func (c *tableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}
func (c *tableCodec) Encode(dst io.Writer, value any) error {
	if result, ok := value.(statusResult); ok {
		table := style.NewTable("FIELD", "VALUE")
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		for _, key := range []string{"name", "id", "lifecycle", "archivedAt", "assessment", "calibration", "runs", "usage", "audit", "version"} {
			if v, ok := fields[key]; ok {
				table.Row(key, string(v))
			}
		}
		return table.Render(dst)
	}
	var items []map[string]any
	switch v := value.(type) {
	case listResult:
		items = v.Items
	case map[string]any:
		items = []map[string]any{v}
	default:
		return fmt.Errorf("unsupported Watcher table value %T", value)
	}
	headers := []string{"NAME", "ID", "TITLE"}
	if c.formatName == "wide" {
		headers = append(headers, "INTERVAL", "SENSITIVITY")
	}
	table := style.NewTable(headers...)
	for _, item := range items {
		meta, _ := item["metadata"].(map[string]any)
		spec, _ := item["spec"].(map[string]any)
		annotations, _ := meta["annotations"].(map[string]any)
		row := []string{fmt.Sprint(meta["name"]), fmt.Sprint(annotations[watcher.WatcherIDAnnotation]), fmt.Sprint(spec["title"])}
		if c.formatName == "wide" {
			row = append(row, fmt.Sprint(spec["interval"]), fmt.Sprint(spec["sensitivity"]))
		}
		table.Row(row...)
	}
	return table.Render(dst)
}
