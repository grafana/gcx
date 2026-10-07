// Package suggestions exposes the Synthetic Monitoring Reliability Inbox: check
// suggestions generated from a stack's telemetry.
//
// The Synthetic Monitoring datasource's backend proxies two resource endpoints to
// a co-located reliability-inbox service (see pkg/plugin/datasource.go in
// synthetic-monitoring-app). This package is the CLI side of that proxy: it calls
// them through the datasource's resources route with the caller's own Grafana
// credential, so the datasource's stored access token never leaves the server.
package suggestions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	healthPath      = "reliability-inbox/health"
	suggestionsPath = "reliability-inbox/suggestions"

	// defaultLimit matches the service's own per-algorithm cap. The service has
	// no pagination and ignores its request body, returning up to ~30
	// suggestions at once, so trimming can only happen on this side.
	defaultLimit = 10
)

const experimentalPreamble = "This command is experimental. It may be removed, or its subcommands, " +
	"flags and responses may change without following the normal semantic versioning conventions."

var (
	// errUnavailable means the health probe answered 404. Usually that is "not
	// deployed for this region", but a plugin too old to proxy the inbox answers
	// the same, so the server's message is always kept alongside.
	errUnavailable = errors.New("the reliability inbox is unavailable")

	// errNotReady means the plugin answered 503, which it does when the
	// datasource has no access token. A gateway outage answers the same, so the
	// server's message is always kept alongside.
	errNotReady = errors.New("the reliability inbox is not ready")
)

// resourceCaller is the part of synth.BackendDatasourceClient this package needs.
type resourceCaller interface {
	CallResource(ctx context.Context, datasourceUID, method, path string, body []byte) (*synth.Response, error)
}

// Evidence is the telemetry behind a suggestion. Numeric fields are pointers
// because the service omits zero values: absence is not the same as zero, and
// printing a missing error ratio as 0 would claim a measurement nobody made.
type Evidence struct {
	ReqPerS            *float64           `json:"reqPerS,omitempty"`
	ErrorRatio         *float64           `json:"errorRatio,omitempty"`
	P99Ms              *float64           `json:"p99Ms,omitempty"`
	StatusDistribution map[string]float64 `json:"statusDistribution,omitempty"`
	Families           []string           `json:"families,omitempty"`
	Provenance         json.RawMessage    `json:"provenance,omitempty"`
}

// Suggestion mirrors reliabilitySuggestionSchema in the app
// (src/features/reliabilityInbox/types.ts). checkType is a plain string: the
// service also emits grpc, tcp and multihttp, and a closed set here would make
// one unfamiliar suggestion discard the whole response.
type Suggestion struct {
	ID                 string            `json:"id"`
	Target             string            `json:"target"`
	CheckType          string            `json:"checkType"`
	Namespace          string            `json:"namespace,omitempty"`
	OwnerLabels        map[string]string `json:"ownerLabels,omitempty"`
	Evidence           Evidence          `json:"evidence"`
	Reachability       string            `json:"reachability"`
	ReachabilitySource string            `json:"reachabilitySource"`
	Confidence         string            `json:"confidence"`
	Score              float64           `json:"score"`
	DedupStatus        string            `json:"dedupStatus"`
	AuthRequired       bool              `json:"authRequired"`
	NeedsConfiguration *bool             `json:"needsConfiguration,omitempty"`
	Relevance          *float64          `json:"relevance,omitempty"`
	Rationale          string            `json:"rationale,omitempty"`
	Prompt             string            `json:"prompt"`
}

// result is the service's response body.
type result struct {
	Suggestions []Suggestion `json:"suggestions"`
	Warnings    []string     `json:"warnings"`
}

func (r *result) decode(body []byte) error {
	if err := json.Unmarshal(body, r); err != nil {
		return fmt.Errorf("failed to parse reliability inbox response: %w", err)
	}

	return nil
}

