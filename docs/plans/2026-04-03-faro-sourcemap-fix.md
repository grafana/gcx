# Faro Sourcemap Commands Fix

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all three Faro sourcemap commands (list, delete, upload) which are currently broken due to wrong API paths and wrong auth model.

**Architecture:** List/delete switch from the wrong plugin proxy path (`/api/plugins/.../resources/...`) to the correct one (`/api/plugin-proxy/.../api-proxy/...`). Upload switches from plugin proxy to the direct Faro API with `Bearer {stackId}:{cloudToken}` auth. The Faro API URL is auto-discovered from plugin settings and cached in provider config.

**Tech Stack:** Go, Cobra CLI, `k8s.io/client-go/rest`, `net/http`

---

### Task 1: Fix ListSourcemaps — correct path + pagination

**Files:**
- Modify: `internal/providers/faro/client.go:17-20` (remove `sourcemapBasePath`)
- Modify: `internal/providers/faro/client.go:210-226` (`ListSourcemaps`)
- Modify: `internal/providers/faro/client_test.go:291-337` (`TestClient_ListSourcemaps`)

**Step 1: Write the failing test**

Replace `TestClient_ListSourcemaps` in `client_test.go`. The test should verify:
- Correct path: `/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps` (not the old `/api/plugins/...` path)
- Query params: `?limit=100` on first page
- Pagination: follows `page.next` cursor when `page.hasNext=true`
- Returns aggregated `[]SourcemapBundle` from all pages

```go
func TestClient_ListSourcemaps(t *testing.T) {
	tests := []struct {
		name      string
		appID     string
		limit     int
		handler   http.HandlerFunc
		wantLen   int
		wantErr   bool
		wantFirst string
	}{
		{
			name:  "single page",
			appID: "42",
			limit: 0,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps", r.URL.Path)
				writeJSON(w, map[string]any{
					"bundles": []map[string]any{
						{"ID": "bundle-1", "Created": "2026-01-01T00:00:00Z", "Updated": "2026-01-01T00:00:00Z"},
						{"ID": "bundle-2", "Created": "2026-01-02T00:00:00Z", "Updated": "2026-01-02T00:00:00Z"},
					},
					"page": map[string]any{"hasNext": false, "limit": 100, "totalItems": 2},
				})
			},
			wantLen:   2,
			wantFirst: "bundle-1",
		},
		{
			name:  "auto-paginates",
			appID: "42",
			limit: 0,
			handler: func() http.HandlerFunc {
				call := 0
				return func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						writeJSON(w, map[string]any{
							"bundles": []map[string]any{
								{"ID": "bundle-1"},
							},
							"page": map[string]any{"hasNext": true, "next": "cursor-abc", "limit": 1, "totalItems": 2},
						})
						return
					}
					assert.Equal(t, "cursor-abc", r.URL.Query().Get("page"))
					writeJSON(w, map[string]any{
						"bundles": []map[string]any{
							{"ID": "bundle-2"},
						},
						"page": map[string]any{"hasNext": false, "limit": 1, "totalItems": 2},
					})
				}
			}(),
			wantLen: 2,
		},
		{
			name:  "respects limit",
			appID: "42",
			limit: 5,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "5", r.URL.Query().Get("limit"))
				writeJSON(w, map[string]any{
					"bundles": []map[string]any{{"ID": "b1"}},
					"page":    map[string]any{"hasNext": false, "limit": 5, "totalItems": 1},
				})
			},
			wantLen: 1,
		},
		{
			name:  "server error",
			appID: "42",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			result, err := c.ListSourcemaps(t.Context(), tt.appID, tt.limit)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result, tt.wantLen)
			if tt.wantFirst != "" {
				assert.Equal(t, tt.wantFirst, result[0].ID)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/providers/faro/ -run TestClient_ListSourcemaps -v`
Expected: compilation error — `SourcemapBundle` not exported, `ListSourcemaps` signature changed.

**Step 3: Implement in client.go**

1. Remove `sourcemapBasePath` constant (line 19).
2. Add exported `SourcemapBundle` type:

