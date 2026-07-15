package remote

import (
	"context"
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

// errDryRunUnverified is returned when the guard blocks a dry-run against a resource that
// does not honor server-side dryRun. It is the same sentinel provider adapters return for a
// dry-run they cannot validate (adapter.ErrDryRunUnverified), so the pusher and deleter
// record both paths as skipped, not as a success or failure.
var errDryRunUnverified = adapter.ErrDryRunUnverified

// GuardConfig configures the dry-run safety guard applied to a Pusher or Deleter.
type GuardConfig struct {
	// AssumeServerDryRun augments the built-in dry-run allowlist with user-asserted
	// GroupResource strings ("<resource>.<group>").
	AssumeServerDryRun []string
	// Warn is where guard warnings are written (typically stderr). A nil writer suppresses
	// warnings but keeps the fail-safe blocking.
	Warn io.Writer
}

// newGuardedDynamicClient wraps inner with the dry-run guard. Malformed user-asserted values
// are ignored with a warning rather than failing, so a bad flag or config entry never blocks
// the operation.
func newGuardedDynamicClient(inner adapter.DynamicClient, cfg GuardConfig) adapter.DynamicClient {
	allowlist, invalid := newDryRunAllowlist(cfg.AssumeServerDryRun)
	if len(invalid) > 0 && cfg.Warn != nil {
		output.Warning(cfg.Warn, "ignoring invalid assume-server-dry-run value(s) %v: expected <resource>.<group>, e.g. alertrules.rules.alerting.grafana.app", invalid)
	}
	return newDryRunGuard(inner, allowlist, cfg.Warn)
}

// dryRunGuard wraps a DynamicClient to make --dry-run fail safe. For a dry-run mutation
// against a resource not known to honor server-side dryRun, it skips the request (which a
// legacy backend would otherwise apply for real) after a best-effort client-side check and
// returns errDryRunUnverified. Everything else (reads, real mutations, and dry-runs of
// allowlisted resources) passes straight through. Only the dynamic fallback is wrapped;
// provider adapters never mutate on dry-run and return adapter.ErrDryRunUnverified
// themselves when they have no way to validate.
//
// inner is a named field and the read methods are written out by hand, instead of embedding
// adapter.DynamicClient (which would auto-forward every method). That is deliberate: if the
// interface gains a new mutating method, this file should fail to compile rather than forward
// it to the server past the guard.
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

// blockDryRun reports whether a dry-run mutation must be blocked (the resource does not honor
// server-side dryRun). Non-dry-run calls are never blocked. It emits the one-time stderr
// warning or user-asserted note as a side effect.
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

// What a blocked dry-run actually checked client-side, per verb, so the warning says what
// gcx verified rather than only what it skipped. Push covers both create and update.
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
		// Best-effort existence check; never sends the Delete a legacy backend would apply
		// for real. A missing target is still reported as skipped (nothing to delete).
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

// announceOnce runs emit the first time a GroupResource is seen this run, so a bulk operation
// warns once per resource type instead of once per item.
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
