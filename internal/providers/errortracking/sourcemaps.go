package errortracking

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/net/publicsuffix"
)

const (
	maxUploadSize = 256 << 20
)

type upsertOpts struct {
	IO      cmdio.Options
	Project projectRef
	Release string
	Files   []string

	maps    []mapFile
	skipped []string
	links   []string
	total   int64
}

type mapFile struct {
	path, name, abs string
	data            []byte
}

func (o *upsertOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	flags.StringVar(&o.Release, "release", "", "Release the maps belong to, as the SDK sends it (required)")
	flags.StringArrayVarP(&o.Files, "filename", "f", nil, "A .map file or a directory of them (repeatable); each map is named after its bundle plus .map, must be JSON with version and mappings, and at most 256 MiB, as is the total (required)")
	_ = cobra.MarkFlagRequired(flags, "release")
	_ = cobra.MarkFlagRequired(flags, "filename")
	bindText(&o.IO, flags, mutationText{})
}

func (o *upsertOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	o.Release = strings.TrimSpace(o.Release)
	if o.Release == "" || o.Release == "." || o.Release == ".." {
		return usage(cmd, `--release must not be empty, "." or ".."`)
	}
	if err := validIO(cmd, &o.IO); err != nil {
		return err
	}
	return o.collect(cmd)
}

func isAppleDouble(name string) bool {
	return strings.HasPrefix(filepath.Base(name), "._")
}

func (o *upsertOpts) collect(cmd *cobra.Command) error {
	for _, in := range o.Files {
		if strings.TrimSpace(in) == "" {
			return usage(cmd, "-f must not be empty")
		}
	}
	for _, in := range o.Files {
		info, err := os.Stat(in)
		if err != nil {
			if _, lerr := os.Lstat(in); lerr == nil {
				return usage(cmd, fmt.Sprintf("-f %s: a link to a file that does not exist", printable(in)))
			}
			if errors.Is(err, fs.ErrNotExist) {
				return usage(cmd, fmt.Sprintf("-f %s: the file does not exist", printable(in)))
			}
			return err
		}
		switch {
		case info.IsDir():
			err = o.walk(cmd, strings.TrimRight(in, string(filepath.Separator))+string(filepath.Separator))
		case isAppleDouble(in):
			o.skipped = append(o.skipped, in)
		case !strings.HasSuffix(in, ".map"):
			err = usage(cmd, fmt.Sprintf("-f %s: the file must end in .map or be a directory", printable(in)))
		default:
			err = o.add(cmd, in, info)
		}
		if err != nil {
			return err
		}
	}
	if len(o.maps) == 0 {
		return usage(cmd, "no source maps to upload: every -f input was skipped")
	}
	return nil
}

func (o *upsertOpts) walk(cmd *cobra.Command, root string) error {
	found := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			o.links = append(o.links, p)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || isAppleDouble(p) || !strings.HasSuffix(p, ".map") {
			o.skipped = append(o.skipped, p)
			return nil
		}
		found++
		return o.add(cmd, p, info)
	})
	if err != nil {
		return err
	}
	if found == 0 {
		return usage(cmd, fmt.Sprintf("-f %s: no .map files in the directory", printable(strings.TrimRight(root, string(filepath.Separator)))))
	}
	return nil
}

