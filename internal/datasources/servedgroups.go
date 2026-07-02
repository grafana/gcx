package datasources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// servedGroupCache memoizes, for the lifetime of a transport, which per-plugin
// datasource groups the stack serves (from the /apis discovery document) and a
// UID→pluginID index built by listing those groups. It is process-scoped only —
// never persisted to config — because app-platform availability varies per stack
// and per rollout, so a sticky on-disk cache would go stale silently.
type servedGroupCache struct {
	t *k8sTransport

	mu          sync.Mutex
	loaded      bool
	plugins     []string
	loadErr     error
	indexedFlag bool
	uidIndex    map[string]string
}

func newServedGroupCache(t *k8sTransport) *servedGroupCache {
	return &servedGroupCache{t: t}
}

// servedPlugins returns the plugin IDs whose datasource groups are served on
// this stack, discovering them once and caching the result (including errors).
func (c *servedGroupCache) servedPlugins(ctx context.Context) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.plugins, c.loadErr
	}
	c.loaded = true
	c.plugins, c.loadErr = c.discover(ctx)
	return c.plugins, c.loadErr
}

func (c *servedGroupCache) discover(ctx context.Context) ([]string, error) {
	status, body, err := c.t.do(ctx, http.MethodGet, "/apis", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		// Discovery unavailable — treat as "no app-platform groups" so callers
		// fall back to the legacy REST API.
		return nil, nil
	}
	var list struct {
		Groups []struct {
			Name string `json:"name"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("failed to parse /apis discovery: %w", err)
	}
	var plugins []string
	for _, g := range list.Groups {
		if id, ok := IsDatasourceGroup(g.Name); ok {
			plugins = append(plugins, id)
		}
	}
	return plugins, nil
}

func (c *servedGroupCache) indexed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.indexedFlag
}

func (c *servedGroupCache) setIndex(index map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uidIndex = index
	c.indexedFlag = true
}

func (c *servedGroupCache) lookup(uid string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.uidIndex[uid]
	return p, ok
}
