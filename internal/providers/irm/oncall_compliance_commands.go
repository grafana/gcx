package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// Selectable values offered by the interactive `set` form.
var (
	complianceChannelOptions  = []string{"phone", "slack", "telegram", "msteams", "email", "mobile_app"}
	complianceRuleTypeOptions = []string{
		"notify_by_slack", "notify_by_sms", "notify_by_phone_call", "notify_by_telegram",
		"notify_by_mobile_app", "notify_by_mobile_app_critical", "notify_by_email",
	}

	// complianceLabels maps raw API enum values to friendly display names. The raw value
	// is always what gets sent to / received from the API; labels are display-only.
	complianceLabels = map[string]string{
		"phone":      "Phone",
		"slack":      "Slack",
		"telegram":   "Telegram",
		"msteams":    "Microsoft Teams",
		"email":      "Email",
		"mobile_app": "Mobile app",

		"notify_by_slack":               "Slack",
		"notify_by_sms":                 "SMS",
		"notify_by_phone_call":          "Phone call",
		"notify_by_telegram":            "Telegram",
		"notify_by_mobile_app":          "Mobile app",
		"notify_by_mobile_app_critical": "Mobile app (critical)",
		"notify_by_email":               "Email",
	}
)

// complianceLabel returns the friendly display name for a value, falling back to the
// raw value when no mapping exists.
func complianceLabel(v string) string {
	if l, ok := complianceLabels[v]; ok {
		return l
	}
	return v
}

// ---------------------------------------------------------------------------
// compliance-rules command group (org-level "expected configuration")
//
// The compliance API currently lives only on the OnCall *public* API
// (GET/POST /api/v1/oncall_compliance/, evaluate at .../evaluate/), authenticated
// with an Authorization token + X-Grafana-Instance-ID header — NOT the plugin-proxy
// surface the rest of the IRM client uses. Until it is exposed through the proxy,
// these commands support a TEST-ONLY direct transport via --oncall-url/--oncall-token/
// --instance-id (env: ONCALL_URL/ONCALL_TOKEN/INSTANCE_ID). With no --oncall-url they
// fall back to the plugin-proxy client (which 404s today).
// ---------------------------------------------------------------------------

// complianceAPI is the read/write surface the get/set commands need. Both the
// plugin-proxy OnCallClient (via OnCallAPI) and the test-only public client satisfy it.
type complianceAPI interface {
	GetComplianceRules(ctx context.Context) (*ComplianceRules, error)
	SetComplianceRules(ctx context.Context, rules ComplianceRules) (*ComplianceRules, error)
}

// complianceTransport holds the test-only direct-transport flags.
type complianceTransport struct {
	URL        string
	Token      string
	InstanceID string
}

func (t *complianceTransport) bind(flags *pflag.FlagSet) {
	flags.StringVar(&t.URL, "oncall-url", os.Getenv("ONCALL_URL"),
		"TEST-ONLY: OnCall engine base URL (e.g. http://host:8084); bypasses the plugin proxy. Defaults to $ONCALL_URL.")
	flags.StringVar(&t.Token, "oncall-token", os.Getenv("ONCALL_TOKEN"),
		"TEST-ONLY: OnCall API token for direct transport. Defaults to $ONCALL_TOKEN.")
	flags.StringVar(&t.InstanceID, "instance-id", os.Getenv("INSTANCE_ID"),
		"TEST-ONLY: X-Grafana-Instance-ID for direct transport. Defaults to $INSTANCE_ID.")
}

// resolve returns a complianceAPI: the direct public client when --oncall-url is set,
// otherwise the plugin-proxy client.
func (t *complianceTransport) resolve(ctx context.Context, loader OnCallConfigLoader) (complianceAPI, error) {
	if t.URL != "" {
		return newPublicComplianceClient(t.URL, t.Token, t.InstanceID), nil
	}
	client, _, err := loader.LoadOnCallClient(ctx)
	return client, err
}

// newComplianceRulesCmd builds the `compliance-rules` group: get + set + evaluate.
func newComplianceRulesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "compliance-rules",
		Short:   "Manage the org's notification compliance rules (expected configuration).",
		Aliases: []string{"compliance"},
	}
	cmd.AddCommand(
		newComplianceRulesGetCmd(loader),
		newComplianceRulesSetCmd(loader),
		newComplianceEvaluateCmd(),
	)
	return cmd
}

// registerComplianceCodecs wires the shared output codecs: text is the human default
// (not built-in, so it must be registered), yaml uses stable key ordering, json/agents
// are built-in.
func registerComplianceCodecs(io *cmdio.Options, flags *pflag.FlagSet, textCodec format.Codec) {
	io.RegisterCustomCodec("text", textCodec)
	io.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	io.DefaultFormat("text")
	io.BindFlags(flags)
}