func (o *upsertOpts) add(cmd *cobra.Command, p string, info fs.FileInfo) error {
	shown := printable(p)
	size := info.Size()
	switch {
	case !info.Mode().IsRegular() || size == 0:
		return usage(cmd, fmt.Sprintf("-f %s: not a file, or empty", shown))
	case size > maxUploadSize:
		return usage(cmd, fmt.Sprintf("-f %s: %d bytes is over the 256 MiB limit", shown, size))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	name := filepath.Base(p)
	for _, m := range o.maps {
		if m.abs == abs {
			return nil
		}
		if m.name == name {
			return usage(cmd, fmt.Sprintf("-f: %s and %s are both named %s, and the upload names each map by its file name", printable(m.path), shown, printable(name)),
				"Upload them in separate releases, or rename one")
		}
	}
	if o.total += size; o.total > maxUploadSize {
		return usage(cmd, "-f: the maps add up to over the 256 MiB limit at "+shown)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if why := mapProblem(data); why != "" {
		return usage(cmd, fmt.Sprintf("-f %s: not a source map (%s)", shown, why))
	}
	o.maps = append(o.maps, mapFile{path: p, name: name, abs: abs, data: data})
	return nil
}

func mapProblem(data []byte) string {
	var m struct {
		Version  json.RawMessage `json:"version"`
		Mappings json.RawMessage `json:"mappings"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		if _, ok := errors.AsType[*json.SyntaxError](err); ok {
			return "not JSON"
		}
		return "not a JSON object"
	}
	switch {
	case len(m.Version) == 0 || string(m.Version) == "null":
		return `no "version"`
	case len(m.Mappings) == 0 || string(m.Mappings) == "null":
		return `no "mappings"`
	}
	return ""
}

func (o *upsertOpts) uploaded() []uploadedMap {
	files := make([]uploadedMap, len(o.maps))
	for i, m := range o.maps {
		files[i] = uploadedMap{File: m.name, Bundle: strings.TrimSuffix(m.name, ".map"), Size: int64(len(m.data))}
	}
	return files
}

func newUpsertCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &upsertOpts{}
	cmd := &cobra.Command{
		Use:   "upsert",
		Short: "Upload the source maps of one release.",
		Long: `Upload the source maps of one release to an Error Tracking project,
replacing maps with the same name in that release. -f takes .map files and
directories (links and other files in them are skipped with a warning); every
map is checked before anything is sent. The upload uses a Cloud token with the
sourcemaps:write scope (cloud.<name>.token or GRAFANA_CLOUD_TOKEN), plus
stacks:read to look up the stack and confirm its address. The upload host must
share the Grafana server's registrable domain, which a custom domain normally
does not. For Frontend Observability apps, use gcx frontend apps
apply-sourcemap. ` + noDryRun,
		Example: `  # Upload every map in a build directory.
  gcx error-tracking sourcemaps upsert --project 314 --release v1.2.3 -f dist/

  # Upload several maps.
  gcx error-tracking sourcemaps upsert --project 314 --release v1.2.3 -f main.3f2a1c.js.map -f vendor.9b1e.js.map

  # Upload from CI with the Cloud token in the environment.
  GRAFANA_CLOUD_TOKEN=<token> \
  gcx et sourcemaps upsert --project 314 --release "$RELEASE" -f dist/

  # As an agent: the names of the maps that went up.
  gcx error-tracking sourcemaps upsert --project 314 --release v1.2.3 -f dist/ -o json --jq '.files[].file'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			snapshot, err := loader.LoadDirectProviderSnapshot(ctx, providers.DirectProviderPolicy{
				ProviderName:    providerName,
				RejectAutoLocal: true,
			})
			if err != nil {
				return err
			}
			if snapshot.GrafanaConfig == nil {
				return configError("uploading source maps needs the context's Grafana server to read the project",
					"gcx config set stacks.<name>.grafana.server <url>")
			}
			stackID, token, cloudStack, err := uploadAuth(ctx, cmd.ErrOrStderr(), snapshot)
			if err != nil {
				return err
			}
			c, err := newClientFor(*snapshot.GrafanaConfig)
			if err != nil {
				return err
			}
			id, err := opts.Project.resolve(ctx, loader, c)
			if err != nil {
				return err
			}
			p, err := c.getProject(ctx, id)
			if err != nil {
				return err
			}
			c.knowProject(id, p.Name)
			base, host, err := uploadURL(p.SourceMapUploadURL)
			if err != nil {
				return err
			}
			if err := sameSite(host, snapshot.GrafanaConfig.GrafanaURL, cloudStack); err != nil {
				return err
			}
			if err := upload(ctx, base, stackID, token, opts); err != nil {
				return err
			}
			m := sourceMapMutation{mutationBase: c.mutation("uploaded", "SourceMap", 0, true)}
			m.Release = opts.Release
			m.Files, m.Skipped, m.SkippedLinks, m.Host = opts.uploaded(), opts.skipped, opts.links, host
			noun := "source maps"
			if len(m.Files) == 1 {
				noun = "source map"
			}
			m.text = fmt.Sprintf("Uploaded %d %s for release %s to %s", len(m.Files), noun, printable(opts.Release), c.projectShown())
			if err := emit(cmd, &opts.IO, m); err != nil {
				return err
			}
			if opts.IO.OutputFormat != "text" {
				return nil
			}
			if len(m.Skipped) > 0 {
				files := "files that are not source maps"
				if len(m.Skipped) == 1 {
					files = "file that is not a source map"
				}
				cmdio.Warning(cmd.ErrOrStderr(), "skipped %d %s: %s", len(m.Skipped), files, printable(joinShort(m.Skipped)))
			}
			if len(m.SkippedLinks) > 0 {
				links := "links"
				if len(m.SkippedLinks) == 1 {
					links = "link"
				}
				cmdio.Warning(cmd.ErrOrStderr(), "skipped %d %s, since links are not followed: %s", len(m.SkippedLinks), links, printable(joinShort(m.SkippedLinks)))
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "--project 314 --release <release> -f <dir> -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func uploadTable() cmdio.Table[uploadedMap] {
	return cmdio.Table[uploadedMap]{Columns: []cmdio.Column[uploadedMap]{
		{Header: "FILE", Content: func(m uploadedMap) string { return m.File }},
		{Header: "BUNDLE", Content: func(m uploadedMap) string { return m.Bundle }},
		{Header: "SIZE", Content: func(m uploadedMap) string { return humanize.Bytes(uint64(max(m.Size, 0))) }},
	}}
}

func uploadAuth(ctx context.Context, stderr io.Writer, snapshot providers.DirectProviderSnapshot) (int64, string, bool, error) {
	cloud, err := snapshot.ResolveCloudConfig(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0, "", false, err
		}
		status := httpStatusOf(err)
		if _, lookup := errors.AsType[*providers.StackLookupError](err); status == 0 && !lookup {
			return 0, "", false, configError(printable(err.Error()),
				"Log in to Grafana Cloud: gcx cloud login",
				"Or set GRAFANA_CLOUD_TOKEN to a Cloud token with the sourcemaps:write and stacks:read scopes")
		}
		d := &gcxerrors.DetailedError{Summary: gcxerrors.SummaryAPIError, Details: printable(err.Error())}
		if status != 0 {
			d.Details = fmt.Sprintf("Grafana Cloud stack lookup failed (HTTP %d)", status)
		}
		switch status {
		case http.StatusUnauthorized:
			d.Summary, d.ExitCode = gcxerrors.SummaryAuthenticationFailed, new(gcxerrors.ExitAuthFailure)
		case http.StatusForbidden:
			d.Summary, d.ExitCode = gcxerrors.SummaryAuthorizationFailed, new(gcxerrors.ExitAuthFailure)
		}
		return 0, "", false, d
	}
	stackID := snapshot.GrafanaConfig.StackID()
	if stackID == 0 {
		stackID = int64(cloud.Stack.ID)
	}
	if stackID == 0 {
		return 0, "", false, configError("the stack ID is unknown, so the upload cannot be authorized",
			"Set it: gcx config set stacks.<name>.grafana.stack-id <stack-id>",
			"Or name the stack so gcx can look it up: gcx config set stacks.<name>.slug <stack-slug>, or set GRAFANA_CLOUD_STACK")
	}
	stackHost, grafanaHost := hostOf(cloud.Stack.URL), hostOf(snapshot.GrafanaConfig.GrafanaURL)
	if stackHost == "" && strings.TrimSpace(cloud.Stack.URL) != "" {
		cmdio.EmitWarn(stderr, fmt.Sprintf("uploading for the Cloud stack %s (ID %d), whose URL could not be read to compare with the Grafana server %s",
			printable(cloud.Stack.Slug), cloud.Stack.ID, printable(grafanaHost)))
	}
	if stackHost != "" && grafanaHost != "" && stackHost != grafanaHost {
		cmdio.EmitWarn(stderr, fmt.Sprintf("uploading for the Cloud stack %s (ID %d) at %s, which differs from the Grafana server %s",
			printable(cloud.Stack.Slug), cloud.Stack.ID, printable(stackHost), printable(grafanaHost)))
	}
	return stackID, cloud.Token, int64(cloud.Stack.ID) == stackID && stackHost != "" && stackHost == grafanaHost, nil
}

func httpStatusOf(err error) int {
	if e, ok := errors.AsType[*gcxerrors.HTTPStatusError](err); ok {
		return e.Status
	}
	if e, ok := errors.AsType[*cloud.GCOMHTTPError](err); ok {
		return e.Status
	}
	return 0
}

func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func uploadURL(raw string) (string, string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return "", "", &gcxerrors.DetailedError{
			Summary: gcxerrors.SummaryEndpointNotAvailable,
			Details: "This Grafana deployment has no source map upload service set up, so source maps cannot be uploaded.\nAsk your Grafana administrator or Grafana Labs account team to set it up.",
		}
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || strings.ContainsAny(base, "?#") ||
		(u.Scheme != "https" && (u.Scheme != "http" || !isLoopback(u.Hostname()))) {
		return "", "", errors.New("error tracking: the project's upload address is not an https URL without a query, fragment or user info (http only for localhost)")
	}
	return base, u.Host, nil
}

func sameSite(uploadHost, grafana string, cloudStack bool) error {
	upload, server := strings.ToLower(hostOf("https://"+uploadHost)), strings.ToLower(hostOf(grafana))
	if isLoopback(upload) && isLoopback(server) {
		return nil
	}
	if upload == server {
		return nil
	}
	if net.ParseIP(server) == nil && net.ParseIP(upload) == nil {
		site, err := publicsuffix.EffectiveTLDPlusOne(server)
		if other, err2 := publicsuffix.EffectiveTLDPlusOne(upload); err == nil && err2 == nil && site == other {
			return nil
		}
		if suffix, icann := publicsuffix.PublicSuffix(server); !icann && cloudStack && siblingUnder(upload, suffix) {
			return nil
		}
	}
	return configError(fmt.Sprintf("the project's upload address is on %s, outside the domain of the Grafana server %s, so the Cloud token is not sent", printable(upload), printable(server)),
		"Upload through a context whose Grafana server is the stack's own address, not a custom domain")
}

func siblingUnder(host, suffix string) bool {
	label, ok := strings.CutSuffix(host, "."+suffix)
	if !ok || label == "" || strings.Contains(label, ".") {
		return false
	}
	own, _ := publicsuffix.PublicSuffix(host)
	return own == suffix
}

func isLoopback(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func (o *upsertOpts) payload(endpoint string) (string, io.Reader, string, error) {
	if len(o.maps) == 1 {
		m := o.maps[0]
		return endpoint + "?file=" + url.QueryEscape(m.name), bytes.NewReader(m.data), "application/json", nil
	}
	data, err := pack(o.maps)
	if err != nil {
		return "", nil, "", err
	}
	return endpoint, bytes.NewReader(data), "application/gzip", nil
}

func pack(maps []mapFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, m := range maps {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: m.name, Mode: 0o644, Size: int64(len(m.data))}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(m.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func upload(ctx context.Context, base string, stackID int64, token string, opts *upsertOpts) error {
	endpoint, body, contentType, err := opts.payload(base + "/" + url.PathEscape(opts.Release))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %d:%s", stackID, token))

	resp, err := httputils.NewClient(httputils.ClientOpts{
		Timeout:       5 * time.Minute,
		DisableRetry:  true,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}).Do(req)
	if err != nil {
		return fmt.Errorf("error tracking: upload source maps: %w", err)
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); resp.StatusCode/100 == 3 && loc != "" {
		return fmt.Errorf("error tracking: the upload address redirects to %q (HTTP %d)", loc, resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 {
		body := errorBody(resp)
		err := statusError(resp, body)
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return err
		}
		details := fmt.Sprintf("The upload host refused the source maps for stack %d (HTTP %d)", stackID, resp.StatusCode)
		if status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err); ok && status.ServerMessage != "" {
			details += ": " + status.ServerMessage
		}
		return &gcxerrors.DetailedError{
			Summary: gcxerrors.SummaryAuthorizationFailed,
			Details: details,
			Suggestions: []string{
				fmt.Sprintf("Use a Cloud token with the sourcemaps:write scope for stack %d", stackID),
				"Check that the context's Grafana server is the stack the token belongs to",
			},
			ExitCode: new(gcxerrors.ExitAuthFailure),
		}
	}
	return nil
}

func configError(details string, suggestions ...string) *gcxerrors.DetailedError {
	return &gcxerrors.DetailedError{Summary: gcxerrors.SummaryInvalidConfiguration, Details: details, Suggestions: suggestions}
}

type sourceMap struct {
	Release    string `json:"release"`
	File       string `json:"file"`
	UploadedAt string `json:"uploadedAt"`
}

type sourcemapsListOpts struct {
	IO      cmdio.Options
	Project projectRef
	Release string
	Limit   int
}

func (o *sourcemapsListOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	flags.StringVar(&o.Release, "release", "", "Only the maps of this release, matched exactly")
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of source maps to show. 0 shows every map")
	bindListTable(&o.IO, flags, cmdio.Table[sourceMap]{Columns: []cmdio.Column[sourceMap]{
		{Header: "RELEASE", Content: func(m sourceMap) string { return m.Release }},
		{Header: "FILE", Content: func(m sourceMap) string { return m.File }},
		{Header: "UPLOADED", Visible: when, Content: func(m sourceMap) string { return m.UploadedAt }},
	}})
}

func (o *sourcemapsListOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if err := nonEmpty(cmd, "release"); err != nil {
		return err
	}
	o.Release = strings.TrimSpace(o.Release)
	if o.Limit < 0 {
		return usage(cmd, fmt.Sprintf("invalid --limit %d: must be >= 0 (0 shows every map)", o.Limit))
	}
	return validIO(cmd, &o.IO)
}

func newSourcemapsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &sourcemapsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the source maps uploaded to an Error Tracking project.",
		Long: `List the source maps uploaded to an Error Tracking project, newest first:
the release each belongs to, its file name and when it arrived. --release
keeps the maps of one release.

The command follows the API's pages until it has --limit maps; --limit 0
shows every map. A list cut off at --limit says so in list_meta and on stderr
with a command that shows more. An empty list can also mean the deployment
has no source map upload service.`,
		Example: `  # The latest uploads.
  gcx error-tracking sourcemaps list --project 314

  # Check a release's maps arrived.
  gcx et sourcemaps list --project checkout-api --release v1.2.3 --limit 0

  # As an agent: the file names of one release.
  gcx error-tracking sourcemaps list --project 314 --release v1.2.3 --limit 0 --json release,file`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			query := url.Values{}
			if opts.Release != "" {
				query.Set("release", opts.Release)
			}
			items, hasMore, err := collectPages(ctx, opts.Limit, func(ctx context.Context, cursor string, _ int) ([]sourceMap, string, error) {
				q := maps.Clone(query)
				if cursor != "" {
					q.Set("cursor", cursor)
				}
				var page struct {
					SourceMaps []sourceMap `json:"sourcemaps"`
					NextCursor string      `json:"nextCursor"`
				}
				err := c.do(ctx, http.MethodGet, routePath("projects", id, "sourcemaps"), q, nil, &page)
				return page.SourceMaps, page.NextCursor, err
			})
			if err != nil {
				return err
			}
			if err := encodeList(cmd, c, &opts.IO, items, cmdio.PagedListMeta(len(items), opts.Limit, hasMore, 0)); err != nil {
				return err
			}
			if opts.IO.JSONDiscovery {
				return nil
			}
			w := cmd.ErrOrStderr()
			if len(items) > 0 {
				return nil
			}
			if opts.Release != "" {
				release := printable(opts.Release)
				say(w, fmt.Sprintf("No source maps for release %s in %s; upload them", release, c.projectShown()),
					et("sourcemaps", "upsert", "--project", strconv.FormatInt(id, 10), "--release", release, "-f", dirSlot))
				return nil
			}
			say(w, fmt.Sprintf("No source maps in %s; upload some", c.projectShown()),
				fmt.Sprintf("gcx error-tracking sourcemaps upsert --project %d --release <release> -f <dir>", id))
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--project 314 --release v1.2.3 --json release,file"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
