//nolint:testpackage // white-box tests require access to unexported IRM types and helpers
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
)

// fakeRichListAPI drives the rich alert-groups list path (internal API). It
// records the wire limit so tests can prove server-side pushdown.
type fakeRichListAPI struct {
	OnCallAPI

	items    []json.RawMessage
	page     alertGroupPageInfo
	gotLimit int
}

func (f *fakeRichListAPI) ListAlertGroupsRaw(_ context.Context, _ alertGroupListFilters, limit int) ([]json.RawMessage, alertGroupPageInfo, error) {
	f.gotLimit = limit
	return f.items, f.page, nil
}

func (f *fakeRichListAPI) GetAlertGroupRich(context.Context, string) (*AlertGroupRich, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRichListAPI) ListAlertIDs(context.Context, string, int) ([]string, int, error) {
	return nil, 0, nil
}

func (f *fakeRichListAPI) GetAlertRich(context.Context, string) (*alertAPI, *AlertRich, error) {
	return nil, nil, errors.New("not implemented")
}

func (f *fakeRichListAPI) ResolveTeams(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// fakeLegacyListAPI drives the alternate-implementation fallback list path.
// It intentionally does NOT implement RichAlertGroupReader, so the command
// takes the legacy branch. The applied ListConfig is recorded to observe the
// wire limit.
type fakeLegacyListAPI struct {
	OnCallAPI

	items  []AlertGroup
	gotCfg ListConfig
}

func (f *fakeLegacyListAPI) ListAlertGroups(_ context.Context, opts ...ListOption) ([]AlertGroup, error) {
	for _, o := range opts {
		o(&f.gotCfg)
	}
	if f.gotCfg.Limit > 0 && len(f.items) > f.gotCfg.Limit {
		return f.items[:f.gotCfg.Limit], nil
	}
	return f.items, nil
}

// alertGroupListPayload is the decoded stdout shape asserted by these tests.
type alertGroupListPayload struct {
	Items    []map[string]any `json:"items"`
	ListMeta *cmdio.ListMeta  `json:"list_meta"`
}

func runAlertGroupList(t *testing.T, client OnCallAPI, args ...string) (alertGroupListPayload, string) {
	t.Helper()
	resetAgentMode(t)
	testutils.PinArgv(t, append([]string{"gcx", "irm", "oncall", "alert-groups", "list"}, args...)...)

	cmd := newAlertGroupListCommand(&fakeLoader{client: client})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"-o", "json"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("alert-groups list: %v\nstderr=%s", err, stderr.String())
	}

	var payload alertGroupListPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON on stdout: %v\nraw=%s", err, stdout.String())
	}
	return payload, stderr.String()
}

func rawAlertGroups(n int) []json.RawMessage {
	items := make([]json.RawMessage, 0, n)
	for i := range n {
		items = append(items, json.RawMessage(fmt.Sprintf(
			`{"pk":"AG%d","alerts_count":1,"started_at":"2026-01-01T00:00:00Z"}`, i)))
	}
	return items
}

func TestAlertGroupList_RichPath_UserLimitTruncates(t *testing.T) {
	fake := &fakeRichListAPI{items: rawAlertGroups(2), page: alertGroupPageInfo{HasMore: true}}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "2")

	if fake.gotLimit != 2 {
		t.Errorf("wire limit = %d, want 2 (rich path passes the limit server-side)", fake.gotLimit)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(payload.Items))
	}
	meta := payload.ListMeta
	if meta == nil || !meta.Truncated || meta.Returned != 2 {
		t.Fatalf("list_meta = %+v, want truncated page of 2", meta)
	}
	if meta.Total != nil {
		t.Errorf("list_meta.total = %d, want absent (source not drained)", *meta.Total)
	}
	if meta.Cap != 0 {
		t.Errorf("list_meta.cap = %d, want 0 (the user's limit, not the safety cap, was the bound)", meta.Cap)
	}
	// The continuation derives from argv (filters would survive) and doubles
	// the limit — the total is unknown here (the source was not drained), so
	// a doubled limit is the honest next step rather than --limit 0.
	if meta.Continue != "gcx irm oncall alert-groups list --limit 4" {
		t.Errorf("list_meta.continue = %q, want doubled-limit continuation", meta.Continue)
	}
	want := "hint: showing first 2; more results are available. See more with: gcx irm oncall alert-groups list --limit 4"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr missing truncation hint %q:\n%s", want, stderr)
	}
}

// TestAlertGroupList_RichPath_LimitZeroIsComplete locks the grafana/gcx#1157
// fix: `--limit 0` drains every page, so the fetch reports no more pages and
// the output carries NO list_meta — which per the contract means "this IS the
// complete result set". The old client-side safety cap is gone, so no
// cap-variant hint may appear either.
func TestAlertGroupList_RichPath_LimitZeroIsComplete(t *testing.T) {
	fake := &fakeRichListAPI{items: rawAlertGroups(3), page: alertGroupPageInfo{}}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "0")

	if fake.gotLimit != 0 {
		t.Errorf("wire limit = %d, want 0", fake.gotLimit)
	}
	if len(payload.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(payload.Items))
	}
	if payload.ListMeta != nil {
		t.Errorf("list_meta = %+v, want absent (--limit 0 drains, so the page IS the complete set)", payload.ListMeta)
	}
	if strings.Contains(stderr, "safety cap") {
		t.Errorf("stderr carries a cap-variant hint but the cap is gone:\n%s", stderr)
	}
	if strings.Contains(stderr, "showing first") {
		t.Errorf("unexpected truncation hint on stderr:\n%s", stderr)
	}
}

