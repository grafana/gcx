//nolint:testpackage // white-box tests drive the unexported pagination loop
package irm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// alertGroupPage is one page served by newPagedAlertGroupsServer: a batch of
// raw alert-group items plus whether a `next` cursor points at the following
// page.
type alertGroupPage struct {
	items   []json.RawMessage
	hasNext bool
}

// pagedServerState records what the fake OnCall backend observed.
type pagedServerState struct {
	requests     int
	firstPerPage string
	// firstQuery is the full query string of the first alertgroups request,
	// so filter tests can assert the exact wire encoding.
	firstQuery url.Values
}

// newPagedAlertGroupsServer serves the OnCall internal alertgroups endpoint
// through the plugin-proxy path shape the real client uses. Pages are
// addressed by the `page` query parameter (1-based; absent means page 1) and
// the `next` cursor is an absolute URL containing the `/api/internal/v1/`
// marker so ExtractNextPath re-routes follow-ups through the proxy path,
// exactly as the production backend does.
func newPagedAlertGroupsServer(t *testing.T, pages []alertGroupPage) (*httptest.Server, *pagedServerState) {
	t.Helper()
	state := &pagedServerState{}

	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc(BasePath+"/teams/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[],"next":null}`))
	})
	mux.HandleFunc(BasePath+"/alertgroups/", func(w http.ResponseWriter, r *http.Request) {
		state.requests++
		if state.requests == 1 {
			state.firstQuery = r.URL.Query()
			state.firstPerPage = state.firstQuery.Get("perpage")
		}
		pageNum := 1
		if p := r.URL.Query().Get("page"); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil {
				t.Errorf("invalid page param %q", p)
			}
			pageNum = n
		}
		if pageNum < 1 || pageNum > len(pages) {
			t.Errorf("requested page %d out of range (have %d pages)", pageNum, len(pages))
			http.Error(w, "no such page", http.StatusNotFound)
			return
		}
		page := pages[pageNum-1]
		var next *string
		if page.hasNext {
			u := fmt.Sprintf("%s/oncall/api/internal/v1/alertgroups/?page=%d", srv.URL, pageNum+1)
			next = &u
		}
		body, err := json.Marshal(map[string]any{"results": page.items, "next": next})
		if err != nil {
			t.Fatalf("marshal page: %v", err)
		}
		_, _ = w.Write(body)
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, state
}

func onCallClientFor(srv *httptest.Server) *OnCallClient {
	return &OnCallClient{HTTPClient: srv.Client(), Host: srv.URL}
}

// TestListAlertGroupsRaw_MultiPageDrain drives the real pagination loop
// across two pages to a natural end: everything is returned, no truncation
// evidence, no observed total (nothing was trimmed).
func TestListAlertGroupsRaw_MultiPageDrain(t *testing.T) {
	srv, state := newPagedAlertGroupsServer(t, []alertGroupPage{
		{items: rawAlertGroups(2), hasNext: true},
		{items: rawAlertGroups(2)},
	})

	out, info, err := listAlertGroupsRaw(context.Background(), onCallClientFor(srv), alertGroupListFilters{}, 0)
	if err != nil {
		t.Fatalf("listAlertGroupsRaw: %v", err)
	}
	if len(out) != 4 {
		t.Errorf("items = %d, want 4 (both pages)", len(out))
	}
	if info.HasMore {
		t.Error("HasMore = true, want false (pagination ended before the bound)")
	}
	if info.Total != nil {
		t.Errorf("Total = %d, want nil (nothing trimmed)", *info.Total)
	}
	if state.requests != 2 {
		t.Errorf("requests = %d, want 2", state.requests)
	}
	if state.firstPerPage != strconv.Itoa(alertGroupListPerPageMax) {
		t.Errorf("first perpage = %q, want %d (limit 0 uses the per-page max)", state.firstPerPage, alertGroupListPerPageMax)
	}
}

