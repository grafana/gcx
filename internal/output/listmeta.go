package output

import (
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/pflag"
)

// ListMetaKey is the reserved envelope key under which list commands attach
// truncation metadata. The field-selection and discovery paths in this
// package treat the key specially:
//
//   - `--json field1,field2` selection re-attaches the metadata object to the
//     output, so the truncation signal survives field selection;
//   - `--json list` / `--json ?` discovery samples the first item and never
//     lists `list_meta.*` paths;
//   - a single-key list envelope is still recognized as such when the
//     reserved key rides alongside it.
//
// These guarantees cover typed envelope structs and dynamic map[string]any
// envelopes (whose values may be native Go types — they are JSON-normalized
// before envelope handling). unstructured.UnstructuredList output does not
// participate in the contract yet: no producer attaches truncation metadata
// to unstructured lists (see docs/design/output.md § 15.5).
//
// Commands must attach the metadata with exactly this key and `omitempty`:
//
//	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
const ListMetaKey = "list_meta"

// ListMeta is machine-readable truncation metadata for list envelopes.
//
// A nil ListMeta (field absent from the payload) means the output IS the
// complete result set — presence is the truncation signal, so an agent
// reading stdout cannot mistake a page for the whole set. Table codecs
// ignore the field; the paired stderr hint comes from
// [EmitListTruncationHint].
type ListMeta struct {
	// Truncated is always true — a ListMeta is only attached when the output
	// is a partial page. Serialized explicitly (no omitempty) so agents can
	// key on it.
	Truncated bool `json:"truncated" yaml:"truncated"`
	// Returned is the number of items in this page.
	Returned int `json:"returned" yaml:"returned"`
	// Total is the size of the complete result set, present only when the
	// command actually observed it (fully-fetched source). Absent (nil)
	// means unknown — never a guess.
	Total *int `json:"total,omitempty" yaml:"total,omitempty"`
	// Cap is the safety cap that bounded the fetch, set only when the cap —
	// not the user's --limit — was the effective bound. When Cap is set,
	// raising --limit (including --limit 0) cannot retrieve more results;
	// the result set must be narrowed with filters instead.
	Cap int `json:"cap,omitempty" yaml:"cap,omitempty"`
	// Continue is the runnable command that retrieves more results. It is
	// always derived from the real invocation argv (see [AttachListMeta]) so
	// the user's filter flags survive — never a hardcoded string. Empty for
	// cap-bounded pages, where no larger --limit can help.
	Continue string `json:"continue,omitempty" yaml:"continue,omitempty"`
}

// TruncateCompleteList applies a client-side display limit to a fully-fetched
// list (a "cheaply complete" source: the API has no server-side limit, so the
// command already holds everything). Returns the page and, when items were
// dropped, a ListMeta with the observed total. limit <= 0 means unlimited:
// the input is returned unchanged with nil metadata.
func TruncateCompleteList[T any](items []T, limit int) ([]T, *ListMeta) {
	if limit <= 0 || len(items) <= limit {
		return items, nil
	}
	total := len(items)
	return items[:limit], &ListMeta{
		Truncated: true,
		Returned:  limit,
		Total:     &total,
	}
}

// TruncatePagedList applies a display limit to items over-fetched by one from
// a paginated source. Callers request limit+1 from the API and pass the full
// response: a spare item proves more data exists without draining the source,
// so Total stays unknown. limit <= 0 means unlimited: the input is returned
// unchanged with nil metadata (the caller drained the source).
func TruncatePagedList[T any](items []T, limit int) ([]T, *ListMeta) {
	if limit <= 0 || len(items) <= limit {
		return items, nil
	}
	return items[:limit], &ListMeta{
		Truncated: true,
		Returned:  limit,
	}
}

// PagedListMeta builds truncation metadata for a paginated source whose API
// reports directly whether more pages exist (a continue token / next cursor).
// serverHasMore is honored even when limit <= 0: a fetch bounded by a
// client-side safety cap is still a partial page, and reporting it as
// complete would violate the contract ("absence of list_meta == complete
// set"). safetyCap is the cap value (0 if the caller has none); Cap is
// recorded when the cap — not a larger user limit — is the binding ceiling
// (limit <= 0 or limit >= safetyCap). limit == safetyCap counts: a doubled
// --limit continuation could never return more than the cap allows, so the
// cap variant (refine filters, no continuation) is the honest signal there.
// Returns nil only when the page is genuinely complete. Total stays
// unknown — the source was not drained.
func PagedListMeta(returned, limit int, serverHasMore bool, safetyCap int) *ListMeta {
	if !serverHasMore {
		return nil
	}
	meta := &ListMeta{
		Truncated: true,
		Returned:  returned,
	}
	if safetyCap > 0 && (limit <= 0 || limit >= safetyCap) {
		meta.Cap = safetyCap
	}
	return meta
}

