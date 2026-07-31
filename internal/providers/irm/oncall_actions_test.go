//nolint:testpackage // white-box tests require access to unexported IRM types and helpers
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	fail "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
)

// fakeOnCallAPI is a minimal stub used to drive runAcknowledge in tests
// without spinning up an httptest server. It embeds OnCallAPI so any
// method we don't override returns a nil-interface panic if called — the
// acknowledge code path only needs GetAlertGroup + AcknowledgeAlertGroup.
type fakeOnCallAPI struct {
	OnCallAPI

	getAlertGroupFn         func(context.Context, string) (*AlertGroup, error)
	acknowledgeAlertGroupFn func(context.Context, string) error
	calls                   []string
}

func (f *fakeOnCallAPI) GetAlertGroup(ctx context.Context, id string) (*AlertGroup, error) {
	if f.getAlertGroupFn != nil {
		return f.getAlertGroupFn(ctx, id)
	}
	return &AlertGroup{PK: id, Status: float64(0)}, nil // default: firing
}

func (f *fakeOnCallAPI) AcknowledgeAlertGroup(ctx context.Context, id string) error {
	f.calls = append(f.calls, id)
	if f.acknowledgeAlertGroupFn != nil {
		return f.acknowledgeAlertGroupFn(ctx, id)
	}
	return nil
}

// fakeLoader implements OnCallConfigLoader by returning the fake client.
type fakeLoader struct {
	client OnCallAPI
}

func (l *fakeLoader) LoadOnCallClient(_ context.Context) (OnCallAPI, string, error) {
	return l.client, "stacks-test", nil
}

// runAck drives runAcknowledge with captured stdout/stderr. Returns the
// captured streams, the exit code carried by a returned EmittedError (-1
// when the runner returned nil or a non-emitted error), and the returned
// error (if any).
func runAck(t *testing.T, args []string, opts *alertGroupActionVerbOpts, client OnCallAPI) (string, string, int, error) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetContext(context.Background())

	// Tests bypass RunE/setup/BindFlags, so the IO codec system is not
	// initialised by Cobra. Seed it with JSON output for structured assertion
	// in tests, and route hint diagnostics to the captured stderr buffer.
	opts.IO.OutputFormat = "json"
	opts.IO.ErrWriter = stderr

	loader := &fakeLoader{client: client}
	err := runActionVerb(cmd, args, opts, loader, acknowledgeVerb())

	// Partial failures surface as an EmittedError sentinel (the result
	// document is already on stdout); mirror the old exit-code capture by
	// extracting the carried code.
	exitCode := -1
	var emitted *fail.EmittedError
	if errors.As(err, &emitted) {
		exitCode = emitted.Code
	}
	return stdout.String(), stderr.String(), exitCode, err
}

// resetAgentMode resets the agent-mode detection between tests.
func resetAgentMode(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
}

// topLevelKeys returns the sorted set of top-level keys in a JSON document.
func topLevelKeys(t *testing.T, raw string) map[string]bool {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("invalid JSON: %v\nraw=%s", err, raw)
	}
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// --- Guardrail tests ---

func TestRunAcknowledge_RequiresIDOrFilter(t *testing.T) {
	resetAgentMode(t)
	stdout, _, exit, err := runAck(t, nil, &alertGroupActionVerbOpts{}, &fakeOnCallAPI{})
	if exit != -1 {
		t.Errorf("did not expect os.Exit; got %d", exit)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout; got %q", stdout)
	}
	if err == nil {
		t.Fatal("expected runAcknowledge to return an error")
	}
	var de *fail.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("expected DetailedError, got %T: %v", err, err)
	}
	if de.ExitCode == nil || *de.ExitCode != 2 {
		t.Errorf("expected exit 2; got %v", de.ExitCode)
	}
	if !strings.Contains(de.Summary, "argument or filter flag required") {
		t.Errorf("unexpected summary: %q", de.Summary)
	}
}

func TestRunAcknowledge_RejectsIDPlusFilter(t *testing.T) {
	resetAgentMode(t)
	opts := &alertGroupActionVerbOpts{Teams: []string{"prod-sre"}}
	_, _, _, err := runAck(t, []string{"I123"}, opts, &fakeOnCallAPI{}) //nolint:dogsled
	if err == nil {
		t.Fatal("expected error for id+filter combination")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually exclusive error; got: %v", err)
	}
}

