package watcher

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const pushPlanAnnotation = WatcherAPIGroup + "/push-plan"

type pushPlan struct {
	target Candidate
	index  map[string][]Candidate
}
type validationWarningsError struct{ values []string }

func (e validationWarningsError) Error() string               { return adapter.ErrDryRunUnverified.Error() }
func (e validationWarningsError) Unwrap() error               { return adapter.ErrDryRunUnverified }
func (e validationWarningsError) OperationWarnings() []string { return e.values }

func (a *manifestAdapter) NewPushPreflight(ctx context.Context, selections []adapter.PushSelection) (adapter.PushPreflight, error) {
	index, err := IdentityIndex(ctx, a.client)
	if err != nil {
		return nil, err
	}
	failures := make(map[*resources.Resource]error)
	groups := make(map[string][]*resources.Resource)
	targets := make(map[*resources.Resource]Candidate)
	for _, selection := range selections {
		for _, res := range selection.Items {
			if res.GroupVersionKind() != WatcherDescriptor().GroupVersionKind() {
				continue
			}
			obj := res.ToUnstructured()
			w, err := DecodeManifest(&obj)
			if err != nil {
				failures[res] = err
				continue
			}
			target, err := selectUpsert(index, *w, false)
			if err != nil {
				failures[res] = err
				continue
			}
			targets[res] = target
			if target.ID == "" || target.Title != w.Title {
				groups["title:"+w.Title] = append(groups["title:"+w.Title], res)
			}
			if target.ID != "" {
				groups["id:"+target.ID] = append(groups["id:"+target.ID], res)
			}
		}
	}
	for key, items := range groups {
		if len(items) > 1 {
			for _, res := range items {
				failures[res] = fmt.Errorf("multiple Watcher manifests select the same target (%s); none of these manifests were applied", key)
			}
		}
	}
	return func(_ context.Context, res *resources.Resource) error {
		if res.GroupVersionKind() != WatcherDescriptor().GroupVersionKind() {
			return nil
		}
		if err := failures[res]; err != nil {
			return err
		}
		token := uuid.NewString()
		a.planMu.Lock()
		if a.plans == nil {
			a.plans = make(map[string]pushPlan)
		}
		a.plans[token] = pushPlan{target: targets[res], index: index}
		a.planMu.Unlock()
		annotations := res.Object.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[pushPlanAnnotation] = token
		res.Object.SetAnnotations(annotations)
		return nil
	}, nil
}
func (a *manifestAdapter) PushEnvelope(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions) error {
	w, err := DecodeManifest(obj)
	if err != nil {
		return err
	}
	if len(opts.DryRun) > 0 {
		return validationWarningsError{values: w.Warnings()}
	}
	token := obj.GetAnnotations()[pushPlanAnnotation]
	a.planMu.Lock()
	plan, planned := a.plans[token]
	delete(a.plans, token)
	a.planMu.Unlock()
	if !planned {
		index, err := IdentityIndex(ctx, a.client)
		if err != nil {
			return err
		}
		target, err := selectUpsert(index, *w, false)
		if err != nil {
			return err
		}
		plan = pushPlan{target: target, index: index}
	}
	ctx = context.WithValue(ctx, identityContextKey{}, plan.index)
	crud := NewTypedCRUDForClient(a.client, "")
	input := &adapter.TypedObject[Watcher]{Spec: *w}
	if plan.target.ID == "" {
		_, err = crud.Create(ctx, input)
	} else {
		input.Spec.serverID = plan.target.ID
		_, err = crud.Update(ctx, plan.target.ID, input)
	}
	return err
}
func (a *manifestAdapter) SelectDelete(ctx context.Context, filter resources.Filter, manifests []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	index, err := IdentityIndex(ctx, a.client)
	if err != nil {
		return nil, err
	}
	var selected []Candidate
	switch {
	case manifests != nil:
		for _, obj := range manifests {
			id := obj.GetAnnotations()[WatcherIDAnnotation]
			ref := obj.GetName()
			if id != "" {
				ref = id
			}
			target, err := resolveIdentity(index, ref)
			if err == nil && id != "" && target.ID != id {
				err = fmt.Errorf("Watcher ID %q: %w; address the target by name or ID", id, adapter.ErrNotFound)
			}
			if err != nil {
				return nil, err
			}
			selected = append(selected, target)
		}
	case filter.Type == resources.FilterTypeAll:
		selected = candidatesFlat(index)
	default:
		for _, ref := range filter.ResourceUIDs {
			target, err := resolveIdentity(index, ref)
			if err != nil {
				return nil, err
			}
			selected = append(selected, target)
		}
	}
	seen := map[string]bool{}
	items := make([]unstructured.Unstructured, 0, len(selected))
	for _, c := range selected {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		obj := unstructured.Unstructured{}
		obj.SetGroupVersionKind(WatcherDescriptor().GroupVersionKind())
		obj.SetName(c.ID)
		obj.SetAnnotations(map[string]string{WatcherIDAnnotation: c.ID})
		items = append(items, obj)
	}
	return items, nil
}
func (a *manifestAdapter) Delete(ctx context.Context, ref string, opts metav1.DeleteOptions) error {
	if strings.TrimSpace(ref) == "" {
		return errors.New("Watcher reference is required")
	}
	if len(opts.DryRun) > 0 {
		_, err := ResolveDeleteIdentity(ctx, a.client, ref)
		if err != nil {
			return err
		}
		return adapter.ErrDryRunUnverified
	}
	return NewTypedCRUDForClient(a.client, "").Delete(ctx, ref)
}
