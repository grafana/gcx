package services

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	kgquery "github.com/grafana/gcx/internal/query/kg"
	"github.com/prometheus/common/model"
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
	flags.StringVar(&f.Mode, "kg", string(kgModeAuto), "Knowledge Graph catalog consumption: auto (annotate rows with what the graph knows, when it's active) or off (never contact the Knowledge Graph). The annotation appears in JSON/YAML/agents output only — table and wide render nothing extra")
}

func (f *kgFlags) resolve() (kgMode, error) { return resolveKGMode(f.Mode) }

// catalog builds the kgCatalog for f's --kg value. Every call site runs
// after opts.Validate, which already calls resolve and fails the command on
// a bad value — by the time catalog runs, the mode is known good, so unlike
// an earlier version of this method it does not re-surface that error.
func (f *kgFlags) catalog(cfg config.NamespacedRESTConfig) *kgCatalog {
	mode, _ := f.resolve()
	return newKGCatalog(cfg, mode)
}

// KGRef records what the Knowledge Graph knows about a service. Present
// only when --kg is auto AND the graph was reachable, active, and knew
// about this service — pointer + omitempty so JSON on any other path (off,
// inactive, unreachable, unknown service) stays byte-identical to before
// this field existed. There is no separate "known" flag: the presence of a
// non-nil *KGRef already says the graph knew this service, and a boolean
// that can only ever be true would just be a second, redundant way to say
// the same thing in every response this package emits.
type KGRef struct {
	EntityType string            `json:"entity_type,omitempty" yaml:"entity_type,omitempty"`
	Scope      map[string]string `json:"scope,omitempty" yaml:"scope,omitempty"`
}

