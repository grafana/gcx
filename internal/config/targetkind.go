package config

import (
	"bytes"
	"context"
	"maps"
	"net/url"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/format"
)

// Target kind values reported by LoadTargetKind. Deliberately coarse: never
// the URL, hostname, or stack slug.
const (
	targetKindCloud       = "cloud"
	targetKindSelfManaged = "self-managed"
)

// LoadTargetKind classifies the current context's Grafana target as "cloud"
// or "self-managed" for anonymous usage telemetry. Like LoadDiagnostics it
// reads the layered config without building the full Config: it never probes
// the OS keychain, never prompts, and never auto-creates a config file.
// Returns "" when no target is resolvable (no config, no current context,
// malformed files).
func LoadTargetKind(ctx context.Context) string {
	view := newTargetKindView()
	for _, path := range diagnosticsSourcePaths(ctx) {
		layer, err := readTargetKindLayer(path)
		if err != nil {
			continue
		}
		view.merge(layer)
	}
	return view.classify()
}

// targetKindStack carries the per-stack fields Context.IsCloud consults.
type targetKindStack struct {
	slug    string
	stackID int64
	server  string
}

// targetKindView is the minimal layered-config view needed to classify the
// current context: the context→stack references and per-stack cloud signals.
type targetKindView struct {
	currentContext string
	contextStacks  map[string]string
	stacks         map[string]targetKindStack
}

func newTargetKindView() *targetKindView {
	return &targetKindView{
		contextStacks: map[string]string{},
		stacks:        map[string]targetKindStack{},
	}
}

// merge folds a higher-precedence layer into the view, mirroring MergeConfigs:
// current-context is last-non-empty-wins, stack entries are atomic per name,
// and a context's stack reference is only overridden by a non-empty one.
func (view *targetKindView) merge(layer *targetKindView) {
	if layer.currentContext != "" {
		view.currentContext = layer.currentContext
	}
	maps.Copy(view.stacks, layer.stacks)
	for name, stack := range layer.contextStacks {
		if stack != "" {
			view.contextStacks[name] = stack
		}
	}
}

// classify applies the Context.IsCloud signals to the resolved current
// context. GRAFANA_CLOUD_STACK and GRAFANA_SERVER are honoured directly
// (as a full load's env parsing would) so env-only detached contexts are
// classified too.
func (view *targetKindView) classify() string {
	stack := view.stacks[view.contextStacks[view.currentContext]]
	slug := os.Getenv("GRAFANA_CLOUD_STACK")
	if slug == "" {
		slug = stack.slug
	}
	server := os.Getenv("GRAFANA_SERVER")
	if server == "" {
		server = stack.server
	}
	if slug != "" || stack.stackID != 0 {
		return targetKindCloud
	}
	if server == "" {
		return ""
	}
	if parsed, err := url.Parse(server); err == nil && IsGrafanaCloudHost(strings.ToLower(parsed.Hostname())) {
		return targetKindCloud
	}
	return targetKindSelfManaged
}

// readTargetKindLayer decodes one config file into a view, following
// readDiagnostics: full-struct decode, but no keychain resolution, no
// migration, no auto-creation. Legacy-format files are read through the
// legacy struct, where each context carries its own grafana block and cloud
// stack slug. Missing or malformed files yield (nil, err).
func readTargetKindLayer(path string) (*targetKindView, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := validateDeclaredConfigVersion(path, contents); err != nil {
		return nil, err
	}
	layer := newTargetKindView()
	codec := &format.YAMLCodec{BytesAsBase64: true}
	if isLegacyConfig(contents) {
		var lc legacyConfig
		if err := codec.Decode(bytes.NewBuffer(contents), &lc); err != nil {
			return nil, err
		}
		layer.currentContext = lc.CurrentContext
		for name, lctx := range lc.Contexts {
			if lctx == nil {
				continue
			}
			stack := targetKindStack{}
			if lctx.Cloud != nil {
				stack.slug = lctx.Cloud.Stack
			}
			if lctx.Grafana != nil {
				stack.stackID = lctx.Grafana.StackID
				stack.server = lctx.Grafana.Server
			}
			// Mirror the in-memory migration: each legacy context becomes a
			// same-named stack referenced by the context.
			layer.stacks[name] = stack
			layer.contextStacks[name] = name
		}
		return layer, nil
	}
	var cfg Config
	if err := codec.Decode(bytes.NewBuffer(contents), &cfg); err != nil {
		return nil, err
	}
	layer.currentContext = cfg.CurrentContext
	for name, stack := range cfg.Stacks {
		if stack == nil {
			continue
		}
		entry := targetKindStack{slug: stack.Slug}
		if stack.Grafana != nil {
			entry.stackID = stack.Grafana.StackID
			entry.server = stack.Grafana.Server
		}
		layer.stacks[name] = entry
	}
	for name, c := range cfg.Contexts {
		if c == nil {
			continue
		}
		layer.contextStacks[name] = c.Stack
	}
	return layer, nil
}