// fetch asks the Reliability Inbox for suggestions.
//
// It probes health first, as the app does, and only generates once the service
// is confirmed present: generation is a paid LLM call, so it is never made
// speculatively and never retried.
func fetch(ctx context.Context, c resourceCaller, datasourceUID string) (*result, error) {
	health, err := c.CallResource(ctx, datasourceUID, http.MethodGet, healthPath, nil)
	if err != nil {
		return nil, err
	}

	switch health.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, sentinelError(errUnavailable, health)
	default:
		// Not a confirmed negative: a timeout or 5xx says nothing about whether
		// the service exists, so it is not reported as "not available".
		return nil, statusError("reliability inbox health check", health)
	}

	resp, err := c.CallResource(ctx, datasourceUID, http.MethodPost, suggestionsPath, []byte(`{}`))
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusServiceUnavailable:
		return nil, sentinelError(errNotReady, resp)
	default:
		// A 404 here is deliberately not errUnavailable: health just confirmed
		// the service exists, so it is not the confirmed negative that error means.
		return nil, statusError("reliability inbox", resp)
	}

	var res result
	if err := res.decode(resp.Body); err != nil {
		return nil, err
	}

	// The service reports degraded failures as HTTP 200 with warnings and no
	// suggestions. That is a failure, not an empty inbox.
	if len(res.Suggestions) == 0 && len(res.Warnings) > 0 {
		return nil, fmt.Errorf("reliability inbox returned no suggestions: %s", strings.Join(res.Warnings, "; "))
	}

	return &res, nil
}

// maxRawMessage bounds how much of a non-JSON error body (an HTML gateway page,
// say) is echoed into an error.
const maxRawMessage = 200

// responseMessage extracts a human message from an error response. The plugin
// answers {"message": "..."} and the service behind it {"error": "..."}; both
// pass through the plugin untouched, so both are read. Anything else is echoed
// raw, truncated.
func responseMessage(resp *synth.Response) string {
	var body struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}

	if json.Unmarshal(resp.Body, &body) == nil {
		if body.Message != "" {
			return body.Message
		}
		if body.Error != "" {
			return body.Error
		}
	}

	msg := strings.TrimSpace(string(resp.Body))
	if len(msg) > maxRawMessage {
		msg = msg[:maxRawMessage] + "..."
	}

	return msg
}

// statusError describes a non-success response.
func statusError(what string, resp *synth.Response) error {
	if msg := responseMessage(resp); msg != "" {
		return fmt.Errorf("%s: HTTP %d: %s", what, resp.StatusCode, msg)
	}

	return fmt.Errorf("%s: HTTP %d", what, resp.StatusCode)
}

// sentinelError wraps a sentinel with the response's status and message, so
// callers can match it with errors.Is while the user still sees what the server
// actually said.
func sentinelError(sentinel error, resp *synth.Response) error {
	if msg := responseMessage(resp); msg != "" {
		return fmt.Errorf("%w (HTTP %d): %s", sentinel, resp.StatusCode, msg)
	}

	return fmt.Errorf("%w (HTTP %d)", sentinel, resp.StatusCode)
}

// Commands returns the `suggestions` command group.
func Commands(loader smcfg.Loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggestions",
		Short: "[experimental] Discover Synthetic Monitoring check suggestions.",
		Long: experimentalPreamble + `

Check suggestions come from the Synthetic Monitoring Reliability Inbox, an
experimental service that analyses a stack's telemetry.`,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
	}
	cmd.AddCommand(newListCommand(loader))

	return cmd
}

type listOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &tableCodec{rows: suggestionTable().Codec("table")})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	// The service has no pagination, so the whole generated set is always in
	// hand and the limit is a display trim with an exact observed total.
	o.IO.BindListLimit(flags, &o.Limit, "suggestions", defaultLimit)
}

// Validate also rejects a negative --limit (via Options.Validate), which must
// happen before any call: a bad flag must not cost a paid generation.
func (o *listOpts) Validate() error {
	return o.IO.Validate()
}