```go
// SourcemapBundle represents a sourcemap bundle from the Faro API.
type SourcemapBundle struct {
	ID      string `json:"ID"`
	Created string `json:"Created"`
	Updated string `json:"Updated"`
}
```

3. Add paginated response type (unexported):

```go
type sourcemapPage struct {
	Bundles []SourcemapBundle `json:"bundles"`
	Page    struct {
		HasNext    bool   `json:"hasNext"`
		Next       string `json:"next"`
		Limit      int    `json:"limit"`
		TotalItems int    `json:"totalItems"`
	} `json:"page"`
}
```

4. Rewrite `ListSourcemaps`:

```go
// ListSourcemaps retrieves sourcemap bundles for a Faro app.
// If limit is 0, all bundles are fetched via auto-pagination.
// If limit > 0, at most limit bundles are returned (single page).
func (c *Client) ListSourcemaps(ctx context.Context, appID string, limit int) ([]SourcemapBundle, error) {
	log := logging.FromContext(ctx)
	var all []SourcemapBundle
	pageSize := 100
	if limit > 0 {
		pageSize = limit
	}
	cursor := ""

	for {
		path := fmt.Sprintf("%s/%s/sourcemaps?limit=%d", basePath, url.PathEscape(appID), pageSize)
		if cursor != "" {
			path += "&page=" + url.QueryEscape(cursor)
		}
		log.Debug("Listing sourcemaps", "app_id", appID, "path", path)

		body, statusCode, err := c.doRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, fmt.Errorf("faro: list sourcemaps for app %s: %w", appID, err)
		}
		if statusCode >= 400 {
			return nil, fmt.Errorf("faro: list sourcemaps for app %s: status %d, body: %s", appID, statusCode, string(body))
		}

		var page sourcemapPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("faro: decode sourcemaps for app %s: %w", appID, err)
		}

		all = append(all, page.Bundles...)

		if !page.Page.HasNext || limit > 0 {
			break
		}
		cursor = page.Page.Next
	}

	log.Debug("Listed sourcemaps", "app_id", appID, "count", len(all))
	return all, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/providers/faro/ -run TestClient_ListSourcemaps -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/providers/faro/client.go internal/providers/faro/client_test.go
git commit -m "fix(faro): use correct plugin proxy path for ListSourcemaps with pagination

ListSourcemaps was using /api/plugins/.../resources/... which returns 500.
Switch to /api/plugin-proxy/.../api-proxy/... (same path as app CRUD).
Add auto-pagination support and return typed SourcemapBundle slice."
```

---

### Task 2: Fix DeleteSourcemap — batch endpoint + variadic IDs

**Files:**
- Modify: `internal/providers/faro/client.go:256-270` (`DeleteSourcemap`)
- Modify: `internal/providers/faro/client_test.go:353-364` (`TestClient_DeleteSourcemap`)

**Step 1: Write the failing test**

Replace `TestClient_DeleteSourcemap`:

```go
func TestClient_DeleteSourcemaps(t *testing.T) {
	tests := []struct {
		name      string
		appID     string
		bundleIDs []string
		handler   http.HandlerFunc
		wantErr   bool
	}{
		{
			name:      "single bundle",
			appID:     "42",
			bundleIDs: []string{"bundle-1"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps/batch/bundle-1", r.URL.Path)
				writeJSON(w, map[string]any{"status": "OK"})
			},
		},
		{
			name:      "multiple bundles",
			appID:     "42",
			bundleIDs: []string{"bundle-1", "bundle-2", "bundle-3"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps/batch/bundle-1,bundle-2,bundle-3", r.URL.Path)
				writeJSON(w, map[string]any{"status": "OK"})
			},
		},
		{
			name:      "server error",
			appID:     "42",
			bundleIDs: []string{"bundle-1"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			err := c.DeleteSourcemaps(t.Context(), tt.appID, tt.bundleIDs)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/providers/faro/ -run TestClient_DeleteSourcemaps -v`
Expected: compilation error — `DeleteSourcemaps` doesn't exist yet.

**Step 3: Implement in client.go**

Replace `DeleteSourcemap` with `DeleteSourcemaps`:

