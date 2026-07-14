package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ClientDeps carries the pre-built dependencies a Resource[T]'s NewClient
// needs to construct its provider-specific REST client. HTTP is always a
// fully-configured client (logging, retry, --insecure-log-http-payload,
// timeouts, and the correct auth mode already wired) — Resource[T].NewClient
// implementations must use it as-is and must never construct their own
// transport (see docs/architecture/patterns.md § Provider ConfigLoader).
type ClientDeps struct {
	// HTTP is the pre-built HTTP client. Never construct a competing
	// *http.Client or http.Transport in NewClient — reuse this one.
	HTTP *http.Client

	// BaseURL is the resolved Grafana (or product API) base URL.
	BaseURL string

	// Namespace is the loaded config's namespace, stamped on every produced
	// envelope unless overridden by Resource.Namespace.
	Namespace string
}

// DepsLoader lazily resolves ClientDeps for one provider's resources — e.g.
// by loading REST config via providers.ConfigLoader and building the HTTP
// client via rest.HTTPClientFor, exactly like every hand-written provider
// command already does. Each provider supplies its own loader closure to
// NewProvider because adapter cannot import internal/providers (see
// ConfigKey's doc comment). NewProvider invokes it lazily, once per
// resource's Factory call, mirroring BuildRegistration's loadClient
// parameter.
type DepsLoader func(ctx context.Context) (ClientDeps, error)

// Declaration is the non-generic façade every Resource[T] satisfies (via its
// unexported registration method), letting NewProvider accept resources of
// different T in one variadic call.
type Declaration interface {
	registration(loadDeps DepsLoader) Registration
}

// Resource declares one provider resource type end-to-end: its identity
// (GVK), registration metadata, and client constructor. It is the single
// value a provider author writes per type — adapter.NewProvider derives
// Schema, GVK, Singular/Plural, and Namespace, folds in natural-key and
// deep-link registration, and wires CRUD by asserting NewClient's return
// value against the capability interfaces (Lister[T], Getter[T], ...) at the
// single audited seam in capability.go.
type Resource[T ResourceNamer] struct {
	// Group, Version, Kind identify this resource type's GroupVersionKind.
	Group, Version, Kind string

	// Singular and Plural override the derived names (lowercased Kind, and a
	// naive English pluralization of Singular, respectively). Set Plural
	// explicitly whenever the naive deriver gets an irregular plural wrong.
	Singular, Plural string

	// Namespace overrides the namespace threaded from ClientDeps.Namespace
	// (the loaded config's namespace). Rarely needed.
	Namespace string

	// StripFields lists spec-level keys to remove from output (e.g. "uuid").
	StripFields []string

	// NaturalKey names a single spec field used to build a cross-stack
	// identity extractor (SpecFieldKey), folding in RegisterNaturalKey so no
	// separate init() is needed. Leave empty to skip natural-key
	// registration for this type (composite keys still need a hand-written
	// init() calling RegisterNaturalKey directly).
	NaturalKey string

	// URLTemplate, when set, is carried on the Registration; providers.Register
	// already folds every Registration.URLTemplate into
	// deeplink.RegisterPattern, so no separate init() is needed for it either.
	URLTemplate string

	// Example is a representative value of T. It is wrapped in the standard
	// {apiVersion, kind, metadata, spec} envelope and exposed via
	// Registration.Example / ResourceAdapter.Example() — no hand-written
	// example manifest required.
	Example T

	// NewClient constructs this type's provider-specific REST client from
	// pre-built ClientDeps. The returned value is type-asserted against the
	// capability interfaces at the single audited seam — implement only the
	// verbs the underlying API actually supports; unimplemented verbs
	// resolve to errors.ErrUnsupported with no nil-Fn plumbing and no
	// provider-side flags.
	NewClient func(ctx context.Context, deps ClientDeps) (any, error)
}

