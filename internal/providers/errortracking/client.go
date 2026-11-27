package errortracking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/retry"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const apiGroup = "error-tracking.ext.grafana.app"

const apiVersion = apiGroup + "/v0alpha1"

const apiPrefix = "/apis/" + apiVersion

const maxPageSize = 200

type client struct {
	http      *http.Client
	host      string
	namespace string
	robot     bool
	project   *projectRef
}

func newClient(ctx context.Context, loader *providers.ConfigLoader) (*client, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}
	return newClientFor(cfg)
}

func connect(ctx context.Context, loader *providers.ConfigLoader, t target, guard func(*client) error) (*client, int64, error) {
	c, err := newClient(ctx, loader)
	if err != nil {
		return nil, 0, err
	}
	if guard != nil {
		if err := guard(c); err != nil {
			return nil, 0, err
		}
	}
	id, err := t.resolve(ctx, loader, c)
	return c, id, err
}

func newClientFor(cfg config.NamespacedRESTConfig) (*client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("error tracking: build HTTP client: %w", err)
	}
	noRedirect := *httpClient
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client{
		http: &noRedirect, host: strings.TrimRight(cfg.Host, "/"), namespace: cfg.Namespace,
		robot: strings.HasPrefix(cfg.BearerToken, "glsa_"),
	}, nil
}