func TestAlertGroupList_RichPath_Complete(t *testing.T) {
	fake := &fakeRichListAPI{items: rawAlertGroups(2), page: alertGroupPageInfo{}}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "50")

	if payload.ListMeta != nil {
		t.Errorf("list_meta = %+v, want absent for a complete result set", payload.ListMeta)
	}
	if strings.Contains(stderr, "showing first") {
		t.Errorf("unexpected truncation hint on stderr:\n%s", stderr)
	}
}

func TestAlertGroupList_LegacyPath_OverFetchDetectsTruncation(t *testing.T) {
	items := []AlertGroup{
		{PK: "AG1", AlertsCount: 1},
		{PK: "AG2", AlertsCount: 1},
		{PK: "AG3", AlertsCount: 1},
		{PK: "AG4", AlertsCount: 1},
	}
	fake := &fakeLegacyListAPI{items: items}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "3")

	if fake.gotCfg.Limit != 4 {
		t.Errorf("wire limit = %d, want 4 (over-fetch by one)", fake.gotCfg.Limit)
	}
	if len(payload.Items) != 3 {
		t.Fatalf("items = %d, want 3 (display limit)", len(payload.Items))
	}
	meta := payload.ListMeta
	if meta == nil || !meta.Truncated || meta.Returned != 3 || meta.Total != nil || meta.Cap != 0 {
		t.Fatalf("list_meta = %+v, want truncated page of 3 with unknown total and no cap", meta)
	}
	want := "hint: showing first 3; more results are available. See more with: gcx irm oncall alert-groups list --limit 6"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr missing truncation hint %q:\n%s", want, stderr)
	}
}

func TestAlertGroupList_LegacyPath_LimitZeroDrainsFully(t *testing.T) {
	items := []AlertGroup{{PK: "AG1"}, {PK: "AG2"}, {PK: "AG3"}}
	fake := &fakeLegacyListAPI{items: items}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "0")

	if fake.gotCfg.Limit != 0 {
		t.Errorf("wire limit = %d, want 0 (unlimited — no cap on the legacy path)", fake.gotCfg.Limit)
	}
	if len(payload.Items) != 3 {
		t.Fatalf("items = %d, want all 3", len(payload.Items))
	}
	if payload.ListMeta != nil {
		t.Errorf("list_meta = %+v, want absent for --limit 0 (drained source)", payload.ListMeta)
	}
	if strings.Contains(stderr, "showing first") {
		t.Errorf("unexpected truncation hint on stderr:\n%s", stderr)
	}
}

// TestAlertGroupList_LegacyPath_NotesUnsupportedFilters: the SA-token path
// speaks the OnCall public API, which has none of the internal API's option
// filters and only a lower bound on started_at. Every dropped filter must be
// named in the note so the user knows the result set is broader than asked
// for — silently ignoring them would be a wrong answer, not a partial one.
func TestAlertGroupList_LegacyPath_NotesUnsupportedFilters(t *testing.T) {
	fake := &fakeLegacyListAPI{items: []AlertGroup{{PK: "AG1"}}}
	_, stderr := runAlertGroupList(t, fake,
		"--escalation-chain", "EC1",
		"--acknowledged-by", "U1",
		"--resolved-by", "U2",
		"--from", "2026-01-01T00:00:00Z",
		"--to", "2026-01-31T00:00:00Z",
		"--resolved-from", "2026-01-01T00:00:00Z",
		"--resolved-to", "2026-01-31T00:00:00Z",
	)

	for _, want := range []string{
		"--escalation-chain", "--acknowledged-by", "--resolved-by",
		"--to", "--resolved-from", "--resolved-to",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q in the unsupported-filter note:\n%s", want, stderr)
		}
	}
	// --from survives: the public API does understand a started_at lower
	// bound, so it must be applied rather than dropped.
	if strings.Contains(stderr, "does not honor: --from") {
		t.Errorf("--from reported unsupported, but it maps onto started_after:\n%s", stderr)
	}
	if fake.gotCfg.StartedAfter == nil {
		t.Fatal("StartedAfter = nil, want --from applied as the public API's lower bound")
	}
	if got := fake.gotCfg.StartedAfter.UTC().Format(time.RFC3339); got != "2026-01-01T00:00:00Z" {
		t.Errorf("StartedAfter = %s, want 2026-01-01T00:00:00Z", got)
	}
}

func TestAlertGroupList_LegacyPath_ShortPageIsComplete(t *testing.T) {
	items := []AlertGroup{{PK: "AG1"}, {PK: "AG2"}}
	fake := &fakeLegacyListAPI{items: items}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "3")

	if len(payload.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(payload.Items))
	}
	if payload.ListMeta != nil {
		t.Errorf("list_meta = %+v, want absent: no spare row means no more data", payload.ListMeta)
	}
	if strings.Contains(stderr, "showing first") {
		t.Errorf("unexpected truncation hint on stderr:\n%s", stderr)
	}
}
