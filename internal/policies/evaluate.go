package policies

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/grafana/pkg/policy/api"
	"github.com/grafana/grafana/pkg/policy/engine"
	policyschema "github.com/grafana/grafana/pkg/policy/schema"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Finding is one violated validation, with the action its binding takes.
type Finding struct {
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Source     string     `json:"source,omitempty"`
	Policy     string     `json:"policy"`
	Binding    string     `json:"binding"`
	Action     api.Action `json:"action"`
	Validation string     `json:"validation"`
	FieldPath  string     `json:"fieldPath,omitempty"`
	Message    string     `json:"message"`
	// Error is set when the finding comes from an expression that failed to evaluate, which the
	// policy's failure policy turns into a violation.
	Error bool `json:"error,omitempty"`
}

// PolicyError is a policy that could not be used, typically because it does not compile against
// the instance's schemas.
type PolicyError struct {
	Policy string `json:"policy"`
	Error  string `json:"error"`
}

// Report is the outcome of evaluating every applicable policy against a set of resources.
type Report struct {
	// Evaluated is the number of resources that at least one policy applied to.
	Evaluated int `json:"evaluated"`
	// Checked is the number of resources considered, whether or not a policy applied.
	Checked       int           `json:"checked"`
	Findings      []Finding     `json:"findings"`
	PolicyErrors  []PolicyError `json:"policyErrors,omitempty"`
	SkippedChecks int           `json:"skippedChecks,omitempty"`
}

// Denied reports whether any finding would reject a write.
func (r Report) Denied() int {
	n := 0
	for _, f := range r.Findings {
		if f.Action == api.ActionDeny {
			n++
		}
	}
	return n
}

// Evaluator evaluates a namespace's policies against resources.
type Evaluator struct {
	namespace    string
	set          *engine.Set
	policyErrors []PolicyError
}

// NewEvaluator compiles the given policies against schemas from resolver. Parameter objects are
// read from client, using resourceFor to find the API resource of a parameter kind. Policies that
// fail to compile are reported rather than aborting the evaluation, as admission skips them too.
func NewEvaluator(
	namespace string,
	policies []Policy,
	resolver policyschema.Resolver,
	client dynamic.Interface,
	resourceFor func(schema.GroupVersionKind) (schema.GroupVersionResource, bool),
) (*Evaluator, error) {
	compiler := engine.NewCompiler(policyschema.NewCachingResolver(resolver))
	e := &Evaluator{namespace: namespace}

	var compiled []*engine.CompiledPolicy
	var bindings []api.Binding
	for _, p := range policies {
		cp, err := compiler.Compile(p.Policy)
		if err != nil {
			e.policyErrors = append(e.policyErrors, PolicyError{Policy: p.Name, Error: err.Error()})
			continue
		}
		compiled = append(compiled, cp)
		for _, b := range p.Bindings {
			if _, hasParams := cp.ParamGVK(); hasParams && b.ParamRef == nil {
				continue
			}
			if len(b.Validate()) > 0 {
				continue
			}
			bindings = append(bindings, b)
		}
	}

	set, err := engine.NewSet(compiled, bindings, &paramSource{client: client, resourceFor: resourceFor, cache: map[string]paramResult{}})
	if err != nil {
		return nil, err
	}
	e.set = set
	return e, nil
}

// Evaluate checks every resource as though it were being created now, which is the question a
// static check answers: would this object be admitted? Rules that only apply when a resource
// changes, such as a rename, therefore treat every resource as new.
func (e *Evaluator) Evaluate(ctx context.Context, list []*resources.Resource) Report {
	report := Report{Findings: []Finding{}, PolicyErrors: e.policyErrors}
	for _, r := range list {
		gvk := r.GroupVersionKind()
		// Policies never apply to the policy API itself.
		if gvk.Group == Group {
			continue
		}
		report.Checked++
		if !e.set.Matches(gvk) {
			continue
		}
		obj := r.ToUnstructured()
		ev := e.set.EvaluateAll(ctx, engine.Input{
			GVK:       gvk,
			Namespace: e.namespace,
			Object:    obj.Object,
			Request: &engine.RequestInfo{
				Operation: api.OperationCreate,
				Name:      r.Name(),
				Namespace: e.namespace,
			},
		})
		if len(ev.Results) > 0 {
			report.Evaluated++
		}
		for _, res := range ev.Results {
			report.SkippedChecks += len(res.Skipped)
		}
		for _, d := range ev.Decisions {
			report.Findings = append(report.Findings, Finding{
				Kind:       gvk.Kind,
				Name:       r.Name(),
				Source:     r.SourcePath(),
				Policy:     d.Policy,
				Binding:    d.Binding,
				Action:     d.Action,
				Validation: d.Validation,
				FieldPath:  d.FieldPath,
				Message:    d.Message,
				Error:      d.Err != nil,
			})
		}
	}
	sort.SliceStable(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Policy < b.Policy
	})
	return report
}

// paramSource reads parameter objects from the instance, once per object.
type paramSource struct {
	client      dynamic.Interface
	resourceFor func(schema.GroupVersionKind) (schema.GroupVersionResource, bool)

	mu    sync.Mutex
	cache map[string]paramResult
}

type paramResult struct {
	obj map[string]any
	err error
}

func (p *paramSource) GetParams(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string) (map[string]any, error) {
	key := gvk.String() + "/" + namespace + "/" + name
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.cache[key]; ok {
		return r.obj, r.err
	}
	r := p.fetch(ctx, gvk, namespace, name)
	p.cache[key] = r
	return r.obj, r.err
}

func (p *paramSource) fetch(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string) paramResult {
	gvr, ok := p.resourceFor(gvk)
	if !ok {
		return paramResult{err: fmt.Errorf("the instance does not serve param kind %s", gvk)}
	}
	obj, err := p.client.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return paramResult{err: engine.ErrParamsNotFound}
	}
	if err != nil {
		return paramResult{err: errors.Join(fmt.Errorf("reading %s %q", gvk.Kind, name), err)}
	}
	return paramResult{obj: obj.Object}
}