// --- Single-target tests (locked shape: {action, target, changed}) ---

func TestRunAcknowledge_SingleTarget_Changes(t *testing.T) {
	resetAgentMode(t)
	fake := &fakeOnCallAPI{
		getAlertGroupFn: func(_ context.Context, id string) (*AlertGroup, error) {
			return &AlertGroup{PK: id, Status: float64(0)}, nil // firing
		},
	}
	stdout, stderr, exit, err := runAck(t, []string{"IABC"}, &alertGroupActionVerbOpts{}, fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exit != -1 {
		t.Errorf("unexpected exit code %d (single-target success should return cleanly)", exit)
	}
	// Progress line must appear on stderr (TTY mode).
	if !strings.Contains(stderr, "IABC") {
		t.Errorf("expected alert-group ID in stderr progress output; got %q", stderr)
	}

	// Locked shape: top-level keys MUST be exactly {action, target, changed}.
	keys := topLevelKeys(t, stdout)
	for _, must := range []string{"action", "target", "changed"} {
		if !keys[must] {
			t.Errorf("missing required top-level key %q in %s", must, stdout)
		}
	}
	for _, forbidden := range []string{"summary", "failures", "targets", "error"} {
		if keys[forbidden] {
			t.Errorf("unexpected top-level key %q in single-target shape: %s", forbidden, stdout)
		}
	}

	var got singleMutationResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout=%s", err, stdout)
	}
	if got.Action != "acknowledge" {
		t.Errorf("action: got %q want %q", got.Action, "acknowledge")
	}
	if got.Target.AlertGroupID != "IABC" {
		t.Errorf("target.alertGroupId: got %q want %q", got.Target.AlertGroupID, "IABC")
	}
	if got.Changed == nil || !*got.Changed {
		t.Errorf("expected changed:true; got %v", got.Changed)
	}
	if got.Error != nil {
		t.Errorf("expected nil error; got %+v", got.Error)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "IABC" {
		t.Errorf("expected 1 ack call for IABC; got %v", fake.calls)
	}
}

func TestRunAcknowledge_SingleTarget_IdempotentNoOp(t *testing.T) {
	resetAgentMode(t)
	fake := &fakeOnCallAPI{
		getAlertGroupFn: func(_ context.Context, id string) (*AlertGroup, error) {
			return &AlertGroup{PK: id, Status: float64(1)}, nil // already acknowledged
		},
	}
	stdout, _, exit, _ := runAck(t, []string{"IDONE"}, &alertGroupActionVerbOpts{}, fake)
	if exit != -1 {
		t.Errorf("unexpected exit %d (idempotent should be clean exit)", exit)
	}

	keys := topLevelKeys(t, stdout)
	for _, must := range []string{"action", "target", "changed"} {
		if !keys[must] {
			t.Errorf("missing required top-level key %q in %s", must, stdout)
		}
	}
	for _, forbidden := range []string{"summary", "failures", "targets", "error"} {
		if keys[forbidden] {
			t.Errorf("unexpected top-level key %q in single-target shape: %s", forbidden, stdout)
		}
	}

	var got singleMutationResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout JSON: %v\nstdout=%s", err, stdout)
	}
	if got.Target.AlertGroupID != "IDONE" {
		t.Errorf("target.alertGroupId: got %q want %q", got.Target.AlertGroupID, "IDONE")
	}
	if got.Changed == nil || *got.Changed {
		t.Errorf("expected changed:false on idempotent path; got %v", got.Changed)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected zero acknowledge calls (idempotent skip); got %v", fake.calls)
	}
}