// windowMs converts a `--since`-style PromQL duration into the [startMs,
// endMs) window kgquery's LookupEntity/ListEntities expect, ending now. This
// is what lets a graph lookup cover the same time range the command's own
// telemetry query does, instead of silently defaulting to kgquery's last
// hour regardless of --since. Falls back to (0, 0) — kgquery's own
// last-hour default — if since somehow isn't a valid PromQL duration;
// callers validate their own --since already, so this is defense in depth.
func windowMs(since string) (int64, int64) {
	d, err := model.ParseDuration(since)
	if err != nil {
		return 0, 0
	}
	end := time.Now()
	return end.Add(-time.Duration(d)).UnixMilli(), end.UnixMilli()
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

// lookupResult is what lookup actually observed, kept distinct from the
// simpler *KGRef the call sites want so a genuine negative ("the graph
// doesn't know this service") can be told apart from an inconclusive one
// ("the graph or transport failed") — see lookupInconclusive.
type lookupResult struct {
	ref             *KGRef
	inconclusive    bool
	inconclusiveErr error
}

// lookupVerbose resolves one service by bare name and reports the
// inconclusive/error detail that best-effort callers may want to surface as
// a diagnostic (e.g. a stderr hint) instead of silently discarding — an auth
// failure, a 5xx from the Asserts plugin, or a transport error currently
// looks identical to "the graph genuinely doesn't know this service," which
// makes a broken token indistinguishable from a true negative.
//
// startMs/endMs bound the window the graph considers "recent activity";
// pass the command's own --since window (via windowMs) so a service with
// telemetry in the requested window but no graph activity in
// kgquery's default last-hour window doesn't read as "the graph doesn't
// know this" for a --since 1d run.
//
// LookupEntity requires an exact (type, name, scope) match; scope is often
// unknown to a telemetry-derived row (Service.Namespace and the graph's own
// "namespace" scope dimension are different things — see the package doc),
// so a scope-less LookupEntity misses any entity the graph only knows under
// a specific scope. Falls back to a name-exact scan of ListEntities, the
// same two-step discoverEntityScope in internal/providers/kg uses for the
// identical ambiguity.
func (c *kgCatalog) lookupVerbose(ctx context.Context, name string, startMs, endMs int64) lookupResult {
	active, err := c.client.Active(ctx)
	if err != nil {
		return lookupResult{inconclusive: true, inconclusiveErr: err}
	}
	if !active {
		return lookupResult{}
	}
	entity, err := c.client.LookupEntity(ctx, "Service", name, nil, "", startMs, endMs)
	if err != nil {
		return lookupResult{inconclusive: true, inconclusiveErr: err}
	}
	if entity != nil {
		return lookupResult{ref: &KGRef{EntityType: entity.Type, Scope: entity.Scope}}
	}
	page, err := c.client.ListEntities(ctx, "Service", kgquery.EntityScope{}, startMs, endMs, 0)
	if err != nil {
		return lookupResult{inconclusive: true, inconclusiveErr: err}
	}
	for _, e := range page.Entities {
		if e.Name == name {
			return lookupResult{ref: &KGRef{EntityType: e.Type, Scope: e.Scope}}
		}
	}
	return lookupResult{}
}

// indexResult is index's return value: the annotation map plus the
// best-effort diagnostics a caller may want to surface — an inconclusive
// Active() check/list failure, or a first-page result that didn't cover
// every Service entity the graph has.
type indexResult struct {
	idx             map[string]*KGRef
	inconclusive    bool
	inconclusiveErr error
	truncated       bool
}

// index builds a bare-name -> *KGRef map for every Service-type entity the
// Knowledge Graph currently knows about, for services list's per-row
// annotation pass. It only annotates rows telemetry already discovered —
// see annotateServicesFromKG — never adding a row for a KG entity that has
// no matching telemetry-derived Service; synthesizing one would mean
// fabricating a RED-snapshot placeholder for a row nobody asked for. Only
// the entity search's first page is fetched; indexResult.truncated reports
// when that page didn't cover every Service entity, so a caller can warn
// instead of silently under-annotating. startMs/endMs are windowMs's
// translation of the command's own --since window — see lookupVerbose's
// doc comment for why that matters. services list (which has no --since)
// always passes (0, 0); "gcx appo11y operations list" passes a real
// windowMs(opts.Since) window.
func (c *kgCatalog) index(ctx context.Context, startMs, endMs int64) indexResult {
	out := map[string]*KGRef{}
	active, err := c.client.Active(ctx)
	if err != nil {
		return indexResult{idx: out, inconclusive: true, inconclusiveErr: err}
	}
	if !active {
		return indexResult{idx: out}
	}
	page, err := c.client.ListEntities(ctx, "Service", kgquery.EntityScope{}, startMs, endMs, 0)
	if err != nil {
		return indexResult{idx: out, inconclusive: true, inconclusiveErr: err}
	}
	for _, e := range page.Entities {
		out[e.Name] = &KGRef{EntityType: e.Type, Scope: e.Scope}
	}
	truncated := page.MaxLimitHit || (!page.LastPage && len(page.Entities) > 0)
	return indexResult{idx: out, truncated: truncated}
}

// warnKGInconclusive surfaces a best-effort Knowledge Graph lookup/index
// failure (auth, 5xx, transport) as a stderr warning instead of letting it
// collapse silently into the same shape as "the graph genuinely doesn't
// know this" — see lookupResult.inconclusive / indexResult.inconclusive.
func warnKGInconclusive(stderr io.Writer, err error) {
	cmdio.EmitWarn(stderr, fmt.Sprintf("Knowledge Graph lookup failed, service rows won't be annotated: %v", err))
}

// warnKGTruncated surfaces indexResult.truncated as a stderr hint: the
// entity search's first page didn't cover every Service entity the graph
// has, so annotation coverage on this run may be incomplete.
func warnKGTruncated(stderr io.Writer) {
	cmdio.EmitHint(stderr,
		"Knowledge Graph has more services than fit on one page — some rows may be missing their kg annotation",
		"")
}

// warnKGLookup is the single-call pattern every per-record lookup call site
// needs: warn on an inconclusive lookupVerbose result, then hand back the
// ref either way. Collapses the two-branch inline pattern (warn, then
// assign) down to one expression at each call site.
func warnKGLookup(stderr io.Writer, lr lookupResult) *KGRef {
	if lr.inconclusive {
		warnKGInconclusive(stderr, lr.inconclusiveErr)
	}
	return lr.ref
}

// warnKGIndex is warnKGLookup's counterpart for the bulk index() call
// sites: warn on an inconclusive or truncated result, then hand back the
// annotation map either way.
func warnKGIndex(stderr io.Writer, result indexResult) map[string]*KGRef {
	if result.inconclusive {
		warnKGInconclusive(stderr, result.inconclusiveErr)
	} else if result.truncated {
		warnKGTruncated(stderr)
	}
	return result.idx
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