```go
// DeleteSourcemaps deletes one or more sourcemap bundles for a Faro app.
// Bundle IDs are joined with commas and sent to the batch delete endpoint.
func (c *Client) DeleteSourcemaps(ctx context.Context, appID string, bundleIDs []string) error {
	joined := strings.Join(bundleIDs, ",")
	path := fmt.Sprintf("%s/%s/sourcemaps/batch/%s", basePath, url.PathEscape(appID), joined)

	log := logging.FromContext(ctx)
	log.Info("Deleting sourcemaps", "app_id", appID, "bundle_ids", bundleIDs)

	body, statusCode, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("faro: delete sourcemaps for app %s: %w", appID, err)
	}

	if statusCode >= 400 {
		return fmt.Errorf("faro: delete sourcemaps for app %s: status %d, body: %s", appID, statusCode, string(body))
	}

	return nil
}
```

Add `"strings"` to imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/providers/faro/ -run TestClient_DeleteSourcemaps -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/providers/faro/client.go internal/providers/faro/client_test.go
git commit -m "fix(faro): switch DeleteSourcemaps to batch endpoint with variadic IDs

Old endpoint /api/plugins/.../sourcemaps/{id} returned 500.
New endpoint /api/plugin-proxy/.../sourcemaps/batch/{ids} uses
comma-separated IDs matching the actual Faro API."
```

---

### Task 3: Remove old UploadSourcemap from Client

**Files:**
- Modify: `internal/providers/faro/client.go:228-254` (remove `UploadSourcemap`)
- Modify: `internal/providers/faro/client_test.go:339-351` (remove `TestClient_UploadSourcemap`)

**Step 1: Delete `UploadSourcemap` method from `client.go` (lines 228-254)**

**Step 2: Delete `TestClient_UploadSourcemap` from `client_test.go` (lines 339-351)**

**Step 3: Verify compilation**

Run: `go build ./internal/providers/faro/`
Expected: compilation error in `sourcemap_commands.go` referencing `client.UploadSourcemap` — this is expected and will be fixed in Task 5.

**Step 4: Commit**

```bash
git add internal/providers/faro/client.go internal/providers/faro/client_test.go
git commit -m "refactor(faro): remove UploadSourcemap from plugin proxy Client

Upload will move to the direct Faro API with different auth model."
```

---

### Task 4: Add Faro API URL discovery + provider ConfigKeys

**Files:**
- Modify: `internal/providers/faro/provider.go:29` (`ConfigKeys`)
- Create: `internal/providers/faro/sourcemap_upload.go`
- Create: `internal/providers/faro/sourcemap_upload_test.go`

**Step 1: Write the failing test for DiscoverFaroAPIURL**

New file `sourcemap_upload_test.go`:

```go
package faro_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestDiscoverFaroAPIURL(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantURL string
		wantErr bool
	}{
		{
			name: "extracts api_endpoint from plugin settings",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/plugins/grafana-kowalski-app/settings", r.URL.Path)
				writeJSON(w, map[string]any{
					"jsonData": map[string]any{
						"api_endpoint": "https://faro-api-dev.grafana.net/faro",
					},
				})
			},
			wantURL: "https://faro-api-dev.grafana.net/faro",
		},
		{
			name: "returns error when plugin not installed",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
		{
			name: "returns error when api_endpoint missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"jsonData": map[string]any{},
				})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			cfg := config.NamespacedRESTConfig{
				Config:    rest.Config{Host: server.URL},
				Namespace: "stacks-13",
			}

			result, err := faro.DiscoverFaroAPIURL(t.Context(), cfg)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, result)
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/providers/faro/ -run TestDiscoverFaroAPIURL -v`
Expected: compilation error — `DiscoverFaroAPIURL` doesn't exist.

**Step 3: Implement**

New file `sourcemap_upload.go`:

```go
package faro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/grafana-app-sdk/logging"
	"k8s.io/client-go/rest"
)

const faroPluginSettingsPath = "/api/plugins/grafana-kowalski-app/settings"