type complianceRulesGetOpts struct {
	IO        cmdio.Options
	transport complianceTransport
}

func newComplianceRulesGetCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &complianceRulesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show the org's notification compliance rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := opts.transport.resolve(cmd.Context(), loader)
			if err != nil {
				return err
			}
			rules, err := client.GetComplianceRules(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *rules)
		},
	}
	registerComplianceCodecs(&opts.IO, cmd.Flags(), &complianceRulesTextCodec{})
	opts.transport.bind(cmd.Flags())
	return cmd
}

type complianceRulesSetOpts struct {
	IO               cmdio.Options
	transport        complianceTransport
	Interactive      bool
	RequiredChannels []string
	DefaultRules     []string
	ImportantRules   []string
}

func (o *complianceRulesSetOpts) setup(flags *pflag.FlagSet) {
	registerComplianceCodecs(&o.IO, flags, &complianceRulesTextCodec{})
	o.transport.bind(flags)
	flags.BoolVarP(&o.Interactive, "interactive", "i", false,
		"Select channels and notification rules interactively (pre-checked from the current ruleset)")
	flags.StringSliceVar(&o.RequiredChannels, "required-channels", nil,
		"Channels every user must have configured (comma-separated, e.g. phone,slack)")
	flags.StringSliceVar(&o.DefaultRules, "default", nil,
		"Notification rule types required in the default policy (e.g. notify_by_slack)")
	flags.StringSliceVar(&o.ImportantRules, "important", nil,
		"Notification rule types required in the important policy (e.g. notify_by_sms,notify_by_slack)")
}

func (o *complianceRulesSetOpts) Validate() error {
	if len(o.RequiredChannels) == 0 && len(o.DefaultRules) == 0 && len(o.ImportantRules) == 0 {
		return errors.New("at least one of --required-channels, --default, or --important is required (or use --interactive)")
	}
	return nil
}

// runInteractive pre-fills a multi-select form from the current ruleset (best-effort)
// and returns the rules the user selected.
func (o *complianceRulesSetOpts) runInteractive(ctx context.Context, client complianceAPI) (ComplianceRules, error) {
	var cur ComplianceRules
	if current, err := client.GetComplianceRules(ctx); err == nil && current != nil {
		cur = *current
	}

	var channels, def, imp []string
	form := huh.NewForm(huh.NewGroup(
		complianceMultiSelect("Required channels", "Channels every user must have configured",
			complianceChannelOptions, cur.RequiredChannels, &channels),
		complianceMultiSelect("Default policy", "Notification rule types required in the default policy",
			complianceRuleTypeOptions, cur.RequiredNotificationRules.Default, &def),
		complianceMultiSelect("Important policy", "Notification rule types required in the important policy",
			complianceRuleTypeOptions, cur.RequiredNotificationRules.Important, &imp),
	))
	if err := form.Run(); err != nil {
		return ComplianceRules{}, err
	}
	return ComplianceRules{
		RequiredChannels:          channels,
		RequiredNotificationRules: ComplianceNotificationRules{Default: def, Important: imp},
	}, nil
}

// complianceMultiSelect builds a multi-select with the items in `current` pre-checked.
func complianceMultiSelect(title, desc string, all, current []string, out *[]string) *huh.MultiSelect[string] {
	checked := make(map[string]bool, len(current))
	for _, c := range current {
		checked[c] = true
	}
	opts := make([]huh.Option[string], 0, len(all))
	for _, v := range all {
		opts = append(opts, huh.NewOption(complianceLabel(v), v).Selected(checked[v]))
	}
	return huh.NewMultiSelect[string]().Title(title).Description(desc).Options(opts...).Value(out)
}

func newComplianceRulesSetCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &complianceRulesSetOpts{}
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Create or update the org's notification compliance rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := opts.transport.resolve(cmd.Context(), loader)
			if err != nil {
				return err
			}

			var rules ComplianceRules
			if opts.Interactive {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return errors.New("--interactive requires a terminal; pass --required-channels/--default/--important instead")
				}
				rules, err = opts.runInteractive(cmd.Context(), client)
				if errors.Is(err, huh.ErrUserAborted) {
					fmt.Fprintln(cmd.ErrOrStderr(), "Aborted; no changes made.")
					return nil
				}
				if err != nil {
					return err
				}
			} else {
				if err := opts.Validate(); err != nil {
					return err
				}
				rules = ComplianceRules{
					RequiredChannels: opts.RequiredChannels,
					RequiredNotificationRules: ComplianceNotificationRules{
						Default:   opts.DefaultRules,
						Important: opts.ImportantRules,
					},
				}
			}

			result, err := client.SetComplianceRules(cmd.Context(), rules)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type complianceEvaluateOpts struct {
	IO        cmdio.Options
	transport complianceTransport
}