// TestListAlertGroupsRaw_OvershootWithNextCursor: the final page overshoots
// the effective cap AND reports a next cursor — truncated, total unknown.
func TestListAlertGroupsRaw_OvershootWithNextCursor(t *testing.T) {
	srv, state := newPagedAlertGroupsServer(t, []alertGroupPage{
		{items: rawAlertGroups(2), hasNext: true},
		{items: rawAlertGroups(2), hasNext: true},
	})

	out, info, err := listAlertGroupsRaw(context.Background(), onCallClientFor(srv), alertGroupListFilters{}, 3)
	if err != nil {
		t.Fatalf("listAlertGroupsRaw: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("items = %d, want 3 (trimmed to the limit)", len(out))
	}
	if !info.HasMore {
		t.Error("HasMore = false, want true (next cursor on the stopping page)")
	}
	if info.Total != nil {
		t.Errorf("Total = %d, want nil (source not drained)", *info.Total)
	}
	if state.firstPerPage != "3" {
		t.Errorf("first perpage = %q, want 3 (min(limit, perPageMax))", state.firstPerPage)
	}
}

// TestListAlertGroupsRaw_OvershootWithoutNextCursor locks the H1 fix: the
// final page overshoots the effective cap and reports NO next cursor —
// items are dropped in-hand, so that IS truncation evidence, and because
// pagination ended the total is genuinely observed.
func TestListAlertGroupsRaw_OvershootWithoutNextCursor(t *testing.T) {
	srv, _ := newPagedAlertGroupsServer(t, []alertGroupPage{
		{items: rawAlertGroups(2), hasNext: true},
		{items: rawAlertGroups(2)},
	})

	out, info, err := listAlertGroupsRaw(context.Background(), onCallClientFor(srv), alertGroupListFilters{}, 3)
	if err != nil {
		t.Fatalf("listAlertGroupsRaw: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("items = %d, want 3 (trimmed to the limit)", len(out))
	}
	if !info.HasMore {
		t.Error("HasMore = false, want true — the overshooting final page dropped items in-hand (H1 regression)")
	}
	if info.Total == nil {
		t.Fatal("Total = nil, want 4 (source fully drained, total observed)")
	}
	if *info.Total != 4 {
		t.Errorf("Total = %d, want 4", *info.Total)
	}
}

// TestListAlertGroupsRaw_CapAlignedExactFit: pagination ends exactly at the
// effective cap — the page IS the complete set; no truncation metadata.
func TestListAlertGroupsRaw_CapAlignedExactFit(t *testing.T) {
	srv, _ := newPagedAlertGroupsServer(t, []alertGroupPage{
		{items: rawAlertGroups(2), hasNext: true},
		{items: rawAlertGroups(2)},
	})

	out, info, err := listAlertGroupsRaw(context.Background(), onCallClientFor(srv), alertGroupListFilters{}, 4)
	if err != nil {
		t.Fatalf("listAlertGroupsRaw: %v", err)
	}
	if len(out) != 4 {
		t.Errorf("items = %d, want 4", len(out))
	}
	if info.HasMore {
		t.Error("HasMore = true, want false (exact fit is a complete set)")
	}
	if info.Total != nil {
		t.Errorf("Total = %d, want nil (no truncation)", *info.Total)
	}
}

// TestAlertGroupList_RichPath_EndToEndHTTP_DrainedOvershoot exercises the
// full command path — real OnCallClient, real listAlertGroupsRaw pagination
// against an httptest backend, list_meta attachment, stderr hint — for the
// drained-overshoot case fixed by H1: the observed total is reported and the
// continuation honestly promises --limit 0.
func TestAlertGroupList_RichPath_EndToEndHTTP_DrainedOvershoot(t *testing.T) {
	srv, state := newPagedAlertGroupsServer(t, []alertGroupPage{
		{items: rawAlertGroups(2), hasNext: true},
		{items: rawAlertGroups(2)},
	})

	payload, stderr := runAlertGroupList(t, onCallClientFor(srv), "--limit", "3")

	if state.requests != 2 {
		t.Errorf("requests = %d, want 2 (real pagination loop)", state.requests)
	}
	if len(payload.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(payload.Items))
	}
	meta := payload.ListMeta
	if meta == nil || !meta.Truncated || meta.Returned != 3 {
		t.Fatalf("list_meta = %+v, want truncated page of 3", meta)
	}
	if meta.Total == nil || *meta.Total != 4 {
		t.Fatalf("list_meta.total = %v, want 4 (drained source, observed total)", meta.Total)
	}
	if meta.Cap != 0 {
		t.Errorf("list_meta.cap = %d, want 0 (the user's limit was the bound)", meta.Cap)
	}
	if meta.Continue != "gcx irm oncall alert-groups list --limit 0" {
		t.Errorf("list_meta.continue = %q, want --limit 0 (total observed and retrievable)", meta.Continue)
	}
	want := "hint: showing first 3 of 4. See all results with: gcx irm oncall alert-groups list --limit 0"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr missing known-total truncation hint %q:\n%s", want, stderr)
	}
}

