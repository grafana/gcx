package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/resources"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// SchemaFetcher fetches OpenAPI v3 spec schemas from a Grafana server.
// It caches fetched documents on disk keyed by content hash.
type SchemaFetcher struct {
	baseURL    string
	httpClient *http.Client
	cache      *diskCache
}

// NewSchemaFetcher creates a SchemaFetcher using the given REST config for
// authentication and base URL.
func NewSchemaFetcher(cfg *rest.Config) (*SchemaFetcher, error) {
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP client: %w", err)
	}

	cacheDir, err := defaultCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolving cache directory: %w", err)
	}

	return &SchemaFetcher{
		baseURL:    strings.TrimSuffix(cfg.Host, "/"),
		httpClient: httpClient,
		cache:      &diskCache{dir: cacheDir},
	}, nil
}

// FetchSpecSchemas fetches OpenAPI v3 spec schemas for the given descriptors.
// Returns a map keyed by "group/version/kind" → resolved spec schema.
// Resource types without an OpenAPI schema (e.g. provider-backed resources)
// are silently omitted from the result.
func (f *SchemaFetcher) FetchSpecSchemas(
	ctx context.Context,
	descriptors resources.Descriptors,
) (map[string]map[string]any, error) {
	// Group descriptors by GroupVersion to know which OpenAPI docs to fetch.
	gvDescs := make(map[schema.GroupVersion][]resources.Descriptor)
	for _, d := range descriptors {
		gv := d.GroupVersion
		gvDescs[gv] = append(gvDescs[gv], d)
	}

	// Fetch the OpenAPI v3 index to get per-GV document URLs.
	index, err := f.fetchIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching OpenAPI index: %w", err)
	}

	// Parallel-fetch OpenAPI docs for each GV we need.
	type gvResult struct {
		gv      schema.GroupVersion
		schemas map[string]map[string]any // GVK key → spec schema
	}

	results := make([]gvResult, 0, len(gvDescs))
	var mu sync.Mutex

	errg, ctx := errgroup.WithContext(ctx)
	errg.SetLimit(10) // Bounded parallelism.

	for gv, descs := range gvDescs {
		indexKey := fmt.Sprintf("apis/%s/%s", gv.Group, gv.Version)
		entry, ok := index[indexKey]
		if !ok {
			continue // No OpenAPI doc for this GV (e.g. provider resource).
		}

		errg.Go(func() error {
			doc, fetchErr := f.fetchDocument(ctx, entry.ServerRelativeURL)
			if fetchErr != nil {
				return nil //nolint:nilerr // Skip unavailable docs — don't fail the whole operation.
			}

			extracted := extractSpecSchemas(doc, descs)
			mu.Lock()
			results = append(results, gvResult{gv: gv, schemas: extracted})
			mu.Unlock()
			return nil
		})
	}

	if err := errg.Wait(); err != nil {
		return nil, err
	}

	// Merge results into a single map keyed by "group/version/kind".
	merged := make(map[string]map[string]any)
	for _, r := range results {
		maps.Copy(merged, r.schemas)
	}

	return merged, nil
}

// openAPIIndex represents the /openapi/v3 index response.
type openAPIIndexEntry struct {
	ServerRelativeURL string `json:"serverRelativeURL"`
}

func (f *SchemaFetcher) fetchIndex(ctx context.Context) (map[string]openAPIIndexEntry, error) {
	body, err := f.doGet(ctx, f.baseURL+"/openapi/v3")
	if err != nil {
		return nil, err
	}

	var idx struct {
		Paths map[string]openAPIIndexEntry `json:"paths"`
	}
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("decoding OpenAPI index: %w", err)
	}

	return idx.Paths, nil
}

func (f *SchemaFetcher) fetchDocument(ctx context.Context, relativeURL string) (map[string]any, error) {
	// Extract hash from URL for caching.
	hash := extractHash(relativeURL)
	if hash != "" {
		if cached, ok := f.cache.Get(hash); ok {
			var doc map[string]any
			if err := json.Unmarshal(cached, &doc); err == nil {
				return doc, nil
			}
		}
	}

	fullURL := f.baseURL + relativeURL
	body, err := f.doGet(ctx, fullURL)
	if err != nil {
		return nil, err
	}

	// Cache the response.
	if hash != "" {
		_ = f.cache.Set(hash, body)
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decoding OpenAPI document: %w", err)
	}

	return doc, nil
}