func newComplianceEvaluateCmd() *cobra.Command {
	opts := &complianceEvaluateOpts{}
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Evaluate org users against the compliance rules (who is ready to be paged).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			// evaluate is only on the public API today.
			if opts.transport.URL == "" {
				return errors.New("evaluate requires --oncall-url (test-only) until the endpoint is exposed via the plugin proxy")
			}
			client := newPublicComplianceClient(opts.transport.URL, opts.transport.Token, opts.transport.InstanceID)
			report, err := client.Evaluate(cmd.Context())
			if err != nil {
				return err
			}
			users, err := client.ListUsers(cmd.Context())
			if err != nil {
				// Best-effort enrichment: fall back to IDs only.
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not fetch user names: %v\n", err)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), enrichEvaluation(report, users))
		},
	}
	// evaluate defaults to a table; text/yaml/json remain available via -o.
	opts.IO.RegisterCustomCodec("table", &evaluationTableCodec{})
	opts.IO.RegisterCustomCodec("text", &complianceEvaluationTextCodec{})
	opts.IO.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())
	opts.transport.bind(cmd.Flags())
	return cmd
}

// --- test-only public OnCall API client ---

// publicComplianceClient talks directly to the OnCall engine public API. It exists only
// for end-to-end testing of the compliance endpoints until they are reachable through the
// plugin proxy; see the package comment above.
type publicComplianceClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
	instanceID string
}

func newPublicComplianceClient(baseURL, token, instanceID string) *publicComplianceClient {
	return &publicComplianceClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		instanceID: instanceID,
	}
}

func (c *publicComplianceClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return c.doURL(ctx, method, c.baseURL+path, body)
}

func (c *publicComplianceClient) doURL(ctx context.Context, method, fullURL string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.token)
	req.Header.Set("X-Grafana-Instance-ID", c.instanceID)
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

// publicUser is the subset of the public users API we use to label evaluation results.
type publicUser struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type publicUsersPage struct {
	Next    *string      `json:"next"`
	Results []publicUser `json:"results"`
}

// ListUsers returns org users keyed by their OnCall user ID (follows pagination).
func (c *publicComplianceClient) ListUsers(ctx context.Context) (map[string]publicUser, error) {
	out := make(map[string]publicUser)
	// short=true returns the lightweight user list (id/email/username/…) and skips the
	// per-user Slack enrichment that otherwise 500s when a Slack integration is unhealthy.
	url := c.baseURL + "/api/v1/users/?short=true"
	for url != "" {
		resp, err := c.doURL(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("irm: list users: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			err := handleErrorResponse(resp)
			resp.Body.Close()
			return nil, err
		}
		var page publicUsersPage
		decErr := json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if decErr != nil {
			return nil, fmt.Errorf("irm: decode users: %w", decErr)
		}
		for _, u := range page.Results {
			out[u.ID] = u
		}
		if page.Next == nil {
			break
		}
		url = *page.Next
	}
	return out, nil
}

func (c *publicComplianceClient) GetComplianceRules(ctx context.Context) (*ComplianceRules, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/oncall_compliance/", nil)
	if err != nil {
		return nil, fmt.Errorf("irm: get compliance rules: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var result ComplianceRules
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("irm: decode compliance rules: %w", err)
	}
	return &result, nil
}

func (c *publicComplianceClient) SetComplianceRules(ctx context.Context, rules ComplianceRules) (*ComplianceRules, error) {
	data, err := json.Marshal(rules)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal compliance rules: %w", err)
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/oncall_compliance/", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("irm: set compliance rules: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, handleErrorResponse(resp)
	}
	var result ComplianceRules
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("irm: decode compliance rules: %w", err)
	}
	return &result, nil
}

