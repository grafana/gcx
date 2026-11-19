package observation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/remote"
	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// DefaultConcurrency bounds the number of resource types, and of individual
// item reads within a type, read in parallel.
const DefaultConcurrency = 10

// Capture is a collected observation: its manifest (without inventory until
// written) and its normalized objects by type key and name.
type Capture struct {
	Manifest Manifest
	Objects  map[string]map[string]map[string]any
}

// Selection is one resolved (or unresolvable) resource type in the scope.
type Selection struct {
	Selector string
	Type     string
	Filter   resources.Filter
	// Err is set when the selector could not be resolved on the stack.
	Err error
}

// UnsupportedSelectorError reports a selector that cannot be captured.
type UnsupportedSelectorError struct {
	Selector string
	Reason   string
}

func (e *UnsupportedSelectorError) Error() string {
	return fmt.Sprintf("cannot capture %q: %s", e.Selector, e.Reason)
}

// FilterMaker resolves selectors into filters (implemented by
// discovery.Registry).
type FilterMaker interface {
	MakeFilters(opts discovery.MakeFiltersOptions) (resources.Filters, error)
}

// TypeKey returns the plural.group key of a descriptor.
func TypeKey(desc resources.Descriptor) string {
	return desc.Plural + "." + desc.GroupVersion.Group
}