func (f *SchemaFetcher) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// extractSpecSchemas finds resource schemas in an OpenAPI v3 document and
// returns their resolved spec property schemas.
func extractSpecSchemas(doc map[string]any, descs []resources.Descriptor) map[string]map[string]any {
	components, _ := doc["components"].(map[string]any)
	if components == nil {
		return nil
	}
	allSchemas, _ := components["schemas"].(map[string]any)
	if allSchemas == nil {
		return nil
	}

	// Build a set of GVKs we're looking for.
	wanted := make(map[string]bool)
	for _, d := range descs {
		key := d.GroupVersion.Group + "/" + d.GroupVersion.Version + "/" + d.Kind
		wanted[key] = true
	}

	result := make(map[string]map[string]any)

	for _, rawSchema := range allSchemas {
		schemaObj, ok := rawSchema.(map[string]any)
		if !ok {
			continue
		}

		// Check x-kubernetes-group-version-kind to match against our descriptors.
		xgvk, _ := schemaObj["x-kubernetes-group-version-kind"].([]any)
		for _, rawGVK := range xgvk {
			gvkMap, ok := rawGVK.(map[string]any)
			if !ok {
				continue
			}

			group, _ := gvkMap["group"].(string)
			version, _ := gvkMap["version"].(string)
			kind, _ := gvkMap["kind"].(string)
			key := group + "/" + version + "/" + kind

			if !wanted[key] {
				continue
			}

			// Extract the spec property schema.
			specSchema := resolveSpecSchema(schemaObj, allSchemas)
			if specSchema != nil {
				result[key] = specSchema
			}
		}
	}

	return result
}

// resolveSpecSchema extracts and resolves the "spec" property from a resource
// schema. It follows one level of $ref to get the actual spec type definition.
func resolveSpecSchema(resourceSchema map[string]any, allSchemas map[string]any) map[string]any {
	props, _ := resourceSchema["properties"].(map[string]any)
	if props == nil {
		return nil
	}

	specProp, _ := props["spec"].(map[string]any)
	if specProp == nil {
		return nil
	}

	// Resolve $ref or allOf[{$ref}].
	ref := resolveRef(specProp)
	if ref == "" {
		// Inline spec — return as-is.
		return specProp
	}

	refKey := strings.TrimPrefix(ref, "#/components/schemas/")
	resolved, _ := allSchemas[refKey].(map[string]any)
	if resolved == nil {
		// Unresolvable ref — return the spec with the ref intact.
		return specProp
	}

	return resolved
}

// resolveRef extracts a $ref string from a property that may use direct $ref
// or allOf[{$ref}] patterns.
func resolveRef(prop map[string]any) string {
	if ref, ok := prop["$ref"].(string); ok {
		return ref
	}

	if allOf, ok := prop["allOf"].([]any); ok {
		for _, item := range allOf {
			if m, ok := item.(map[string]any); ok {
				if ref, ok := m["$ref"].(string); ok {
					return ref
				}
			}
		}
	}

	return ""
}

// extractHash produces a safe cache key for an OpenAPI serverRelativeURL.
// The server-supplied ?hash= parameter is intentionally ignored: using it
// directly as a filename would expose a path-traversal vector when connecting
// to a malicious server. Instead we always SHA-256 the full URL (which already
// contains the server hash as a query param), giving stable, content-addressed
// keys without trusting server input.
func extractHash(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:16])
}

// diskCache provides simple on-disk caching of OpenAPI documents.
type diskCache struct {
	dir string
}

func defaultCacheDir() (string, error) {
	if dir := os.Getenv("GCX_OPENAPI_CACHE_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "gcx", "openapi"), nil
}

func (c *diskCache) Get(hash string) ([]byte, bool) {
	data, err := os.ReadFile(filepath.Join(c.dir, hash+".json"))
	if err != nil {
		return nil, false
	}
	return data, true
}

func (c *diskCache) Set(hash string, data []byte) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.dir, hash+".json"), data, 0o600)
}
