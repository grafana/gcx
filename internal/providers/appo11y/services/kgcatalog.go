package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/config"
	kgquery "github.com/grafana/gcx/internal/query/kg"
	"github.com/spf13/pflag"
)

type kgMode string

const (
	kgModeAuto kgMode = "auto"
	kgModeOff  kgMode = "off"
)

// resolveKGMode validates a --kg flag value.
func resolveKGMode(raw string) (kgMode, error) {
	switch kgMode(strings.ToLower(strings.TrimSpace(raw))) {
	case kgModeAuto:
		return kgModeAuto, nil
	case kgModeOff:
		return kgModeOff, nil
	default:
		return "", fmt.Errorf("--kg must be %q or %q, got %q", kgModeAuto, kgModeOff, raw)
	}
}

// kgFlags is the --kg flag registration shared by every gated services
// subcommand.
type kgFlags struct {
	Mode string
}

func (f *kgFlags) register(flags *pflag.FlagSet) {
	flags.StringVar(&f.Mode, "kg", string(kgModeAuto), "Knowledge Graph catalog consumption: auto (annotate rows with what the graph knows, when it's active) or off (never contact the Knowledge Graph)")
}

func (f *kgFlags) resolve() (kgMode, error) { return resolveKGMode(f.Mode) }

// catalog resolves the flag value and builds the kgCatalog for it in one
// step — the pattern every gated run* function needs right after its
// activation check passes.
func (f *kgFlags) catalog(cfg config.NamespacedRESTConfig) (*kgCatalog, error) {
	mode, err := f.resolve()
	if err != nil {
		return nil, err
	}
	return newKGCatalog(cfg, mode), nil
}

// KGRef records what the Knowledge Graph knows about a service. Present
// only when --kg is auto AND the graph was reachable, active, and knew
// about this service — pointer + omitempty so JSON on any other path (off,
// inactive, unreachable, unknown service) stays byte-identical to before
// this field existed.
type KGRef struct {
	Known      bool              `json:"known" yaml:"known"`
	EntityType string            `json:"entity_type,omitempty" yaml:"entity_type,omitempty"`
	Scope      map[string]string `json:"scope,omitempty" yaml:"scope,omitempty"`
}

// kgCatalog is the services package's best-effort Knowledge Graph lookup
// surface. A nil *kgCatalog means "don't consult the graph at all" — every
// method is safe to call through a nil-checked call site
// (`if cat != nil { ... }`), never through a nil-safe method receiver, so
// callers stay explicit about when the graph is in play.
type kgCatalog struct {
	client *kgquery.Client
}

// newKGCatalog builds a kgCatalog for mode, or returns nil for kgModeOff or
// on any client-construction failure — construction failure is exceedingly
// rare (bad TLS/transport config) and, consistent with --kg auto's
// best-effort contract, degrading to "no graph enrichment" is preferable to
// failing an otherwise-working classic command over it.
func newKGCatalog(cfg config.NamespacedRESTConfig, mode kgMode) *kgCatalog {
	if mode == kgModeOff {
		return nil
	}
	client, err := kgquery.NewClient(cfg)
	if err != nil {
		return nil
	}
	return &kgCatalog{client: client}
}

// lookup resolves one service by bare name, with no scope filter (see the
// name-join caveat on Service.Namespace vs. the Knowledge Graph's own
// "namespace" scope dimension: they're different things, so this package
// deliberately matches leniently across whatever scope the graph has the
// entity in rather than guess a filter). Returns nil whenever the graph
// doesn't know the service, isn't active, or the lookup fails for any
// reason — this method never returns an error; every failure mode collapses
// to "nothing to annotate with," per --kg auto's best-effort contract.
func (c *kgCatalog) lookup(ctx context.Context, name string) *KGRef {
	active, err := c.client.Active(ctx)
	if err != nil || !active {
		return nil
	}
	entity, err := c.client.LookupEntity(ctx, "Service", name, nil, "", 0, 0)
	if err != nil || entity == nil {
		return nil
	}
	return &KGRef{Known: true, EntityType: entity.Type, Scope: entity.Scope}
}

// index builds a bare-name -> *KGRef map for every Service-type entity the
// Knowledge Graph currently knows about, for services list's per-row
// annotation pass. It only annotates rows telemetry already discovered —
// see annotateServicesFromKG — never adding a row for a KG entity that has
// no matching telemetry-derived Service; synthesizing one would mean
// fabricating a RED-snapshot placeholder for a row nobody asked for.
// Returns an empty (non-nil) map under the same best-effort rules as
// lookup. Only the entity search's first page is fetched — a stack with
// more Service entities than fit on one page will only get partial
// annotation coverage; this is a documented limitation, not a silent one.
func (c *kgCatalog) index(ctx context.Context) map[string]*KGRef {
	out := map[string]*KGRef{}
	active, err := c.client.Active(ctx)
	if err != nil || !active {
		return out
	}
	page, err := c.client.ListEntities(ctx, "Service", kgquery.EntityScope{}, 0, 0, 0)
	if err != nil {
		return out
	}
	for _, e := range page.Entities {
		out[e.Name] = &KGRef{Known: true, EntityType: e.Type, Scope: e.Scope}
	}
	return out
}

// annotateServicesFromKG sets Service.KG from idx for each item with a
// matching bare-name entry, leaving items with no match untouched (nil).
// idx entries with no matching item are never reflected in the result — the
// Knowledge Graph annotates telemetry-discovered rows, it doesn't add rows
// of its own.
func annotateServicesFromKG(items []Service, idx map[string]*KGRef) []Service {
	for i := range items {
		items[i].KG = idx[items[i].Name]
	}
	return items
}