// DiscoverFaroAPIURL queries the Grafana Faro plugin settings to discover
// the direct Faro API endpoint URL (jsonData.api_endpoint).
func DiscoverFaroAPIURL(ctx context.Context, cfg config.NamespacedRESTConfig) (string, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return "", fmt.Errorf("faro: create HTTP client for discovery: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Host+faroPluginSettingsPath, nil)
	if err != nil {
		return "", fmt.Errorf("faro: create discovery request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("faro: discover API URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("faro: plugin settings returned HTTP %d (is grafana-kowalski-app installed?)", resp.StatusCode)
	}

	var body struct {
		JSONData struct {
			APIEndpoint string `json:"api_endpoint"`
		} `json:"jsonData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("faro: decode plugin settings: %w", err)
	}

	if body.JSONData.APIEndpoint == "" {
		return "", fmt.Errorf("faro: api_endpoint not configured in Faro plugin settings")
	}

	return body.JSONData.APIEndpoint, nil
}

// GenerateBundleID creates a bundle ID matching the faro-cli pattern: {timestamp}-{randomHex5}.
func GenerateBundleID() string {
	ts := time.Now().UnixMilli()
	hex := fmt.Sprintf("%05x", rand.IntN(0xFFFFF))
	return fmt.Sprintf("%d-%s", ts, hex)
}

// UploadSourcemap uploads a sourcemap file to the direct Faro API.
// Auth uses Bearer {stackId}:{token} format.
func UploadSourcemap(ctx context.Context, faroAPIURL string, stackID int, token string, appID string, bundleID string, reader io.Reader, contentType string) error {
	endpoint := fmt.Sprintf("%s/api/v1/app/%s/sourcemaps/%s",
		strings.TrimRight(faroAPIURL, "/"), appID, bundleID)

	log := logging.FromContext(ctx)
	log.Info("Uploading sourcemap", "app_id", appID, "bundle_id", bundleID, "endpoint", endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, reader)
	if err != nil {
		return fmt.Errorf("faro: create upload request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %d:%s", stackID, token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("faro: upload sourcemap: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("faro: upload sourcemap: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}
```

Update `provider.go` `ConfigKeys`:

```go
func (p *FaroProvider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "faro-api-url", Secret: false},
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/providers/faro/ -run TestDiscoverFaroAPIURL -v`
Expected: PASS

**Step 5: Write test for GenerateBundleID**

Add to `sourcemap_upload_test.go`:

```go
func TestGenerateBundleID(t *testing.T) {
	id := faro.GenerateBundleID()
	parts := strings.SplitN(id, "-", 2)
	require.Len(t, parts, 2, "bundle ID should be timestamp-hex")
	assert.Len(t, parts[1], 5, "hex suffix should be 5 chars")
}
```

Run: `go test ./internal/providers/faro/ -run TestGenerateBundleID -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/providers/faro/sourcemap_upload.go internal/providers/faro/sourcemap_upload_test.go internal/providers/faro/provider.go
git commit -m "feat(faro): add Faro API URL discovery and direct upload client

DiscoverFaroAPIURL queries /api/plugins/grafana-kowalski-app/settings
for the api_endpoint. UploadSourcemap uses the direct Faro API with
Bearer {stackId}:{token} auth. GenerateBundleID matches faro-cli format.

ConfigKeys now declares faro-api-url for manual override."
```

---

### Task 5: Update sourcemap_commands.go — wire everything together

**Files:**
- Modify: `internal/providers/faro/sourcemap_commands.go` (all three commands)
- Modify: `internal/providers/faro/provider.go:50-58` (command wiring)

**Step 1: Update show-sourcemaps command**

Key changes:
- Change loader type from `RESTConfigLoader` to `*providers.ConfigLoader`
- Add `--limit` flag (int, default 0)
- Update `SourcemapTableCodec` to work with `[]SourcemapBundle` instead of `json.RawMessage`
- Update table columns: `BUNDLE ID`, `CREATED`, `UPDATED`

```go
type showSourcemapsOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *showSourcemapsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &SourcemapTableCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 0, "Maximum number of sourcemaps to return (0 for all)")
}

func newShowSourcemapsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &showSourcemapsOpts{}
	cmd := &cobra.Command{
		Use:   "show-sourcemaps <app-name>",
		Short: "Show sourcemaps for a Faro app.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			appID := resolveAppID(args[0])
			bundles, err := client.ListSourcemaps(ctx, appID, opts.Limit)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), bundles)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
```

Update `sourcemapBundle` → use exported `SourcemapBundle` from client.go. Remove the old unexported type.

Update `SourcemapTableCodec.Encode`:

```go
func (c *SourcemapTableCodec) Encode(w io.Writer, v any) error {
	bundles, ok := v.([]SourcemapBundle)
	if !ok {
		return fmt.Errorf("invalid data type for sourcemap table codec: expected []SourcemapBundle, got %T", v)
	}

	if len(bundles) == 0 {
		_, err := fmt.Fprintln(w, "No sourcemap bundles found.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "BUNDLE ID\tCREATED\tUPDATED")
	for _, b := range bundles {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", b.ID, b.Created, b.Updated)
	}
	return tw.Flush()
}
```

**Step 2: Update remove-sourcemap command**

Key changes:
- Change loader type to `*providers.ConfigLoader`
- Accept variadic bundle IDs: `cobra.MinimumNArgs(2)`
- Use `client.DeleteSourcemaps(ctx, appID, args[1:])`
- Update Use/Short to reflect plural

```go
func newRemoveSourcemapCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-sourcemap <app-name> <bundle-id> [bundle-id...]",
		Short: "Remove sourcemap bundles from a Faro app.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			appID := resolveAppID(args[0])
			bundleIDs := args[1:]
			if err := client.DeleteSourcemaps(ctx, appID, bundleIDs); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Removed %d sourcemap(s) from app %s", len(bundleIDs), appID)
			return nil
		},
	}
	return cmd
}
```

**Step 3: Rewrite apply-sourcemap command**

Key changes:
- Change loader type to `*providers.ConfigLoader`
- Use `LoadCloudConfig()` for stack ID + token
- Use `LoadGrafanaConfig()` for Faro API URL discovery
- Check provider config cache for `faro-api-url` first, fall back to discovery
- Add `--bundle-id` flag for explicit override
- Detect content type from file extension

```go
type applySourcemapOpts struct {
	File     string
	BundleID string
}

func (o *applySourcemapOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "Path to the sourcemap file to upload")
	flags.StringVar(&o.BundleID, "bundle-id", "", "Bundle ID (auto-generated if not set)")
}

func (o *applySourcemapOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func newApplySourcemapCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &applySourcemapOpts{}
	cmd := &cobra.Command{
		Use:   "apply-sourcemap <app-name>",
		Short: "Upload a sourcemap for a Faro app.",
		Example: `  # Upload a sourcemap file
  gcx faro apps apply-sourcemap my-web-app-42 -f bundle.js.map

  # Upload a gzipped tar bundle
  gcx faro apps apply-sourcemap my-web-app-42 -f sourcemaps.tar.gz

  # Upload with explicit bundle ID
  gcx faro apps apply-sourcemap my-web-app-42 -f bundle.js.map --bundle-id my-release-1`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()

			// Resolve Faro API URL: provider config cache → plugin settings discovery.
			faroAPIURL, err := resolveFaroAPIURL(ctx, loader)
			if err != nil {
				return err
			}

			// Load cloud config for stack ID + token.
			cloudCfg, err := loader.LoadCloudConfig(ctx)
			if err != nil {
				return fmt.Errorf("cloud config required for sourcemap upload: %w", err)
			}

			appID := resolveAppID(args[0])

			bundleID := opts.BundleID
			if bundleID == "" {
				bundleID = GenerateBundleID()
			}

			f, err := os.Open(opts.File)
			if err != nil {
				return fmt.Errorf("failed to open sourcemap file %s: %w", opts.File, err)
			}
			defer f.Close()

			contentType := "application/json"
			if strings.HasSuffix(opts.File, ".tar.gz") || strings.HasSuffix(opts.File, ".tgz") {
				contentType = "application/gzip"
			}

			if err := UploadSourcemap(ctx, faroAPIURL, cloudCfg.Stack.ID, cloudCfg.Token, appID, bundleID, f, contentType); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Uploaded sourcemap for app %s (bundle %s)", appID, bundleID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// resolveFaroAPIURL resolves the Faro API URL from provider config cache,
// falling back to auto-discovery from plugin settings.
func resolveFaroAPIURL(ctx context.Context, loader *providers.ConfigLoader) (string, error) {
	// Check provider config cache first.
	provCfg, _, err := loader.LoadProviderConfig(ctx, "faro")
	if err == nil && provCfg != nil && provCfg["faro-api-url"] != "" {
		return provCfg["faro-api-url"], nil
	}

	// Fall back to discovery from plugin settings.
	grafanaCfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("faro: grafana config required for API URL discovery: %w", err)
	}

	apiURL, err := DiscoverFaroAPIURL(ctx, grafanaCfg)
	if err != nil {
		return "", fmt.Errorf("faro API URL not configured and discovery failed: %w\n\nSet providers.faro.faro-api-url in config or GRAFANA_PROVIDER_FARO_FARO_API_URL env var", err)
	}

	// Cache for subsequent calls.
	_ = loader.SaveProviderConfig(ctx, "faro", "faro-api-url", apiURL)

	return apiURL, nil
}
```

**Step 4: Update provider.go command wiring**

The sourcemap commands already receive `loader` which is `*providers.ConfigLoader`.
Update the `RESTConfigLoader` interface references — since we're now using `*providers.ConfigLoader` directly,
remove the `RESTConfigLoader` interface from `resource_adapter.go` if it's only used by sourcemap commands.
Actually, `RESTConfigLoader` is also used by `commands.go` for app CRUD — leave it. Just change the sourcemap
command signatures to accept `*providers.ConfigLoader`.

**Step 5: Clean up imports**

Remove unused imports from `sourcemap_commands.go` (e.g., `encoding/json` if no longer needed).
Add new imports: `"strings"`, `"github.com/grafana/gcx/internal/providers"`.

**Step 6: Verify compilation**

Run: `go build ./cmd/gcx/`
Expected: PASS

**Step 7: Run all faro tests**

Run: `go test ./internal/providers/faro/ -v`
Expected: PASS (some test adjustments may be needed for codec changes)

**Step 8: Commit**

```bash
git add internal/providers/faro/sourcemap_commands.go internal/providers/faro/provider.go
git commit -m "feat(faro): wire updated sourcemap commands

show-sourcemaps: --limit flag, auto-pagination, typed SourcemapBundle output.
remove-sourcemap: variadic bundle IDs via batch endpoint.
apply-sourcemap: direct Faro API upload with auto-discovered URL + cloud auth."
```

---

### Task 6: Manual integration test

**Step 1: Build**

```bash
go build -buildvcs=false -o bin/gcx ./cmd/gcx/
```

**Step 2: Test list (should return empty or populated)**

```bash
bin/gcx --context dev faro apps show-sourcemaps grafana-frontend-dev-164
bin/gcx --context dev faro apps show-sourcemaps grafana-frontend-dev-164 --limit 5
bin/gcx --context dev faro apps show-sourcemaps grafana-frontend-dev-164 -o json
```

**Step 3: Test upload**

Create a minimal test sourcemap and upload:

```bash
echo '{"version":3,"sources":["test.js"],"mappings":"AAAA"}' > /tmp/test.js.map
bin/gcx --context dev faro apps apply-sourcemap grafana-frontend-dev-164 -f /tmp/test.js.map
```

**Step 4: Verify upload appears in list**

```bash
bin/gcx --context dev faro apps show-sourcemaps grafana-frontend-dev-164
```

**Step 5: Test delete**

```bash
bin/gcx --context dev faro apps remove-sourcemap grafana-frontend-dev-164 <bundle-id-from-step-4>
```

**Step 6: Run full quality gates**

```bash
GCX_AGENT_MODE=false make all
```

**Step 7: Commit any fixes from integration testing**

---

### Task 7: Final cleanup

**Step 1: Update findings doc**

Update `docs/specs/faro-provider/sourcemap-api-findings.md` with the resolution status.

**Step 2: Run quality gates one final time**

```bash
GCX_AGENT_MODE=false make all
```

**Step 3: Final commit + push**

```bash
git add -A
git commit -m "docs: update sourcemap API findings with resolution status"
git push
```