func TestRunAcknowledge_SingleTarget_ApplyError(t *testing.T) {
	resetAgentMode(t)
	fake := &fakeOnCallAPI{
		getAlertGroupFn: func(_ context.Context, id string) (*AlertGroup, error) {
			return &AlertGroup{PK: id, Status: float64(0)}, nil
		},
		acknowledgeAlertGroupFn: func(_ context.Context, _ string) error {
			return errors.New("backend boom")
		},
	}
	stdout, _, exit, err := runAck(t, []string{"IFAIL"}, &alertGroupActionVerbOpts{}, fake)
	if exit != fail.ExitPartialFailure {
		t.Errorf("expected exit %d (ExitPartialFailure); got %d", fail.ExitPartialFailure, exit)
	}
	var emitted *fail.EmittedError
	if !errors.As(err, &emitted) {
		t.Fatalf("expected *gcxerrors.EmittedError sentinel (result already on stdout); got %T: %v", err, err)
	}

	// Single-target failure path (Option A): {action, target, error} — NO `changed`.
	keys := topLevelKeys(t, stdout)
	for _, must := range []string{"action", "target", "error"} {
		if !keys[must] {
			t.Errorf("missing required top-level key %q in failure shape: %s", must, stdout)
		}
	}
	for _, forbidden := range []string{"summary", "failures", "targets", "changed"} {
		if keys[forbidden] {
			t.Errorf("unexpected top-level key %q in single-target failure shape: %s", forbidden, stdout)
		}
	}

	var got singleMutationResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout JSON: %v\nstdout=%s", err, stdout)
	}
	if got.Error == nil || got.Error.Code != "acknowledge_failed" {
		t.Errorf("expected error envelope with code=acknowledge_failed; got %+v", got.Error)
	}
	if got.Changed != nil {
		t.Errorf("expected changed to be omitted on failure; got %v", got.Changed)
	}
}

// --- Bulk-by-filter tests (locked shape: {action, summary, failures}) ---

// fakeBulkOnCallAPI drives the full bulk-by-filter path: embedding
// fakeOnCallAPI supplies the single-target surface (AcknowledgeAlertGroup +
// call recording), and the RichAlertGroupReader methods below let
// resolveBulkTargets enumerate targets without a live OnCall backend. Only
// ListAlertGroupsRaw is meaningfully implemented — the remaining reader
// methods are unused by the bulk action path.
type fakeBulkOnCallAPI struct {
	fakeOnCallAPI

	items []json.RawMessage
}

func (f *fakeBulkOnCallAPI) ListAlertGroupsRaw(context.Context, alertGroupListFilters, int) ([]json.RawMessage, alertGroupPageInfo, error) {
	return f.items, alertGroupPageInfo{}, nil
}

func (f *fakeBulkOnCallAPI) GetAlertGroupRich(context.Context, string) (*AlertGroupRich, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeBulkOnCallAPI) ListAlertIDs(context.Context, string, int) ([]string, int, error) {
	return nil, 0, nil
}

func (f *fakeBulkOnCallAPI) GetAlertRich(context.Context, string) (*alertAPI, *AlertRich, error) {
	return nil, nil, errors.New("not implemented")
}