// TestAlertGroupList_FilterQueryParams pins the wire encoding of the filter
// flags added to close the escalation-chain gap (grafana/gcx#1156). Each
// option filter is sent as a REPEATED param, never comma-joined — matching
// team/integration — and the two daterange filters are sent as the naive-UTC
// `<from>_<to>` pair the internal API expects. `--to` on its own still sends a
// pair, with the start defaulted to the unix epoch.
//
// Assertions are per-key so the always-present defaults (status, is_root,
// perpage) don't have to be restated in every case.
func TestAlertGroupList_FilterQueryParams(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want url.Values
	}{
		{
			name: "escalation chains repeat, never comma-joined",
			args: []string{"--escalation-chain", "EC1,EC2"},
			want: url.Values{"escalation_chain": {"EC1", "EC2"}},
		},
		{
			name: "repeated flag occurrences accumulate",
			args: []string{"--escalation-chain", "EC1", "--escalation-chain", "EC2"},
			want: url.Values{"escalation_chain": {"EC1", "EC2"}},
		},
		{
			name: "acknowledged-by and resolved-by",
			args: []string{"--acknowledged-by", "U1,U2", "--resolved-by", "U3"},
			want: url.Values{
				"acknowledged_by": {"U1", "U2"},
				"resolved_by":     {"U3"},
			},
		},
		{
			name: "started-at window from an absolute --from/--to pair",
			args: []string{"--from", "2026-01-01T00:00:00Z", "--to", "2026-01-31T12:00:00Z"},
			want: url.Values{"started_at": {"2026-01-01T00:00:00_2026-01-31T12:00:00"}},
		},
		{
			name: "--from accepts a unix timestamp",
			args: []string{"--from", "1767225600", "--to", "2026-01-31T12:00:00Z"},
			want: url.Values{"started_at": {"2026-01-01T00:00:00_2026-01-31T12:00:00"}},
		},
		{
			name: "--to alone defaults the start to the unix epoch",
			args: []string{"--to", "2026-01-31T12:00:00Z"},
			want: url.Values{"started_at": {"1970-01-01T00:00:00_2026-01-31T12:00:00"}},
		},
		{
			name: "resolved-at window is a separate param",
			args: []string{"--resolved-from", "2026-01-01T00:00:00Z", "--resolved-to", "2026-01-31T12:00:00Z"},
			want: url.Values{"resolved_at": {"2026-01-01T00:00:00_2026-01-31T12:00:00"}},
		},
		{
			name: "no time flags means no daterange params",
			args: []string{"--escalation-chain", "EC1"},
			want: url.Values{"started_at": nil, "resolved_at": nil},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, state := newPagedAlertGroupsServer(t, []alertGroupPage{{items: rawAlertGroups(1)}})
			runAlertGroupList(t, onCallClientFor(srv), tc.args...)

			if state.firstQuery == nil {
				t.Fatal("no alertgroups request observed")
			}
			for key, want := range tc.want {
				if got := state.firstQuery[key]; !reflect.DeepEqual(got, want) {
					t.Errorf("query[%q] = %v, want %v (full query: %v)", key, got, want, state.firstQuery)
				}
			}
		})
	}
}