func (c *publicComplianceClient) Evaluate(ctx context.Context) (*ComplianceEvaluation, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/oncall_compliance/evaluate/", nil)
	if err != nil {
		return nil, fmt.Errorf("irm: evaluate compliance: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var result ComplianceEvaluation
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("irm: decode compliance evaluation: %w", err)
	}
	return &result, nil
}

// --- output codecs ---

// complianceRulesTextCodec renders ComplianceRules as a human summary.
type complianceRulesTextCodec struct{}

func (c *complianceRulesTextCodec) Format() format.Format { return format.Format("text") }

func (c *complianceRulesTextCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(ComplianceRules)
	if !ok {
		return fmt.Errorf("text codec: unsupported value type %T (expected ComplianceRules)", v)
	}
	if _, err := fmt.Fprintln(w, "Notification compliance rules:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Required channels: %s\n", joinOrDash(r.RequiredChannels)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Default policy:    %s\n", joinOrDash(r.RequiredNotificationRules.Default)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "  Important policy:  %s\n", joinOrDash(r.RequiredNotificationRules.Important))
	return err
}

func (c *complianceRulesTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// evaluationView is the user-facing evaluation report: raw evaluation joined with user
// names/emails. Used for all output formats so json/yaml also carry the labels.
type evaluationView struct {
	Compliant    []evalUser `json:"compliant"`
	NonCompliant []evalUser `json:"non_compliant"`
}

type evalUser struct {
	UserID     string   `json:"user_id"`
	Email      string   `json:"email,omitempty"`
	Username   string   `json:"username,omitempty"`
	Violations []string `json:"violations,omitempty"`
}

// label renders "name (user_id)", preferring email, then username, then just the ID.
func (u evalUser) label() string {
	if n := u.name(); n != u.UserID {
		return fmt.Sprintf("%s (%s)", n, u.UserID)
	}
	return u.UserID
}

// name returns the best display name: email, then username, then the ID.
func (u evalUser) name() string {
	switch {
	case u.Email != "":
		return u.Email
	case u.Username != "":
		return u.Username
	default:
		return u.UserID
	}
}

// enrichEvaluation joins a raw evaluation with user records (keyed by ID). users may be
// nil, in which case only IDs are shown.
func enrichEvaluation(e *ComplianceEvaluation, users map[string]publicUser) evaluationView {
	mk := func(id string, viol []string) evalUser {
		u := evalUser{UserID: id, Violations: viol}
		if info, ok := users[id]; ok {
			u.Email = info.Email
			u.Username = info.Username
		}
		return u
	}
	view := evaluationView{}
	for _, id := range e.Compliant {
		view.Compliant = append(view.Compliant, mk(id, nil))
	}
	for _, nc := range e.NonCompliant {
		view.NonCompliant = append(view.NonCompliant, mk(nc.UserID, nc.Violations))
	}
	return view
}

// evaluationTableCodec renders an evaluationView as a STATUS/USER/PROBLEMS table,
// non-compliant users first. It is the default format for `evaluate`.
type evaluationTableCodec struct{ noDecodeCodec }

func (c *evaluationTableCodec) Format() format.Format { return format.Format("table") }

func (c *evaluationTableCodec) Encode(w io.Writer, v any) error {
	e, ok := v.(evaluationView)
	if !ok {
		return fmt.Errorf("table codec: unsupported value type %T (expected evaluationView)", v)
	}
	t := style.NewTable("STATUS", "USER", "PROBLEMS")
	for _, u := range e.NonCompliant {
		problems := make([]string, len(u.Violations))
		for i, viol := range u.Violations {
			problems[i] = friendlyViolation(viol)
		}
		t.Row(complianceStatusIcon(false), u.name(), strings.Join(problems, "; "))
	}
	for _, u := range e.Compliant {
		t.Row(complianceStatusIcon(true), u.name(), "-")
	}
	return t.Render(w)
}

// complianceStatusIcon returns a green ✓ for compliant, a red ✗ otherwise. Falls back
// to the bare glyph when styling is disabled (piped output / --no-color).
func complianceStatusIcon(compliant bool) string {
	if !compliant {
		return style.ColorCell("✗", false, true)
	}
	if style.IsStylingEnabled() {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#7EB26D")).Render("✓")
	}
	return "✓"
}

// complianceEvaluationTextCodec renders an evaluationView report.
type complianceEvaluationTextCodec struct{}

func (c *complianceEvaluationTextCodec) Format() format.Format { return format.Format("text") }

func (c *complianceEvaluationTextCodec) Encode(w io.Writer, v any) error {
	e, ok := v.(evaluationView)
	if !ok {
		return fmt.Errorf("text codec: unsupported value type %T (expected evaluationView)", v)
	}
	if _, err := fmt.Fprintf(w, "Compliant users: %d\n", len(e.Compliant)); err != nil {
		return err
	}
	for _, u := range e.Compliant {
		if _, err := fmt.Fprintf(w, "  ✓ %s\n", u.label()); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Non-compliant users: %d\n", len(e.NonCompliant)); err != nil {
		return err
	}
	for _, u := range e.NonCompliant {
		if _, err := fmt.Fprintf(w, "  ✗ %s\n", u.label()); err != nil {
			return err
		}
		for _, v := range u.Violations {
			if _, err := fmt.Fprintf(w, "      - %s\n", friendlyViolation(v)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *complianceEvaluationTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// joinOrDash joins friendly labels for display, or "-" when empty.
func joinOrDash(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	labels := make([]string, len(s))
	for i, v := range s {
		labels[i] = complianceLabel(v)
	}
	return strings.Join(labels, ", ")
}