func (f *fakeBulkOnCallAPI) ResolveTeams(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// TestRunAcknowledge_Bulk_PartialFailure drives runActionVerbBulk end-to-end
// with a mixed target set: when some targets fail, the complete bulk envelope
// must still land on stdout as one JSON value, and the runner must return the
// *gcxerrors.EmittedError sentinel carrying ExitPartialFailure (the result is
// already emitted — reportError must not render a second error document).
func TestRunAcknowledge_Bulk_PartialFailure(t *testing.T) {
	resetAgentMode(t)
	fake := &fakeBulkOnCallAPI{
		items: []json.RawMessage{
			json.RawMessage(`{"pk":"I1","status":0}`), // firing → succeeds
			json.RawMessage(`{"pk":"I2","status":0}`), // firing → API call fails
			json.RawMessage(`{"pk":"I3","status":1}`), // already acknowledged → skipped
		},
	}
	fake.acknowledgeAlertGroupFn = func(_ context.Context, id string) error {
		if id == "I2" {
			return errors.New("backend boom")
		}
		return nil
	}

	// --force skips the >1-target confirmation prompt (stdin is empty).
	opts := &alertGroupActionVerbOpts{Teams: []string{"prod-sre"}, Force: true}
	stdout, _, exit, err := runAck(t, nil, opts, fake)

	if exit != fail.ExitPartialFailure {
		t.Errorf("expected exit %d (ExitPartialFailure); got %d", fail.ExitPartialFailure, exit)
	}
	var emitted *fail.EmittedError
	if !errors.As(err, &emitted) {
		t.Fatalf("expected *gcxerrors.EmittedError sentinel (result already on stdout); got %T: %v", err, err)
	}
	if emitted.Code != fail.ExitPartialFailure {
		t.Errorf("EmittedError.Code: got %d want %d", emitted.Code, fail.ExitPartialFailure)
	}

	// Locked bulk shape: top-level keys MUST be exactly {action, summary,
	// failures}. topLevelKeys unmarshals the whole stream, so trailing data
	// (a second JSON value) would fail the assertion too.
	keys := topLevelKeys(t, stdout)
	for _, must := range []string{"action", "summary", "failures"} {
		if !keys[must] {
			t.Errorf("missing required top-level key %q in %s", must, stdout)
		}
	}
	for _, forbidden := range []string{"target", "changed", "targets", "error"} {
		if keys[forbidden] {
			t.Errorf("unexpected top-level key %q in bulk shape: %s", forbidden, stdout)
		}
	}

	var got bulkMutationResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout=%s", err, stdout)
	}
	if got.Action != "acknowledge" {
		t.Errorf("action: got %q want acknowledge", got.Action)
	}
	if got.Summary.Matched != 3 || got.Summary.Succeeded != 1 ||
		got.Summary.Skipped != 1 || got.Summary.Failed != 1 {
		t.Errorf("summary: got %+v want matched=3 succeeded=1 skipped=1 failed=1", got.Summary)
	}
	assertSummaryAddsUp(t, got.Summary)
	if len(got.Failures) != 1 {
		t.Fatalf("expected 1 failure entry; got %d (%+v)", len(got.Failures), got.Failures)
	}
	if got.Failures[0].Target.AlertGroupID != "I2" {
		t.Errorf("failure target: got %q want I2", got.Failures[0].Target.AlertGroupID)
	}
	if got.Failures[0].Error.Code != "acknowledge_failed" {
		t.Errorf("failure error code: got %q want acknowledge_failed", got.Failures[0].Error.Code)
	}

	// I3 is skipped (idempotent), so only I1 and I2 reach the API.
	if len(fake.calls) != 2 || fake.calls[0] != "I1" || fake.calls[1] != "I2" {
		t.Errorf("expected ack calls [I1 I2]; got %v", fake.calls)
	}
}

// TestBulkResult_AllSucceed verifies the bulk envelope for the all-succeed
// case via direct construction of the result-builder loop. This complements
// the live shape test on ops.
func TestBulkResult_AllSucceed(t *testing.T) {
	results := []ackOutcome{
		{id: "I1", changed: true},
		{id: "I2", changed: true},
		{id: "I3", changed: true},
	}
	env := buildBulkEnvelopeForTest(results)

	if env.Action != "acknowledge" {
		t.Errorf("action: got %q want acknowledge", env.Action)
	}
	if env.Summary.Matched != 3 || env.Summary.Succeeded != 3 ||
		env.Summary.Skipped != 0 || env.Summary.Failed != 0 {
		t.Errorf("summary: got %+v want matched=3 succeeded=3 skipped=0 failed=0", env.Summary)
	}
	assertSummaryAddsUp(t, env.Summary)
	if len(env.Failures) != 0 {
		t.Errorf("expected empty failures; got %+v", env.Failures)
	}

	// Verify failures is `[]` not `null` on the wire.
	b, _ := json.Marshal(env) //nolint:errchkjson // stable test struct
	if !strings.Contains(string(b), `"failures":[]`) {
		t.Errorf("failures must marshal as [] (not null) for predictable parsing: %s", string(b))
	}
}