// listResult is the single shape every format encodes (JSON, YAML and the
// table codec, which extracts Suggestions to render rows).
type listResult struct {
	Suggestions []Suggestion `json:"suggestions" yaml:"suggestions"`
	// ListMeta is attached only when the output is a truncated page, so a
	// reader cannot mistake a page for the complete set. Reserved key; see
	// docs/design/output.md, List Truncation Contract.
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

// tableCodec renders the envelope's suggestions as rows.
type tableCodec struct {
	rows format.Codec
}

func (c *tableCodec) Format() format.Format { return "table" }

func (c *tableCodec) Encode(w io.Writer, v any) error {
	res, ok := v.(*listResult)
	if !ok {
		return fmt.Errorf("invalid data type for table codec: expected *listResult, got %T", v)
	}

	return c.rows.Encode(w, res.Suggestions)
}

func (c *tableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}

func newListCommand(loader smcfg.Loader) *cobra.Command {
	opts := &listOpts{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "[experimental] List suggested checks generated from this stack's telemetry.",
		Long: experimentalPreamble + `

List the checks the Synthetic Monitoring Reliability Inbox suggests for this stack.

Generating suggestions sends a summary of the stack's telemetry to an LLM and
is a paid, experimental service. Unlike the Synthetic Monitoring app, gcx cannot
check that Grafana Assistant is available or that its AI terms were accepted;
that is your responsibility before running this. Each run generates anew.

The service is deployed per region and is not available everywhere.

The service has no pagination and returns everything it generated (up to about
30 suggestions) in one response, so --limit only trims what is printed: it does
not reduce cost. A truncated page carries list_meta and a stderr hint. Suggestions
are kept in the service's order, highest confidence first, which is not sorted by
score. Use --json to print only some fields.`,
		Example: `  gcx synthetic-monitoring suggestions list
  gcx synthetic-monitoring suggestions list --limit 0 -o json
  gcx synthetic-monitoring suggestions list --json id,target,confidence,score`,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, datasourceUID, _, err := loader.LoadSMProxyConfig(ctx)
			if err != nil {
				return err
			}
			if datasourceUID == "" {
				return errors.New("no synthetic monitoring datasource found in this context; " +
					"suggestions are served by the datasource, so one must be installed")
			}

			client, err := synth.NewBackendDatasourceClient(restCfg)
			if err != nil {
				return err
			}

			res, err := fetch(ctx, client, datasourceUID)
			if err != nil {
				return err
			}

			// Warnings accompany real suggestions here (none-with-warnings already
			// failed). They go to stderr so stdout stays parseable.
			for _, w := range res.Warnings {
				cmdio.EmitWarn(cmd.ErrOrStderr(), w)
			}

			suggestions := res.Suggestions
			if suggestions == nil {
				suggestions = []Suggestion{}
			}

			// The whole generated set is in hand, so the limit is a display trim
			// and the observed total is exact. Truncation is never silent: the
			// paid generation already ran, so list_meta (machine) and a stderr
			// hint (human) say what the limit hid.
			suggestions, meta := cmdio.TruncateCompleteList(suggestions, opts.Limit)
			meta = cmdio.AttachListMeta(meta, os.Args)

			if err := opts.IO.Encode(cmd.OutOrStdout(), &listResult{Suggestions: suggestions, ListMeta: meta}); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)

			return nil
		},
	}
	opts.setup(cmd.Flags())

	return cmd
}

func suggestionTable() cmdio.Table[Suggestion] {
	return cmdio.Table[Suggestion]{
		Columns: []cmdio.Column[Suggestion]{
			{Header: "ID", Content: func(s Suggestion) string { return s.ID }},
			{Header: "TARGET", Content: func(s Suggestion) string { return s.Target }},
			{Header: "TYPE", Content: func(s Suggestion) string { return s.CheckType }},
			{Header: "CONFIDENCE", Content: func(s Suggestion) string { return s.Confidence }},
			{Header: "SCORE", Content: func(s Suggestion) string { return strconv.FormatFloat(s.Score, 'f', 2, 64) }},
			{Header: "REACHABILITY", Content: func(s Suggestion) string { return s.Reachability }},
			{Header: "NAMESPACE", Content: func(s Suggestion) string { return s.Namespace }},
		},
	}
}