func (c *client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	endpoint := fmt.Sprintf("%s%s/namespaces/%s/%s", c.host, apiPrefix, url.PathEscape(c.namespace), path)
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("error tracking: encode request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	if method != http.MethodGet {
		ctx = retry.WithoutRetries(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("error tracking: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("error tracking: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 3 {
		return fmt.Errorf("error tracking: %s %s redirects to %q (HTTP %d): set the context's Grafana server to the final address", method, path, resp.Header.Get("Location"), resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 {
		return c.handleError(ctx, resp, method, path)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil
	}
	data, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return fmt.Errorf("error tracking: read %s response: %w", path, err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("error tracking: decode %s response: %w", path, err)
	}
	return nil
}

func errorBody(resp *http.Response) []byte {
	body, _ := httputils.ReadResponseBody(resp.Body, 1<<20)
	return body
}

func (c *client) handleError(ctx context.Context, resp *http.Response, method, path string) error {
	body := errorBody(resp)
	if _, _, fromET := etMessage(body); !fromET && resp.StatusCode == http.StatusNotFound {
		switch c.servesAPI(ctx) {
		case http.StatusNotFound:
			return notEnabled()
		case http.StatusOK:
			return unknownRoute(method, path)
		}
		return statusError(resp, body)
	}
	if resp.StatusCode == http.StatusNotFound {
		if err := c.missing(path); err != nil {
			return err
		}
	}
	return statusError(resp, body)
}

func (c *client) missing(path string) error {
	nouns := map[string][2]string{
		"projects":  {"project", "gcx error-tracking projects list"},
		"errors":    {"error", "gcx error-tracking errors list --project {project}"},
		"events":    {"event", "gcx error-tracking errors events list {error} --project {project}"},
		"rawEvents": {"raw event", "gcx error-tracking raw-events list --project {project}"},
		"comments":  {"comment", "gcx error-tracking errors activity list {error} --project {project} --type comment"},
		"assignees": {"user", "gcx error-tracking users list --project {project}"},
		"heatmaps":  {"heatmap", "gcx error-tracking heatmaps list"},
	}
	segs := strings.Split(path, "/")
	var what []string
	ids := map[string]string{}
	list, last := "", ""
	for i := 0; i+1 < len(segs); i += 2 {
		noun, ok := nouns[segs[i]]
		if !ok {
			break
		}
		id, _ := url.PathUnescape(segs[i+1])
		shown := noun[0] + " " + printable(id)
		if p := c.project; noun[0] == "project" && p != nil && strconv.FormatInt(p.id, 10) == id {
			shown = p.shown()
		}
		what = append([]string{shown}, what...)
		ids[noun[0]] = printable(id)
		list, last = noun[1], noun[0]
	}
	if len(what) == 0 {
		return nil
	}
	if p := c.project; len(what) == 1 && last == "project" && p != nil && p.source {
		return p.gone()
	}
	for k, v := range ids {
		list = strings.ReplaceAll(list, "{"+k+"}", v)
	}
	suggestions := []string{"List what exists: " + list}
	if len(what) > 1 {
		suggestions = append(suggestions, "Check the project too: gcx error-tracking projects list")
	}
	details := "Error Tracking has no " + strings.Join(what, " in ")
	if last == "raw event" {
		details += ". Only the latest items are kept"
	}
	return &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryResourceNotFound,
		Details:     details,
		Suggestions: suggestions,
	}
}

// servesAPI asks the stack whether it serves the Error Tracking API version at
// all. It tells a stack without Error Tracking (404) apart from one whose API
// lacks a single route (200). Any other answer returns its status, or 0 when
// the request failed.
func (c *client) servesAPI(ctx context.Context) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+apiPrefix, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 == 2 {
		return http.StatusOK
	}
	return resp.StatusCode
}

func unknownRoute(method, path string) *gcxerrors.DetailedError {
	return &gcxerrors.DetailedError{
		Summary: gcxerrors.SummaryEndpointNotAvailable,
		Details: fmt.Sprintf("Error Tracking on this stack serves %s but not %s %s (HTTP 404).\nThis gcx and the stack's Error Tracking API may be different versions.", apiVersion, method, printable(path)),
		Suggestions: []string{
			"Update gcx to the latest release",
			"If gcx is up to date, the stack may run an older Error Tracking API: try again later",
		},
	}
}

func notEnabled() *gcxerrors.DetailedError {
	return &gcxerrors.DetailedError{
		Summary: gcxerrors.SummaryEndpointNotAvailable,
		Details: "Error Tracking isn't enabled for this stack.\nAsk your Grafana Labs account team to enable it.",
	}
}

type statusConflictError struct {
	*gcxerrors.HTTPStatusError

	CurrentStatus string
}

func (e *statusConflictError) Unwrap() error { return e.HTTPStatusError }

func etMessage(body []byte) (string, string, bool) {
	var et struct {
		Error *struct {
			Message       string `json:"message"`
			CurrentStatus string `json:"currentStatus"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &et) != nil || et.Error == nil {
		return "", "", false
	}
	if et.Error.CurrentStatus != "" {
		return et.Error.Message + " (current status: " + et.Error.CurrentStatus + ")", et.Error.CurrentStatus, true
	}
	return et.Error.Message, "", true
}

func statusError(resp *http.Response, body []byte) error {
	msg, current, _ := etMessage(body)
	if msg != "" {
		if b, err := json.Marshal(map[string]string{"message": msg}); err == nil {
			body = b
		}
	}
	err := providers.FormatError(resp.StatusCode, body)
	if status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err); ok {
		status.Message, status.ServerMessage, status.TraceID = printable(status.Message), printable(status.ServerMessage), printable(status.TraceID)
		status.ContentType = resp.Header.Get("Content-Type")
		if current != "" {
			return &statusConflictError{HTTPStatusError: status, CurrentStatus: printable(current)}
		}
	}
	return err
}

func badRequest(err error, suggestion string) error {
	status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
	if !ok || status.Status != http.StatusBadRequest {
		return err
	}
	details := fmt.Sprintf("%s (HTTP %d)", status.ServerMessage, status.Status)
	if status.ServerMessage == "" {
		details = fmt.Sprintf("the server rejected the request (HTTP %d)", status.Status)
	}
	return &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryAPIError,
		Details:     details,
		Suggestions: []string{suggestion},
	}
}

func routePath(segments ...any) string {
	parts := make([]string, len(segments))
	for i, s := range segments {
		parts[i] = url.PathEscape(fmt.Sprint(s))
	}
	return strings.Join(parts, "/")
}

func collectPages[T any](ctx context.Context, limit int, fetch func(ctx context.Context, cursor string, pageSize int) ([]T, string, error)) ([]T, bool, error) {
	items := []T{}
	cursor := ""
	seen := map[string]bool{}
	for {
		size := maxPageSize
		if limit > 0 {
			size = min(limit-len(items), maxPageSize)
		}
		page, next, err := fetch(ctx, cursor, size)
		if err != nil {
			return nil, false, err
		}
		items = append(items, page...)
		switch {
		case limit > 0 && len(items) >= limit:
			return items[:limit], next != "" || len(items) > limit, nil
		case next == "":
			return items, false, nil
		case seen[next]:
			return nil, false, errors.New("error tracking: the server repeated a page cursor")
		}
		seen[next] = true
		cursor = next
	}
}

type list[T any] []T

func (l list[T]) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]T(l))
}

type itemList[T any] struct {
	Items    []T             `json:"items"`
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty"`
}

func encodeList[T any](cmd *cobra.Command, c *client, opts *cmdio.Options, items []T, meta *cmdio.ListMeta) error {
	meta, err := writeList(cmd, c, opts, items, meta)
	if err == nil && !opts.JSONDiscovery {
		cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
	}
	return err
}

func writeList[T any](cmd *cobra.Command, c *client, opts *cmdio.Options, items []T, meta *cmdio.ListMeta) (*cmdio.ListMeta, error) {
	if items == nil {
		items = []T{}
	}
	if meta != nil && meta.Continue == "" {
		meta = cmdio.AttachListMeta(meta, c.project.argv())
	}
	return meta, opts.Encode(cmd.OutOrStdout(), itemList[T]{Items: items, ListMeta: meta})
}

func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, s)
}

const when cmdio.Visibility = "time"

func cleanTable[R any](t cmdio.Table[R]) cmdio.Table[R] {
	columns := make([]cmdio.Column[R], 0, len(t.Columns))
	for _, c := range t.Columns {
		if c.Visible == when {
			columns = append(columns,
				cmdio.Column[R]{Header: c.Header, Visible: cmdio.NarrowOnly, Content: func(r R) string { return age(printable(c.Content(r))) }},
				cmdio.Column[R]{Header: c.Header, Visible: cmdio.WideOnly, Content: func(r R) string { return stamp(printable(c.Content(r))) }})
			continue
		}
		content := c.Content
		c.Content = func(r R) string { return printable(content(r)) }
		columns = append(columns, c)
	}
	t.Columns = columns
	return t
}

func bindTable[V, R any](opts *cmdio.Options, flags *pflag.FlagSet, t cmdio.Table[R], rows func(V) []R) {
	t = cleanTable(t)
	a := cmdio.TableAdapter[V, R]{Table: t, Rows: func(v V) ([]R, error) { return rows(v), nil }}
	opts.RegisterCustomCodec(cmdio.FormatTable, a.Codec(cmdio.FormatTable))
	if slices.ContainsFunc(t.Columns, func(c cmdio.Column[R]) bool { return c.Visible == cmdio.WideOnly }) {
		opts.RegisterCustomCodec(cmdio.FormatWide, a.Codec(cmdio.FormatWide))
	}
	opts.DefaultFormat(cmdio.FormatTable)
	opts.BindFlags(flags)
}

func bindListTable[T any](opts *cmdio.Options, flags *pflag.FlagSet, t cmdio.Table[T]) {
	bindTable(opts, flags, t, func(l itemList[T]) []T { return l.Items })
}

func bindSingleTable[T any](opts *cmdio.Options, flags *pflag.FlagSet, t cmdio.Table[T]) {
	bindTable(opts, flags, t, func(v T) []T { return []T{v} })
}

func wrap[T adapter.ResourceNamer](c *client, kind string, spec T) adapter.TypedObject[T] {
	return adapter.TypedObject[T]{
		TypeMeta:   metav1.TypeMeta{APIVersion: apiVersion, Kind: kind},
		ObjectMeta: metav1.ObjectMeta{Name: spec.GetResourceName(), Namespace: c.namespace},
		Spec:       spec,
	}
}