// registration builds this Resource's Registration, satisfying Declaration.
// This is where Schema/GVK/Singular/Plural are derived, NaturalKey is folded
// into RegisterNaturalKey, and the Factory lazily resolves ClientDeps, builds
// the client, and runs it through the single capability-assertion seam.
func (r Resource[T]) registration(loadDeps DepsLoader) Registration {
	desc := r.descriptor()
	gvk := desc.GroupVersionKind()

	if r.NaturalKey != "" {
		RegisterNaturalKey(gvk, SpecFieldKey(r.NaturalKey))
	}

	example := exampleJSON(r.Example, desc)

	return Registration{
		Factory: func(ctx context.Context) (ResourceAdapter, error) {
			deps, err := loadDeps(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to load %s client dependencies: %w", desc.Kind, err)
			}

			client, err := r.NewClient(ctx, deps)
			if err != nil {
				return nil, fmt.Errorf("failed to construct %s client: %w", desc.Kind, err)
			}

			namespace := r.Namespace
			if namespace == "" {
				namespace = deps.Namespace
			}

			crud := newCapabilityCRUD[T](client, capabilityMeta{
				Descriptor:  desc,
				Namespace:   namespace,
				StripFields: r.StripFields,
				Example:     example,
			})
			return crud.AsAdapter(), nil
		},
		Descriptor:  desc,
		GVK:         gvk,
		Schema:      SchemaFromType[T](desc),
		Example:     example,
		URLTemplate: r.URLTemplate,
	}
}

// descriptor derives this Resource's resources.Descriptor, applying the
// Singular/Plural overrides when set.
func (r Resource[T]) descriptor() resources.Descriptor {
	singular := r.Singular
	if singular == "" {
		singular = deriveSingular(r.Kind)
	}
	plural := r.Plural
	if plural == "" {
		plural = derivePlural(singular)
	}
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: r.Group, Version: r.Version},
		Kind:         r.Kind,
		Singular:     singular,
		Plural:       plural,
	}
}

// deriveSingular lowercases Kind, matching the codebase's existing
// convention of unseparated lowercase singulars (e.g. "EscalationChain" ->
// "escalationchain", "AlertGroup" -> "alertgroup").
func deriveSingular(kind string) string {
	return strings.ToLower(kind)
}

// derivePlural naively pluralizes an English singular noun: a trailing "y"
// preceded by a consonant becomes "ies"; a trailing s/x/z/ch/sh takes "es";
// everything else takes a plain "s". This covers the common cases (including
// "EscalationPolicy" -> "escalationpolicies") but mishandles true irregulars
// — override via Resource.Plural for those.
func derivePlural(singular string) string {
	switch {
	case strings.HasSuffix(singular, "y") && len(singular) > 1 && !isVowel(singular[len(singular)-2]):
		return singular[:len(singular)-1] + "ies"
	case strings.HasSuffix(singular, "s"), strings.HasSuffix(singular, "x"), strings.HasSuffix(singular, "z"),
		strings.HasSuffix(singular, "ch"), strings.HasSuffix(singular, "sh"):
		return singular + "es"
	default:
		return singular + "s"
	}
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}

// exampleJSON wraps item in the standard TypedObject envelope and marshals
// it, giving Resource[T].Example a derived JSON manifest without requiring
// authors to hand-write one (contrast SLO's hand-written SloExample()).
func exampleJSON[T ResourceNamer](item T, desc resources.Descriptor) json.RawMessage {
	obj := TypedObject[T]{
		TypeMeta:   metav1.TypeMeta{APIVersion: desc.GroupVersion.String(), Kind: desc.Kind},
		ObjectMeta: metav1.ObjectMeta{Name: item.GetResourceName()},
		Spec:       item,
	}
	b, err := json.Marshal(obj)
	if err != nil {
		panic(fmt.Sprintf("adapter: failed to marshal example for %s: %v", desc.Kind, err))
	}
	return b
}
