package remote

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"

	"github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// errDryRunUnverified is a sentinel returned by the dry-run guard when a mutating dry-run
// request targets a resource that does not honor server-side dryRun. The pusher and deleter
// recognise it and record the resource as skipped (neither a failure nor a success), so gcx
// never claims it pushed or deleted something it deliberately did not send.
var errDryRunUnverified = errors.New("server-side dry-run not supported; checked client-side only (not verified)")

// GuardConfig configures the dry-run safety guard applied to a Pusher or Deleter.
type GuardConfig struct {
	// AssumeServerDryRun augments the built-in dry-run allowlist with user-asserted
	// GroupResource strings ("<resource>.<group>").
	AssumeServerDryRun []string
	// Warn is where guard warnings are written (typically stderr). A nil writer suppresses
	// warnings but keeps the fail-safe blocking.
	Warn io.Writer
}

// newGuardedDynamicClient wraps inner with the dry-run guard configured by cfg. Malformed
// user-asserted GroupResource values are ignored (with a warning) rather than failing, so a
// bad --assume-server-dry-run value or persisted config entry never blocks the operation.
func newGuardedDynamicClient(inner adapter.DynamicClient, cfg GuardConfig) adapter.DynamicClient {
	allowlist, invalid := newDryRunAllowlist(cfg.AssumeServerDryRun)
	if len(invalid) > 0 && cfg.Warn != nil {
		output.Warning(cfg.Warn, "ignoring invalid assume-server-dry-run value(s) %v: expected <resource>.<group>, e.g. alertrules.rules.alerting.grafana.app", invalid)
	}
	return newDryRunGuard(inner, allowlist, cfg.Warn)
}

// dryRunGuard decorates a DynamicClient to make --dry-run fail safe. For mutating verbs
// carrying DryRun against a resource NOT known to honor server-side dryRun, it refuses to
// send the request (which a legacy storage bridge would otherwise apply for real) and returns
// errDryRunUnverified after a best-effort client-side check. All other calls (reads,
// non-dry-run mutations, and dry-run mutations against allowlisted resources) pass straight
// through. Only the dynamic fallback is wrapped, so the provider-adapter path (already
// dry-run-safe via typedAdapter) is untouched.
type dryRunGuard struct {
	inner     adapter.DynamicClient
	allowlist dryRunAllowlist
	warn      io.Writer

	mu        sync.Mutex
	announced map[schema.GroupResource]struct{} // dedupe: one stderr note per GroupResource per run
}

func newDryRunGuard(inner adapter.DynamicClient, allowlist dryRunAllowlist, warn io.Writer) *dryRunGuard {
	return &dryRunGuard{
		inner:     inner,
		allowlist: allowlist,
		warn:      warn,
		announced: make(map[schema.GroupResource]struct{}),
	}
}

// blockDryRun reports whether a mutating call must be blocked because it is a dry-run against
// a resource that does not honor server-side dryRun. Non-dry-run calls are never blocked. It
// emits the one-time stderr warning (blocked) or note (user-asserted) as a side effect.
func (g *dryRunGuard) blockDryRun(desc resources.Descriptor, dryRun []string, checkedDetail string) bool {
	if !slices.Contains(dryRun, metav1.DryRunAll) {
		return false
	}
	gr := schema.GroupResource{Group: desc.GroupVersion.Group, Resource: desc.Plural}
	honored, static := g.allowlist.classify(gr)
	if !honored {
		g.warnBlocked(gr, checkedDetail)
		return true
	}
	if !static {
		g.noteAsserted(gr) // honored only because the user asserted it
	}
	return false
}

// Verb-specific descriptions of the client-side checks a blocked dry-run actually ran,
// so the warning states what gcx verified rather than only what it skipped. Push covers
// both create and update (the pusher determines which and runs the same checks).
const (
	pushDryRunChecks = "gcx checked client-side only: the manifest parses and the kind is served by this API. " +
		"It did not validate the spec, references, or uniqueness. No changes were sent."
	deleteDryRunChecks = "gcx checked client-side only whether the target exists. No delete was sent."
)

func (g *dryRunGuard) Create(
	ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions,
) (*unstructured.Unstructured, error) {
	if g.blockDryRun(desc, opts.DryRun, pushDryRunChecks) {
		return nil, errDryRunUnverified
	}
	return g.inner.Create(ctx, desc, obj, opts)
}

func (g *dryRunGuard) Update(
	ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	if g.blockDryRun(desc, opts.DryRun, pushDryRunChecks) {
		return nil, errDryRunUnverified
	}
	return g.inner.Update(ctx, desc, obj, opts)
}

func (g *dryRunGuard) Delete(ctx context.Context, desc resources.Descriptor, name string, opts metav1.DeleteOptions) error {
	if g.blockDryRun(desc, opts.DryRun, deleteDryRunChecks) {
		// Best-effort existence check: this never sends the Delete a legacy bridge would
		// otherwise apply. A NotFound is still reported as skipped (nothing to delete).
		if _, err := g.inner.Get(ctx, desc, name, metav1.GetOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return errDryRunUnverified
	}
	return g.inner.Delete(ctx, desc, name, opts)
}

// Read verbs always pass through.

func (g *dryRunGuard) Get(
	ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions,
) (*unstructured.Unstructured, error) {
	return g.inner.Get(ctx, desc, name, opts)
}

func (g *dryRunGuard) GetMultiple(
	ctx context.Context, desc resources.Descriptor, names []string, opts metav1.GetOptions,
) ([]unstructured.Unstructured, error) {
	return g.inner.GetMultiple(ctx, desc, names, opts)
}

func (g *dryRunGuard) List(
	ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	return g.inner.List(ctx, desc, opts)
}

func (g *dryRunGuard) warnBlocked(gr schema.GroupResource, checkedDetail string) {
	g.announceOnce(gr, func(w io.Writer) {
		output.Warning(w, "%s does not support server-side dry-run. %s", gr.String(), checkedDetail)
	})
}

func (g *dryRunGuard) noteAsserted(gr schema.GroupResource) {
	g.announceOnce(gr, func(w io.Writer) {
		output.Info(w, "%s: server-side dry-run assumed supported by user assertion (--assume-server-dry-run / config).", gr.String())
	})
}

// announceOnce writes emit(warn) the first time a GroupResource is seen in this run, so a
// bulk operation warns once per distinct resource rather than once per item.
func (g *dryRunGuard) announceOnce(gr schema.GroupResource, emit func(io.Writer)) {
	if g.warn == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.announced[gr]; ok {
		return
	}
	g.announced[gr] = struct{}{}
	emit(g.warn)
}
