package kg

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"golang.org/x/sync/errgroup"
)

// typeCheck describes the lookup whose entity type checkEntityType checks.
type typeCheck struct {
	verb string // "list" or "inspect"
	name string // the entity name asked for; "" if none
	// search is the caller's search. Probes reuse it, with the entity type
	// (and a kind property) swapped, so a suggested command returns what the
	// probe found.
	search typeSearch
	// windowGiven reports whether the caller passed --from/--to or --since.
	windowGiven bool
	// flags are the caller's flags carried into suggested commands, before
	// the window: --property/--insight for list, and the scope flags.
	flags []string
}

// checkEntityType runs after a lookup of entityType found nothing (or failed
// with a client error). It returns the type to use: a type that differs only
// in letter case or surrounding whitespace comes back corrected, so the
// caller can run the lookup again; any other type that has entities in the
// window, or is in the window's schema, comes back unchanged, and the empty
// result stands. A type that the window's types do not include is rejected
// with an error that names those types and, where a probe finds them,
// commands that return the entities the caller is probably after (an entity
// with the given name under another type, or the type name used as the value
// of a *_type/*kind property); when the caller passed no window, the error
// also says the default window is the last hour. If the window holds no
// entities of any type, the result stands when the caller chose the window,
// and otherwise the error says the default window is the last hour. The check is best-effort: if the
// type counts or the schema cannot be read, entityType comes back unchanged.
func checkEntityType(ctx context.Context, client *Client, entityType string, c typeCheck) (string, error) {
	startMs, endMs := c.search.startMs, c.search.endMs
	counts, err := client.CountEntityTypes(ctx, startMs, endMs, nil)
	if err != nil {
		return entityType, nil //nolint:nilerr // best-effort: type validation is advisory
	}
	present := typesByCount(counts)
	if t, ok := matchTypeName(entityType, present); ok {
		return t, nil
	}
	schemaResp, err := client.FetchGraphSchema(ctx, startMs, endMs)
	if err != nil {
		return entityType, nil //nolint:nilerr // best-effort: type validation is advisory
	}
	schema := processGraphSchema(schemaResp)
	c.search.dims.seed(schema) // the probes' scope filter reads the same schema
	schemaTypes := make([]string, 0, len(schema.EntityTypes))
	for _, et := range schema.EntityTypes {
		schemaTypes = append(schemaTypes, et.Type)
	}
	if t, ok := matchTypeName(entityType, schemaTypes); ok {
		return t, nil
	}

	window := fmt.Sprintf("--from %s --to %s", fmtWindowTime(startMs), fmtWindowTime(endMs))
	span := fmtWindowTime(startMs) + ".." + fmtWindowTime(endMs)
	// The type counts include the graph's own catalog records even in a
	// window without entities, so the schema decides whether it is empty.
	if len(schemaTypes) == 0 {
		if c.windowGiven {
			return entityType, nil
		}
		return "", gcxerrors.DetailedError{
			Summary: "no entities of any type in the window " + span,
			Details: "Without --from/--to or --since, the window is the last hour.",
			Suggestions: []string{
				"Pass --from/--to covering the time you are asking about",
			},
		}
	}
	summary := fmt.Sprintf("entity type %q is not in the graph in the window %s", entityType, span)
	details := "Entity types in this window: " + strings.Join(schemaTypes, ", ")
	var suggestions []string
	if !c.windowGiven {
		// The type may be missing only because the default window is wrong.
		summary += " (the default window: the last hour)"
		details = "Without --from/--to or --since, the window is the last hour. " + details
		suggestions = append(suggestions, "Pass --from/--to covering the time you are asking about")
	}
	seen := map[string]bool{}
	for _, h := range probeTypeAlternatives(ctx, client, schema, present, entityType, c) {
		cmd := h.command(c, window)
		if !seen[cmd] {
			seen[cmd] = true
			suggestions = append(suggestions, "These return matching entities: "+cmd)
		}
	}
	suggestions = append(suggestions, fmt.Sprintf("Run 'gcx kg meta schema %s' for each type's properties", window))
	return "", gcxerrors.DetailedError{
		Summary:     summary,
		Details:     details,
		Suggestions: suggestions,
	}
}

// isClientAPIError reports whether err is a 4xx from the KG API that is not
// already a detailed gcx error; for a lookup by type it may mean the type is
// wrong.
func isClientAPIError(err error) bool {
	var de gcxerrors.DetailedError
	if err == nil || errors.As(err, &de) {
		return false
	}
	var apiErr *APIError
	return errors.As(err, &apiErr) && !apiErr.IsServerError()
}

// typesByCount returns the types with entities, most entities first.
func typesByCount(counts map[string]int64) []string {
	types := make([]string, 0, len(counts))
	for t, n := range counts {
		if n > 0 {
			types = append(types, t)
		}
	}
	sort.Slice(types, func(i, j int) bool {
		if counts[types[i]] != counts[types[j]] {
			return counts[types[i]] > counts[types[j]]
		}
		return types[i] < types[j]
	})
	return types
}

// matchTypeName returns the element of types equal to t, or else the one equal
// to t ignoring letter case and surrounding whitespace.
func matchTypeName(t string, types []string) (string, bool) {
	for _, c := range types {
		if c == t {
			return c, true
		}
	}
	trimmed := strings.TrimSpace(t)
	for _, c := range types {
		if strings.EqualFold(c, trimmed) {
			return c, true
		}
	}
	return "", false
}

func fmtWindowTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// typeAlternative is an entity type (and optional property match) under which
// a probe found entities for a type name that is not in the graph.
type typeAlternative struct {
	entityType string
	property   string // property name; "" for a name-only match
	value      string
	// scope is the scope of the one entity an inspect probe found; nil when
	// it found several, and for list.
	scope map[string]string
}

// command renders the command that returns the entities the probe found.
// For inspect it is an inspect command only when the probe found exactly one
// entity: inspect needs an entity's full scope and fails on a name that
// several entities share, so for those it is the list command that returns
// them all with their scopes.
func (a typeAlternative) command(c typeCheck, window string) string {
	var parts []string
	flags := c.flags
	if c.verb == "inspect" && a.scope != nil {
		parts = append(parts, "gcx kg entities inspect "+shellArg(a.entityType+"--"+c.name))
		f := scopeFlags{env: a.scope["env"], namespace: a.scope["namespace"], site: a.scope["site"]}
		flags = f.args()
	} else {
		parts = append(parts, "gcx kg entities list --type "+shellArg(a.entityType))
		if a.property != "" {
			parts = append(parts, "--property "+shellArg(a.property+"="+a.value))
		}
		if c.verb == "inspect" {
			parts = append(parts, "--property "+shellArg("name="+c.name))
		}
	}
	parts = append(parts, flags...)
	parts = append(parts, window)
	return strings.Join(parts, " ")
}

var plainShellArg = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./~-]+$`)

// shellArg quotes s for a suggested shell command when it is not plain.
func shellArg(s string) string {
	if plainShellArg.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// flagArgs renders a repeatable flag's values as suggested-command arguments.
func flagArgs(flag string, values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, flag+" "+shellArg(v))
	}
	return out
}

// args renders the scope flags the caller set as suggested-command arguments.
func (f *scopeFlags) args() []string {
	var out []string
	for _, kv := range [][2]string{{"--env", f.env}, {"--namespace", f.namespace}, {"--site", f.site}} {
		if kv[1] != "" {
			out = append(out, kv[0]+" "+shellArg(kv[1]))
		}
	}
	return out
}

// isKindProperty reports whether a property name looks like it holds a kind
// (e.g. a workload kind), so a type name may appear as its value.
func isKindProperty(p string) bool {
	p = strings.ToLower(p)
	return strings.HasSuffix(p, "_type") || strings.HasSuffix(p, "kind")
}

// maxKindProbes and maxNameProbes bound the searches run to build
// suggestions for one unknown type.
const (
	maxKindProbes = 12
	maxNameProbes = 12
)

// probeTypeAlternatives looks for the entities a caller probably meant by an
// unknown type: entities whose kind-like property equals the type name (in
// lower case), and entities called c.name under another schema type. Each probe is
// the caller's search with the type swapped (and the kind property added),
// filtered client-side as the caller's search is, so a probe that returns
// rows means the suggested command returns them. Only types with entities in
// the window are probed, most entities first; kind and name probes have
// separate budgets. Property matches come first.
func probeTypeAlternatives(ctx context.Context, client *Client, schema KGSchemaResult, present []string, entityType string, c typeCheck) []typeAlternative {
	props := make(map[string][]string, len(schema.EntityTypes))
	for _, et := range schema.EntityTypes {
		props[et.Type] = et.Properties
	}
	value := strings.ToLower(strings.TrimSpace(entityType))
	var kindProbes, nameProbes []typeAlternative
	for _, t := range present {
		if _, ok := props[t]; !ok {
			continue // not an entity type of the schema, such as a catalog record
		}
		for _, p := range props[t] {
			if isKindProperty(p) && len(kindProbes) < maxKindProbes {
				kindProbes = append(kindProbes, typeAlternative{entityType: t, property: p, value: value})
			}
		}
		if c.name != "" && len(nameProbes) < maxNameProbes {
			nameProbes = append(nameProbes, typeAlternative{entityType: t})
		}
	}
	probes := append(kindProbes, nameProbes...)

	found := make([]bool, len(probes))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i, pr := range probes {
		g.Go(func() error {
			s := c.search
			if pr.property != "" {
				s.propertyFilters = append([]PropertyMatcher{{Name: pr.property, Op: "=", Value: pr.value}}, c.search.propertyFilters...)
			}
			page, err := s.page(gctx, client, pr.entityType, 0)
			if err != nil {
				return nil
			}
			rows := s.filter(page.Entities)
			found[i] = len(rows) > 0
			if c.verb == "inspect" && len(rows) == 1 && !hasMorePages(page) {
				probes[i].scope = rows[0].Scope
				if probes[i].scope == nil {
					probes[i].scope = map[string]string{}
				}
			}
			return nil
		})
	}
	_ = g.Wait()

	var hits []typeAlternative
	seenType := map[string]bool{}
	for i, pr := range probes {
		if !found[i] {
			continue
		}
		if pr.property == "" && seenType[pr.entityType] {
			continue // a property match for this type already names the entity
		}
		seenType[pr.entityType] = true
		hits = append(hits, pr)
	}
	return hits
}