func TestBulkResult_Mixed(t *testing.T) {
	results := []ackOutcome{
		{id: "I1", changed: true},  // succeeded
		{id: "I2", changed: false}, // skipped (idempotent)
		{id: "I3", err: &mutationTargetError{Code: "acknowledge_failed", Message: "x"}}, // failed
		{id: "I4", changed: true},  // succeeded
		{id: "I5", changed: false}, // skipped
	}
	env := buildBulkEnvelopeForTest(results)

	if env.Summary.Matched != 5 {
		t.Errorf("matched: got %d want 5", env.Summary.Matched)
	}
	if env.Summary.Succeeded != 2 {
		t.Errorf("succeeded: got %d want 2", env.Summary.Succeeded)
	}
	if env.Summary.Skipped != 2 {
		t.Errorf("skipped: got %d want 2", env.Summary.Skipped)
	}
	if env.Summary.Failed != 1 {
		t.Errorf("failed: got %d want 1", env.Summary.Failed)
	}
	assertSummaryAddsUp(t, env.Summary)

	// Failures enumerates only the failed target — successes/skips are counts only.
	if len(env.Failures) != 1 {
		t.Fatalf("expected 1 failure entry; got %d (%+v)", len(env.Failures), env.Failures)
	}
	if env.Failures[0].Target.AlertGroupID != "I3" {
		t.Errorf("failure target: got %q want I3", env.Failures[0].Target.AlertGroupID)
	}
	if env.Failures[0].Error.Code != "acknowledge_failed" {
		t.Errorf("failure error code: got %q want acknowledge_failed", env.Failures[0].Error.Code)
	}
}

// buildBulkEnvelopeForTest mirrors the result-roll-up logic in
// runAcknowledgeBulk. Kept in the test file — and exercising it directly
// rather than re-exporting — keeps the production runner tightly scoped.
func buildBulkEnvelopeForTest(results []ackOutcome) bulkMutationResult {
	summary := mutationSummary{Matched: len(results)}
	failures := []mutationFailure{}
	for _, r := range results {
		switch {
		case r.err != nil:
			summary.Failed++
			failures = append(failures, mutationFailure{
				Target: irmTarget{AlertGroupID: r.id},
				Error:  *r.err,
			})
		case r.changed:
			summary.Succeeded++
		default:
			summary.Skipped++
		}
	}
	return bulkMutationResult{
		Action:   "acknowledge",
		Summary:  summary,
		Failures: failures,
	}
}

// assertSummaryAddsUp enforces the locked-contract invariant
// matched == succeeded + skipped + failed.
func assertSummaryAddsUp(t *testing.T, s mutationSummary) {
	t.Helper()
	if s.Matched != s.Succeeded+s.Skipped+s.Failed {
		t.Errorf("summary invariant violated: matched=%d succeeded=%d skipped=%d failed=%d (sum=%d)",
			s.Matched, s.Succeeded, s.Skipped, s.Failed, s.Succeeded+s.Skipped+s.Failed)
	}
}

// --- Shape mutual-exclusivity tests ---

// TestSingleMutationResult_JSONShape verifies the on-the-wire JSON for the
// single-target envelope omits the `error` field on success and the
// `changed` field on failure.
func TestSingleMutationResult_JSONShape(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		env := singleMutationResult{
			Action:  "acknowledge",
			Target:  irmTarget{AlertGroupID: "I1"},
			Changed: boolPtr(true), //nolint:modernize
		}
		b, _ := json.Marshal(env)
		s := string(b)
		if !strings.Contains(s, `"changed":true`) {
			t.Errorf("expected changed:true in JSON; got %s", s)
		}
		if strings.Contains(s, `"error"`) {
			t.Errorf("error field must be omitted on success; got %s", s)
		}
	})
	t.Run("idempotent", func(t *testing.T) {
		env := singleMutationResult{
			Action:  "acknowledge",
			Target:  irmTarget{AlertGroupID: "I1"},
			Changed: boolPtr(false), //nolint:modernize
		}
		b, _ := json.Marshal(env)
		s := string(b)
		if !strings.Contains(s, `"changed":false`) {
			t.Errorf("changed:false MUST be present (not omitempty) on idempotent path; got %s", s)
		}
	})
	t.Run("failure", func(t *testing.T) {
		env := singleMutationResult{
			Action: "acknowledge",
			Target: irmTarget{AlertGroupID: "I1"},
			Error:  &mutationTargetError{Code: "x", Message: "y"},
		}
		b, _ := json.Marshal(env)
		s := string(b)
		if strings.Contains(s, `"changed"`) {
			t.Errorf("changed must be omitted on failure; got %s", s)
		}
		if !strings.Contains(s, `"error"`) {
			t.Errorf("error field must be present on failure; got %s", s)
		}
	})
}