// TestAlertGroupList_MaxAgeAndFromAreMutuallyExclusive: both compile into the
// same started_at range param, so the combination is rejected at validation
// time — before any config or network work.
func TestAlertGroupList_MaxAgeAndFromAreMutuallyExclusive(t *testing.T) {
	srv, state := newPagedAlertGroupsServer(t, []alertGroupPage{{items: rawAlertGroups(1)}})
	resetAgentMode(t)

	cmd := newAlertGroupListCommand(&fakeLoader{client: onCallClientFor(srv)})
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"-o", "json", "--max-age", "24h", "--from", "now-30d"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want a mutual-exclusion error")
	}
	const want = "--max-age cannot be combined with --from or --to: both bound the started-at window"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if state.requests != 0 {
		t.Errorf("requests = %d, want 0 (validation must fire before any network work)", state.requests)
	}
}

// TestAlertGroupList_RichPath_UnretrievableTotalNotAttached pins the honesty
// guard on the drained-overshoot total: when the observed total exceeds the
// hard cap and the cap was not the bound, attaching it would derive a
// "--limit 0 retrieves everything" continuation that the cap makes a lie —
// so the total stays unknown and the doubled-limit continuation is used.
func TestAlertGroupList_RichPath_UnretrievableTotalNotAttached(t *testing.T) {
	total := alertGroupListHardCap + 500
	fake := &fakeRichListAPI{
		items: rawAlertGroups(2),
		page:  alertGroupPageInfo{HasMore: true, Total: &total},
	}
	payload, _ := runAlertGroupList(t, fake, "--limit", "950")

	meta := payload.ListMeta
	if meta == nil || !meta.Truncated {
		t.Fatalf("list_meta = %+v, want truncated", meta)
	}
	if meta.Total != nil {
		t.Errorf("list_meta.total = %d, want absent (total above the hard cap is not retrievable)", *meta.Total)
	}
	if meta.Continue != "gcx irm oncall alert-groups list --limit 4" {
		t.Errorf("list_meta.continue = %q, want doubled-limit continuation", meta.Continue)
	}
}

// TestAlertGroupList_RichPath_CapBoundedDrainedTotalAttached: a cap-bounded
// page carries no continuation, so an observed total is always honest to
// attach — the agent learns both the ceiling and the real set size.
func TestAlertGroupList_RichPath_CapBoundedDrainedTotalAttached(t *testing.T) {
	total := alertGroupListHardCap + 5
	fake := &fakeRichListAPI{
		items: rawAlertGroups(3),
		page:  alertGroupPageInfo{HasMore: true, Total: &total},
	}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "0")

	meta := payload.ListMeta
	if meta == nil || !meta.Truncated {
		t.Fatalf("list_meta = %+v, want truncated", meta)
	}
	if meta.Cap != alertGroupListHardCap {
		t.Errorf("list_meta.cap = %d, want %d", meta.Cap, alertGroupListHardCap)
	}
	if meta.Total == nil || *meta.Total != total {
		t.Fatalf("list_meta.total = %v, want %d (observed on the drained source)", meta.Total, total)
	}
	if meta.Continue != "" {
		t.Errorf("list_meta.continue = %q, want empty (no --limit can beat the cap)", meta.Continue)
	}
	if !strings.Contains(stderr, "safety cap") {
		t.Errorf("stderr missing cap-variant hint:\n%s", stderr)
	}
}