// AttachListMeta finalizes truncation metadata for embedding in a list
// envelope: it derives the runnable continuation command from the invocation
// argv (typically os.Args), preserving the user's filter flags, and returns
// the metadata for assignment into the envelope's reserved `list_meta` field.
// Nil-safe: returns nil for a complete result set.
func AttachListMeta(meta *ListMeta, argv []string) *ListMeta {
	if meta == nil {
		return nil
	}
	meta.Continue = listContinueCommand(meta, argv)
	return meta
}

// listContinueCommand renders the runnable command that retrieves more
// results for a truncated page:
//
//   - total observed (fully-fetched source): `<argv> --limit 0` — the full
//     set is genuinely retrievable;
//   - total unknown (undrained paginated source): `<argv> --limit <2N>` — a
//     doubled limit is a safe next step that never promises --limit 0
//     retrieves everything (a safety cap may exist upstream);
//   - cap-bounded page: no continuation — a larger --limit cannot help.
//
// The command is always derived from argv with any prior --limit stripped
// (see [BuildListLimitCommand]), so filters survive verbatim.
func listContinueCommand(meta *ListMeta, argv []string) string {
	if meta == nil || !meta.Truncated || len(argv) == 0 {
		return ""
	}
	switch {
	case meta.Cap > 0:
		return ""
	case meta.Total != nil:
		return BuildListLimitCommand(argv, 0)
	case meta.Returned > 0:
		return BuildListLimitCommand(argv, 2*meta.Returned)
	default:
		return ""
	}
}

// EmitListTruncationHint writes the standardized truncation hint for a list
// page to w (stderr). No-op when meta is nil (complete result set). Routes
// through [EmitHint], so agent mode emits the JSONL class:"hint" form with
// the continuation in the `command` field. The continuation is read from
// meta.Continue, populated by [AttachListMeta] — the single derivation point,
// so the stderr hint and the payload's list_meta.continue can never disagree.
//
// TTY shapes:
//
//	total known:   "hint: showing first 5 of 219. See all results with: gcx datasources list --limit 0"
//	total unknown: "hint: showing first 50; more results are available. See more with: gcx irm oncall alert-groups list --limit 100"
//	cap bounded:   "hint: showing first 1000 (safety cap). Refine filters to narrow the result set"
func EmitListTruncationHint(w io.Writer, meta *ListMeta) {
	if meta == nil || !meta.Truncated {
		return
	}

	if meta.Cap > 0 {
		EmitHint(w, fmt.Sprintf("showing first %d (safety cap). Refine filters to narrow the result set", meta.Returned), "")
		return
	}

	cont := meta.Continue

	summary := fmt.Sprintf("showing first %d; more results are available", meta.Returned)
	connective := "See more with"
	if meta.Total != nil {
		summary = fmt.Sprintf("showing first %d of %d", meta.Returned, *meta.Total)
		connective = "See all results with"
	}

	if cont == "" {
		// No runnable continuation (empty argv at AttachListMeta time) — emit
		// the bare summary rather than a dangling connective.
		EmitHint(w, summary, "")
		return
	}

	// Agent mode carries the continuation in the structured `command` field,
	// so the summary stays bare. On a TTY, EmitHint renders
	// "hint: <summary>: <command>" — fold the connective into the summary so
	// the line reads as a sentence instead of a double-colon splice.
	if agent.IsAgentMode() {
		EmitHint(w, summary, cont)
		return
	}
	EmitHint(w, summary+". "+connective, cont)
}

// BindListLimit registers the standard --limit flag for a list command with
// the uniform contract wording ("0 means all results are returned").
// Validation (limit >= 0) is enforced by [Options.Validate], which every
// list command already calls, so no extra per-command wiring is needed.
//
// The binder is deliberately minimal: it only registers the flag and hooks
// validation. Commands still pass the limit to their clients for server-side
// pushdown where the API supports it, and choose the truncation constructor
// matching their source shape (see [TruncateCompleteList],
// [TruncatePagedList], [PagedListMeta]).
//
// Sources bounded by a client-side safety cap must NOT use this binder — the
// "0 means all" promise would be dishonest there. Keep a bespoke flag
// description that discloses the cap, and report the cap via
// [PagedListMeta].
func (opts *Options) BindListLimit(flags *pflag.FlagSet, p *int, subject string, def int) {
	flags.IntVar(p, "limit", def, fmt.Sprintf("Maximum number of %s to return. 0 means all results are returned", subject))
	opts.listLimit = p
}