// TestBulkMutationResult_JSONShape verifies the bulk envelope's required
// fields are always present (matched/succeeded/skipped/failed; failures: []
// not null).
func TestBulkMutationResult_JSONShape(t *testing.T) {
	env := bulkMutationResult{
		Action:   "acknowledge",
		Summary:  mutationSummary{Matched: 0, Succeeded: 0, Skipped: 0, Failed: 0},
		Failures: []mutationFailure{},
	}
	b, _ := json.Marshal(env) //nolint:errchkjson // stable test struct
	s := string(b)

	for _, want := range []string{`"matched":0`, `"succeeded":0`, `"skipped":0`, `"failed":0`, `"failures":[]`} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in bulk envelope JSON; got %s", want, s)
		}
	}
	for _, forbidden := range []string{`"target":`, `"changed":`, `"targets":`} {
		if strings.Contains(s, forbidden) {
			t.Errorf("bulk envelope must not contain %q at top level; got %s", forbidden, s)
		}
	}
}

// --- toListFilters tests ---

func TestToListFilters_Defaults(t *testing.T) {
	opts := &alertGroupActionVerbOpts{Teams: []string{"prod-sre"}}
	f, err := opts.toListFilters()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Default: status filter excludes resolved (no 2 in the slice), is_root=true.
	if len(f.Statuses) != 3 {
		t.Errorf("expected 3 default statuses (firing+ack+silenced); got %v", f.Statuses)
	}
	if f.IsRoot == nil || !*f.IsRoot {
		t.Errorf("expected is_root=true by default; got %v", f.IsRoot)
	}
	if len(f.Teams) != 1 || f.Teams[0] != "prod-sre" {
		t.Errorf("teams: got %v", f.Teams)
	}
}

func TestToListFilters_AllBypassesDefaults(t *testing.T) {
	opts := &alertGroupActionVerbOpts{All: true}
	f, _ := opts.toListFilters()
	if len(f.Statuses) != 0 {
		t.Errorf("--all should drop status filter; got %v", f.Statuses)
	}
	if f.IsRoot != nil {
		t.Errorf("--all should drop is_root filter; got %v", f.IsRoot)
	}
}

func TestToListFilters_ExplicitState(t *testing.T) {
	opts := &alertGroupActionVerbOpts{States: []string{"firing"}}
	f, err := opts.toListFilters()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(f.Statuses) != 1 || f.Statuses[0] != 0 {
		t.Errorf("expected [0] (firing only); got %v", f.Statuses)
	}
}

func TestToListFilters_InvalidState(t *testing.T) {
	opts := &alertGroupActionVerbOpts{States: []string{"bogus"}}
	_, err := opts.toListFilters()
	if err == nil {
		t.Fatal("expected error for invalid --state")
	}
	if !strings.Contains(err.Error(), "invalid --state") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- HasAnyFilter tests ---

func TestHasAnyFilter(t *testing.T) {
	cases := []struct {
		name string
		opts *alertGroupActionVerbOpts
		want bool
	}{
		{"empty", &alertGroupActionVerbOpts{}, false},
		{"force only", &alertGroupActionVerbOpts{Force: true}, false},
		{"max-age", &alertGroupActionVerbOpts{MaxAge: "1h"}, true},
		{"team", &alertGroupActionVerbOpts{Teams: []string{"a"}}, true},
		{"state", &alertGroupActionVerbOpts{States: []string{"firing"}}, true},
		{"integration", &alertGroupActionVerbOpts{Integrations: []string{"x"}}, true},
		{"mine", &alertGroupActionVerbOpts{Mine: true}, true},
		{"all", &alertGroupActionVerbOpts{All: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.hasAnyFilter(); got != tc.want {
				t.Errorf("got %v want %v for %+v", got, tc.want, tc.opts)
			}
		})
	}
}
