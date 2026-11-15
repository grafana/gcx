package observation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/remote"
	"golang.org/x/sync/errgroup"
)

// DefaultConcurrency bounds the number of resource types read in parallel.
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
			if err == nil {
				err = errors.New("no matching resource type")
			}
			if seen[raw] {
				continue
			}
			seen[raw] = true
			out = append(out, Selection{Selector: raw, Type: raw, Err: err})
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
	Stack       StackIdentity
	Selectors   []string
	Selections  []Selection
	Puller      Puller
	GCXVersion  string
	Concurrency int
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
	limit := req.Concurrency
	if limit <= 0 {
		limit = DefaultConcurrency
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
			Selectors:            append([]string(nil), selectors...),
			StartedAt:            now().UTC(),
			Inventory:            []InventoryEntry{},
		},
		Objects: map[string]map[string]map[string]any{},
	}

	coverage := make([]TypeCoverage, len(req.Selections))
	objects := make([]map[string]map[string]any, len(req.Selections))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
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

func collectType(ctx context.Context, puller Puller, sel Selection) (TypeCoverage, map[string]map[string]any, error) {
	cov := TypeCoverage{Type: sel.Type, Selector: sel.Selector}
	cov.Version = sel.Filter.Descriptor.GroupVersion.Version
	cov.Kind = sel.Filter.Descriptor.Kind

	res := resources.NewResources()
	summary, err := puller.Pull(ctx, remote.PullRequest{
		Filters:   resources.Filters{sel.Filter},
		Resources: res,
		// Observation must see everything the caller can read: nothing is
		// excluded by manager or read-only state.
		ExcludeManaged:  false,
		ExcludeReadOnly: false,
		StopOnError:     false,
	})
	if ctx.Err() != nil {
		return cov, nil, ctx.Err()
	}

	var reasons []string
	if err != nil {
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

	objs := map[string]map[string]any{}
	if err == nil {
		for _, r := range res.AsList() {
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