// ResolveSelections resolves the capture scope. Without selectors the scope is
// every supported type. Named selectors (type/name) and unsupported types are
// rejected with an *UnsupportedSelectorError. A selector that does not resolve
// on the stack becomes an unavailable selection, so the gap is recorded rather
// than hidden.
func ResolveSelections(maker FilterMaker, selectors []string, warn io.Writer) ([]Selection, error) {
	if len(selectors) == 0 {
		selectors = SupportedTypes()
	}

	var out []Selection
	seen := map[string]bool{}
	for _, raw := range selectors {
		sels, err := resources.ParseSelectors([]string{raw})
		if err != nil {
			return nil, err
		}
		if sels.HasNamedSelectorsOnly() {
			return nil, &UnsupportedSelectorError{Selector: raw, Reason: "capture selects whole resource types; remove the resource names"}
		}

		filters, err := maker.MakeFilters(discovery.MakeFiltersOptions{
			Selectors:            sels,
			Warn:                 warn,
			PreferredVersionOnly: true,
		})
		if err != nil || len(filters) == 0 {
			// Only a supported type that the stack does not serve is a
			// coverage gap. Anything else is a mistyped or unknown type.
			key := supportedTypeFor(sels[0].GroupVersionKind)
			if key == "" {
				return nil, &UnsupportedSelectorError{
					Selector: raw,
					Reason:   "unknown resource type; supported types: " + strings.Join(SupportedTypes(), ", "),
				}
			}
			if err == nil {
				err = errors.New("no matching resource type")
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Selection{Selector: raw, Type: key, Err: err})
			continue
		}

		for _, filter := range filters {
			key := TypeKey(filter.Descriptor)
			if !IsSupportedType(key) {
				return nil, &UnsupportedSelectorError{
					Selector: raw,
					Reason:   fmt.Sprintf("resource type %s is not supported; supported types: %s", key, strings.Join(SupportedTypes(), ", ")),
				}
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Selection{Selector: raw, Type: key, Filter: filter})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out, nil
}

// Puller reads resources (implemented by remote.Puller).
type Puller interface {
	Pull(ctx context.Context, req remote.PullRequest) (*remote.OperationSummary, error)
}

// CollectRequest describes a capture.
type CollectRequest struct {
	Stack      StackIdentity
	Selectors  []string
	Selections []Selection
	Puller     Puller
	// Access is the caller's identity and permissions (see ReadAccess).
	Access     Access
	GCXVersion string
	// Trash, when set, also records the dashboard trash as its own view
	// whenever dashboards are in the scope (see TypeDashboardTrash).
	Trash TrashReader
	// Permissions, when set, also records the stored permission entries of
	// every captured dashboard and folder as their own views.
	Permissions PermissionReader
	// Now returns the current time; defaults to time.Now.
	Now func() time.Time
}

// Collect reads every selection and returns the normalized capture. Read
// problems for a type are recorded as incomplete coverage. Cancellation of
// ctx aborts the capture with an error so that no artifact is written.
func Collect(ctx context.Context, req CollectRequest) (*Capture, error) {
	now := req.Now
	if now == nil {
		now = time.Now
	}

	selectors := req.Selectors
	if len(selectors) == 0 {
		selectors = SupportedTypes()
	}

	capture := &Capture{
		Manifest: Manifest{
			Type:                 ManifestType,
			SchemaVersion:        SchemaVersion,
			NormalizationVersion: NormalizationVersion,
			GCXVersion:           req.GCXVersion,
			Stack:                req.Stack,
			Caller:               &CallerIdentity{UID: req.Access.Identity.UID, Login: req.Access.Identity.Login},
			Selectors:            append([]string(nil), selectors...),
			StartedAt:            now().UTC(),
			Inventory:            []InventoryEntry{},
		},
		Objects: map[string]map[string]map[string]any{},
	}

	coverage := make([]TypeCoverage, len(req.Selections))
	objects := make([]map[string]map[string]any, len(req.Selections))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(DefaultConcurrency)
	for idx, sel := range req.Selections {
		if sel.Err != nil {
			coverage[idx] = TypeCoverage{
				Type:     sel.Type,
				Selector: sel.Selector,
				Status:   CoverageUnavailable,
				Reasons:  []string{sel.Err.Error()},
			}
			continue
		}
		g.Go(func() error {
			cov, objs, err := collectType(gctx, req.Puller, sel)
			if err != nil {
				return err
			}
			applyReadScope(&cov, req.Access)
			coverage[idx] = cov
			objects[idx] = objs
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	trashCov, trashObjs, err := collectTrash(ctx, req)
	if err != nil {
		return nil, err
	}
	permCov, permObjs, err := collectPermissions(ctx, req, coverage, objects)
	if err != nil {
		return nil, err
	}
	coverage = slices.Concat(coverage, trashCov, permCov)
	objects = slices.Concat(objects, trashObjs, permObjs)

	for idx, cov := range coverage {
		capture.Manifest.Types = append(capture.Manifest.Types, cov)
		if objects[idx] != nil {
			capture.Objects[cov.Type] = objects[idx]
		}
	}
	sort.Slice(capture.Manifest.Types, func(i, j int) bool {
		return capture.Manifest.Types[i].Type < capture.Manifest.Types[j].Type
	})
	capture.Manifest.FinishedAt = now().UTC()

	return capture, nil
}

// applyReadScope records the caller's read scope for a collected type. A
// caller that cannot read every resource sees a filtered list without an
// error, so the type is incomplete unless the scope is all.
func applyReadScope(cov *TypeCoverage, access Access) {
	applyReadScopeOf(cov, cov.Type, access)
}

// applyReadScopeOf records the read scope of typeKey on cov, which is read
// with that type's permission.
func applyReadScopeOf(cov *TypeCoverage, typeKey string, access Access) {
	scope, reason := access.ReadScope(typeKey)
	cov.ReadScope = scope
	if scope == ReadScopeAll {
		return
	}
	cov.Status = CoverageIncomplete
	cov.Reasons = append(cov.Reasons, reason)
	sort.Strings(cov.Reasons)
}

// collectTrash records the dashboard trash as its own view, once for each
// dashboards selection that resolved. It is a no-op without a trash reader.
func collectTrash(ctx context.Context, req CollectRequest) ([]TypeCoverage, []map[string]map[string]any, error) {
	var coverage []TypeCoverage
	var objects []map[string]map[string]any
	if req.Trash == nil {
		return nil, nil, nil
	}
	for _, sel := range req.Selections {
		if sel.Type != TypeDashboards || sel.Err != nil {
			continue
		}
		cov, objs, err := req.Trash(ctx, sel.Filter.Descriptor.GroupVersion.Version)
		if err != nil {
			return nil, nil, err
		}
		if cov.Status != CoverageUnavailable {
			// The trash is read with the dashboards read permission; a caller
			// that cannot read every dashboard cannot see the whole trash.
			applyReadScopeOf(&cov, TypeDashboards, req.Access)
		}
		coverage = append(coverage, cov)
		objects = append(objects, objs)
	}
	return coverage, objects, nil
}

// collectPermissions records the stored permission entries of the captured
// dashboards and folders, one view for each type that was read. It is a no-op
// without a permission reader.
func collectPermissions(ctx context.Context, req CollectRequest, parents []TypeCoverage, parentObjects []map[string]map[string]any) ([]TypeCoverage, []map[string]map[string]any, error) {
	var coverage []TypeCoverage
	var objects []map[string]map[string]any
	if req.Permissions == nil {
		return nil, nil, nil
	}
	for idx, parent := range parents {
		if _, ok := permissionViews[parent.Type]; !ok || parent.Status == CoverageUnavailable {
			continue
		}
		names := make([]string, 0, len(parentObjects[idx]))
		for name := range parentObjects[idx] {
			names = append(names, name)
		}
		sort.Strings(names)
		cov, objs, err := req.Permissions(ctx, parent.Type, names)
		if err != nil {
			return nil, nil, err
		}
		// Permissions are read for the captured resources only, so an
		// incomplete parent leaves the view incomplete too.
		if parent.Status != CoverageComplete {
			cov.Status = CoverageIncomplete
			cov.Reasons = append(cov.Reasons, "the "+parent.Type+" capture is incomplete")
		}
		applyReadScope(&cov, req.Access)
		coverage = append(coverage, cov)
		objects = append(objects, objs)
	}
	return coverage, objects, nil
}

func collectType(ctx context.Context, puller Puller, sel Selection) (TypeCoverage, map[string]map[string]any, error) {
	cov := TypeCoverage{Type: sel.Type, Selector: sel.Selector}
	cov.Version = sel.Filter.Descriptor.GroupVersion.Version
	cov.Kind = sel.Filter.Descriptor.Kind

	listed, reasons, err := pullFilter(ctx, puller, sel.Filter)
	if err != nil {
		return cov, nil, err
	}

	// Some list endpoints return summaries rather than full objects. For
	// those types, every listed item is read again individually, also when
	// the list itself is incomplete: a summary must never be stored as if it
	// were the full object, because it would compare as modified.
	if itemReadTypes[sel.Type] && len(listed) > 0 {
		var itemReasons []string
		listed, itemReasons, err = readItems(ctx, puller, sel.Filter.Descriptor, listed)
		if err != nil {
			return cov, nil, err
		}
		reasons = append(reasons, itemReasons...)
	}

	objs := map[string]map[string]any{}
	for _, r := range listed {
		name := r.Name()
		if _, dup := objs[name]; dup {
			reasons = append(reasons, "duplicate resource name: "+name)
			continue
		}
		norm, nerr := Normalize(sel.Type, r.Object.Object)
		if nerr != nil {
			reasons = append(reasons, "normalize failed: "+name+": "+nerr.Error())
			continue
		}
		objs[name] = norm
	}

	cov.Count = len(objs)
	if len(reasons) == 0 {
		cov.Status = CoverageComplete
	} else {
		sort.Strings(reasons)
		cov.Status = CoverageIncomplete
		cov.Reasons = reasons
	}
	return cov, objs, nil
}

// readItems reads each listed resource on its own, so that one failed read
// costs only that resource. A resource that is gone by the time it is read
// was deleted during the capture: it is omitted without making the type
// incomplete, because the capture observed its absence. Any other failure
// omits the resource and records a reason. The error is set only when ctx
// is done.
func readItems(ctx context.Context, puller Puller, desc resources.Descriptor, listed []*resources.Resource) ([]*resources.Resource, []string, error) {
	read := make([]*resources.Resource, len(listed))
	reasons := make([]string, len(listed))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(DefaultConcurrency)
	for idx, item := range listed {
		g.Go(func() error {
			r, reason, err := readItem(gctx, puller, desc, item.Name())
			read[idx], reasons[idx] = r, reason
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	var outR []*resources.Resource
	var outReasons []string
	for idx := range listed {
		if read[idx] != nil {
			outR = append(outR, read[idx])
		}
		if reasons[idx] != "" {
			outReasons = append(outReasons, reasons[idx])
		}
	}
	return outR, outReasons, nil
}

// readItem reads one resource by name. It returns the resource, or nil and
// the reason that it could not be read; both are empty for a resource that
// no longer exists.
func readItem(ctx context.Context, puller Puller, desc resources.Descriptor, name string) (*resources.Resource, string, error) {
	res := resources.NewResources()
	summary, err := puller.Pull(ctx, remote.PullRequest{
		Filters:   resources.Filters{{Type: resources.FilterTypeSingle, Descriptor: desc, ResourceUIDs: []string{name}}},
		Resources: res,
		// As in pullFilter, nothing is excluded by manager or read-only state.
		ExcludeManaged:  false,
		ExcludeReadOnly: false,
		StopOnError:     false,
	})
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	if err != nil {
		// A read error is a coverage reason, not a capture failure.
		return nil, "read failed: " + name + ": " + err.Error(), nil //nolint:nilerr // see above
	}
	if reason, gone := itemReadReason(name, summary); reason != "" || gone {
		return nil, reason, nil
	}
	items := res.AsList()
	if len(items) != 1 {
		return nil, fmt.Sprintf("read failed: %s: expected one object, got %d", name, len(items)), nil
	}
	return items[0], "", nil
}

// itemReadReason returns why a single-item read did not produce the item,
// or gone when the item no longer exists (a NotFound failure): it was deleted
// during the capture. Both are empty when the read succeeded.
func itemReadReason(name string, summary *remote.OperationSummary) (string, bool) {
	if summary == nil {
		return "", false
	}
	if failures := summary.Failures(); len(failures) > 0 {
		failure := failures[0]
		if apierrors.IsNotFound(failure.Error) {
			return "", true
		}
		msg := "unknown error"
		if failure.Error != nil {
			msg = failure.Error.Error()
		}
		return "read failed: " + name + ": " + msg, false
	}
	skipped := summary.SkippedReasons()
	if len(skipped) == 0 {
		return "", false
	}
	keys := make([]string, 0, len(skipped))
	for reason := range skipped {
		keys = append(keys, reason)
	}
	sort.Strings(keys)
	return "skipped: " + name + ": " + strings.Join(keys, ", "), false
}

// pullFilter reads one filter. It returns the resources read and the reasons
// that make the read incomplete. The error is set only when ctx is done.
// Nothing is returned for a read that failed as a whole.
func pullFilter(ctx context.Context, puller Puller, filter resources.Filter) ([]*resources.Resource, []string, error) {
	res := resources.NewResources()
	summary, err := puller.Pull(ctx, remote.PullRequest{
		Filters:   resources.Filters{filter},
		Resources: res,
		// Observation must see everything the caller can read: nothing is
		// excluded by manager or read-only state.
		ExcludeManaged:  false,
		ExcludeReadOnly: false,
		StopOnError:     false,
	})
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	var reasons []string
	failed := err != nil
	if failed {
		reasons = append(reasons, "read failed: "+err.Error())
	}
	if summary != nil {
		for _, failure := range summary.Failures() {
			msg := "unknown error"
			if failure.Error != nil {
				msg = failure.Error.Error()
			}
			if failure.Resource != nil {
				msg = failure.Resource.Name() + ": " + msg
			}
			reasons = append(reasons, "read failed: "+msg)
		}
		skipped := summary.SkippedReasons()
		skipKeys := make([]string, 0, len(skipped))
		for reason := range skipped {
			skipKeys = append(skipKeys, reason)
		}
		sort.Strings(skipKeys)
		for _, reason := range skipKeys {
			reasons = append(reasons, fmt.Sprintf("skipped: %s (%d)", reason, skipped[reason]))
		}
		if summary.IsTruncated() {
			reasons = append(reasons, "list truncated")
		}
	}
	// A read error is recorded as a reason: it makes the type incomplete
	// rather than failing the capture, and none of its items are kept.
	var read []*resources.Resource
	if !failed {
		read = res.AsList()
	}
	return read, reasons, nil //nolint:nilerr // a read error is a coverage reason, not a capture failure
}

// ServerIdentity returns the server URL with user information removed.
func ServerIdentity(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}
