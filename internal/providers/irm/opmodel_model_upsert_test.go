//nolint:testpackage // exercises the provider command against in-memory and wire fakes
package irm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
)

type modelStore struct {
	fakeOpModel

	body   containerBody
	writes []any
	failAt int
	minted int
}

func (s *modelStore) write(req any) (string, string, error) {
	s.writes = append(s.writes, req)
	s.minted++
	if len(s.writes) == s.failAt {
		return "", "", errors.New("scripted failure")
	}
	s.body.Revision = strconv.Itoa(s.minted)
	return strconv.Itoa(s.minted), s.body.Revision, nil
}

func (s *modelStore) GetContainer(context.Context, getContainerRequest) (*containerBody, error) {
	body := s.body
	return &body, nil
}

func (s *modelStore) CreateObservation(_ context.Context, req createObservationRequest) (*createObservationResponse, error) {
	n, rev, err := s.write(req)
	if err != nil {
		return nil, err
	}
	s.body.Observations = append(s.body.Observations, observationDTO{ID: "obs-" + n, Text: req.Text, SourceRef: req.SourceRef, ContentVersion: 1})
	return &createObservationResponse{ObservationID: "obs-" + n, Revision: rev}, nil
}

func (s *modelStore) CreateClaim(_ context.Context, req createClaimRequest) (*createClaimResponse, error) {
	n, rev, err := s.write(req)
	if err != nil {
		return nil, err
	}
	s.body.Claims = append(s.body.Claims, claimDTO{ID: "clm-" + n, Type: req.Type, Text: req.Text, ContentVersion: 1})
	return &createClaimResponse{ClaimID: "clm-" + n, Revision: rev}, nil
}

func (s *modelStore) AttachEvidence(_ context.Context, req attachEvidenceRequest) (*attachEvidenceResponse, error) {
	n, rev, err := s.write(req)
	if err != nil {
		return nil, err
	}
	s.body.EvidenceLinks = append(s.body.EvidenceLinks, evidenceDTO{ID: "ev-" + n, SourceID: req.SourceID, TargetID: req.TargetID, Stance: req.Stance, SourceVersion: 1, TargetVersion: 1})
	return &attachEvidenceResponse{EvidenceID: "ev-" + n, Revision: rev}, nil
}

func (s *modelStore) PromoteToAction(_ context.Context, req promoteToActionRequest) (*promoteToActionResponse, error) {
	n, rev, err := s.write(req)
	if err != nil {
		return nil, err
	}
	s.body.Actions = append(s.body.Actions, actionDTO{ID: "act-" + n, Text: req.Text, ContentVersion: 1, Origin: &originOutput{ObservationID: req.ObservationID, ContentVersion: 1}})
	return &promoteToActionResponse{ActionID: "act-" + n, Revision: rev}, nil
}

func (s *modelStore) CreateAction(_ context.Context, req createActionRequest) (*createActionResponse, error) {
	n, rev, err := s.write(req)
	if err != nil {
		return nil, err
	}
	s.body.Actions = append(s.body.Actions, actionDTO{ID: "act-" + n, Text: req.Text, ContentVersion: 1})
	return &createActionResponse{ActionID: "act-" + n, Revision: rev}, nil
}

func (s *modelStore) describeWrites(t *testing.T) []string {
	t.Helper()
	out := make([]string, 0, len(s.writes))
	for _, req := range s.writes {
		var line string
		var actor opModelActorInput
		switch r := req.(type) {
		case createObservationRequest:
			line, actor = fmt.Sprintf("observation %q source=%q", r.Text, r.SourceRef), r.Actor
		case createClaimRequest:
			line, actor = fmt.Sprintf("%s %q", r.Type, r.Text), r.Actor
		case attachEvidenceRequest:
			line, actor = fmt.Sprintf("link %s %s %s rationale=%q", r.SourceID, r.TargetID, r.Stance, r.Rationale), r.Actor
		case promoteToActionRequest:
			line, actor = fmt.Sprintf("promote %s %q", r.ObservationID, r.Text), r.Actor
		case createActionRequest:
			line, actor = fmt.Sprintf("action %q", r.Text), r.Actor
		default:
			t.Fatalf("unexpected write %T", req)
		}
		if actor != (opModelActorInput{Kind: "agent", ID: "codex", DisplayName: "Codex"}) {
			t.Errorf("write %q carries actor %+v", line, actor)
		}
		out = append(out, line)
	}
	return out
}

type storeLoader struct {
	api   OpModelAPI
	loads int
}

func (l *storeLoader) LoadOpModel(context.Context, time.Duration) (OpModelAPI, error) {
	l.loads++
	return l.api, nil
}

func runUpsert(t *testing.T, loader *storeLoader, draft string, extra ...string) (map[string]any, string, error) {
	t.Helper()
	setAgentMode(t, true)
	args := append([]string{"containers", "upsert-model", "INC-1", "-f", "-", "--actor-kind", "agent", "--actor-id", "codex", "--actor-name", "Codex"}, extra...)
	stdout, stderr, err := runNounCmd(t, func() *cobra.Command { return newOpModelCmd(loader) }, draft, args...)
	if stdout == "" {
		return nil, stderr, err
	}
	return decodeSingleJSONValue(t, stdout), stderr, err
}

func detailsOf(t *testing.T, err error) string {
	t.Helper()
	requireUsageExit(t, err)
	var detailed *fail.DetailedError
	if !errors.As(err, &detailed) {
		t.Fatalf("not a detailed error: %v", err)
	}
	return detailed.Summary + "\n" + detailed.Details
}

const incidentDraft = `{
 "entities": [
  {"ref": "a1", "type": "action", "text": "Raise the pool size", "prompted_by": "o1"},
  {"ref": "h1", "type": "hypothesis", "text": "The pool is exhausted"},
  {"ref": "o1", "type": "observation", "text": "p99 latency doubled at 14:02", "source_ref": "grafana: d/x"},
  {"ref": "i1", "type": "impact", "text": "Checkout fails for some users"},
  {"ref": "a2", "type": "action", "text": "Page the database owner"},
  {"ref": "o2", "type": "observation", "text": "Connection errors rose"}
 ],
 "evidence_links": [
  {"source": "o2", "target": "i1", "stance": "context"},
  {"source": "o1", "target": "h1", "stance": "supports", "rationale": "timing aligns"}
 ]
}`

func TestUpsertModelWritesFreshContainerInOrder(t *testing.T) {
	store := &modelStore{body: containerBody{Container: "INC-1", Revision: "0"}}
	receipt, stderr, err := runUpsert(t, &storeLoader{api: store}, incidentDraft)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`observation "p99 latency doubled at 14:02" source="grafana: d/x"`,
		`observation "Connection errors rose" source=""`,
		`hypothesis "The pool is exhausted"`,
		`impact "Checkout fails for some users"`,
		`link obs-2 clm-4 context rationale=""`,
		`link obs-1 clm-3 supports rationale="timing aligns"`,
		`promote obs-1 "Raise the pool size"`,
		`action "Page the database owner"`,
	}
	if got := store.describeWrites(t); !reflect.DeepEqual(got, want) {
		t.Errorf("writes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	wantReceipt := decodeSingleJSONValue(t, `{
	 "type": "gcx.irm.opmodel.model_upsert", "schema_version": "1", "status": "saved", "container": "INC-1",
	 "model_revision": "0", "revision": "8",
	 "entities": {"o1": "obs-1", "o2": "obs-2", "h1": "clm-3", "i1": "clm-4", "a1": "act-7", "a2": "act-8"},
	 "reused": {},
	 "evidence_links": [
	  {"source": "o2", "target": "i1", "stance": "context", "id": "ev-5"},
	  {"source": "o1", "target": "h1", "stance": "supports", "id": "ev-6"}
	 ],
	 "skipped_links": []
	}`)
	if !reflect.DeepEqual(receipt, wantReceipt) {
		t.Errorf("receipt = %v\nwant %v", receipt, wantReceipt)
	}
	if !strings.Contains(stderr, "gcx irm opmodel containers get INC-1") {
		t.Errorf("missing inspect hint: %q", stderr)
	}
}

func TestUpsertModelReusesExactRepeatsOnly(t *testing.T) {
	store := &modelStore{body: containerBody{
		Container: "INC-1", Revision: "50",
		Observations: []observationDTO{
			{ID: "obs-a", Text: " p99  latency\tdoubled at 14:02. ", SourceRef: "grafana: d/x", ContentVersion: 1},
			{ID: "obs-b", Text: "P99 latency doubled at 14:02", SourceRef: "grafana: d/y", ContentVersion: 1},
		},
		Actions: []actionDTO{{ID: "act-a", Text: "Raise the pool size", ContentVersion: 1, Origin: &originOutput{ObservationID: "obs-a"}}},
	}}
	draft := `{"entities": [
	 {"ref": "a1", "type": "action", "text": "Raise the pool size", "prompted_by": "o1"},
	 {"ref": "o1", "type": "observation", "text": "p99 latency doubled at 14:02", "source_ref": "grafana: d/x"},
	 {"ref": "o2", "type": "observation", "text": "p99 latency doubled at 14:02", "source_ref": "grafana: d/y"}
	], "evidence_links": []}`
	receipt, _, err := runUpsert(t, &storeLoader{api: store}, draft)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := store.describeWrites(t), []string{`observation "p99 latency doubled at 14:02" source="grafana: d/y"`}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %q; want %q", got, want)
	}
	if want := map[string]any{"o1": "obs-a", "a1": "act-a"}; !reflect.DeepEqual(receipt["reused"], want) {
		t.Errorf("reused = %v; want %v", receipt["reused"], want)
	}
	if want := map[string]any{"o1": "obs-a", "a1": "act-a", "o2": "obs-1"}; !reflect.DeepEqual(receipt["entities"], want) {
		t.Errorf("entities = %v; want %v", receipt["entities"], want)
	}
	if store.calls != 0 {
		t.Errorf("existing entities were edited: %v", store.reqs)
	}
}

func linkedContainer() containerBody {
	return containerBody{
		Container: "INC-1", Revision: "50",
		Observations: []observationDTO{{ID: "obs-a", Text: "Connection errors rose", ContentVersion: 1}},
		Claims: []claimDTO{
			{ID: "clm-h", Type: "hypothesis", Text: "The pool is exhausted", ContentVersion: 1},
			{ID: "clm-i", Type: "impact", Text: "Checkout fails", ContentVersion: 2},
		},
		EvidenceLinks: []evidenceDTO{
			{ID: "e-active", SourceID: "obs-a", TargetID: "clm-h", Stance: "supports", SourceVersion: 1, TargetVersion: 1},
			{ID: "e-withdrawn", SourceID: "obs-a", TargetID: "clm-h", Stance: "challenges", SourceVersion: 1, TargetVersion: 1, Withdrawal: &judgmentOutput{}},
			{ID: "e-stale", SourceID: "obs-a", TargetID: "clm-i", Stance: "context", SourceVersion: 1, TargetVersion: 1},
		},
	}
}

const linkedEntities = `
 {"ref": "o1", "type": "observation", "text": "Connection errors rose"},
 {"ref": "h1", "type": "hypothesis", "text": "The pool is exhausted"},
 {"ref": "i1", "type": "impact", "text": "Checkout fails"}`

func TestUpsertModelSkipsActiveLinksAndRewritesStaleOnes(t *testing.T) {
	store := &modelStore{body: linkedContainer()}
	draft := `{"entities": [` + linkedEntities + `], "evidence_links": [
	 {"source": "o1", "target": "h1", "stance": "supports"},
	 {"source": "o1", "target": "i1", "stance": "context"}
	]}`
	receipt, _, err := runUpsert(t, &storeLoader{api: store}, draft)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := store.describeWrites(t), []string{`link obs-a clm-i context rationale=""`}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %q; want %q", got, want)
	}
	if want := []any{map[string]any{"source": "o1", "target": "h1", "stance": "supports", "existing_id": "e-active"}}; !reflect.DeepEqual(receipt["skipped_links"], want) {
		t.Errorf("skipped_links = %v", receipt["skipped_links"])
	}
}

func TestUpsertModelRejectsBeforeAnyWrite(t *testing.T) {
	for _, tc := range []struct {
		name, draft, want string
	}{
		{"withdrawn link", `{"entities": [` + linkedEntities + `], "evidence_links": [{"source": "o1", "target": "h1", "stance": "challenges"}]}`, "e-withdrawn"},
		{"repeated entities", `{"entities": [` + linkedEntities + `, {"ref": "h2", "type": "hypothesis", "text": "The  pool is exhausted."}], "evidence_links": []}`, "h1 and h2 repeat each other"},
		{"existing id of another kind", `{"entities": [{"ref": "o9", "type": "observation", "text": "new", "existing_id": "clm-h"}], "evidence_links": []}`, "o9: no observation clm-h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &modelStore{body: linkedContainer()}
			receipt, _, err := runUpsert(t, &storeLoader{api: store}, tc.draft)
			if details := detailsOf(t, err); !strings.Contains(details, tc.want) {
				t.Errorf("error %q does not mention %q", details, tc.want)
			}
			if len(store.writes) != 0 || receipt != nil {
				t.Errorf("writes=%d receipt=%v", len(store.writes), receipt)
			}
		})
	}
}

func TestUpsertModelStopsAtFirstFailureThenConverges(t *testing.T) {
	store := &modelStore{body: containerBody{Container: "INC-1", Revision: "0"}, failAt: 3}
	receipt, _, err := runUpsert(t, &storeLoader{api: store}, incidentDraft)
	var emitted *fail.EmittedError
	if !errors.As(err, &emitted) || emitted.Code != fail.ExitPartialFailure {
		t.Fatalf("expected exit %d, got %v", fail.ExitPartialFailure, err)
	}
	if len(store.writes) != 3 {
		t.Fatalf("writes after the failure were attempted: %q", store.describeWrites(t))
	}
	wantStopped := decodeSingleJSONValue(t, `{
	 "type": "gcx.irm.opmodel.model_upsert", "schema_version": "1", "status": "stopped", "container": "INC-1",
	 "model_revision": "0", "revision": "2",
	 "entities": {"o1": "obs-1", "o2": "obs-2"},
	 "reused": {}, "evidence_links": [], "skipped_links": [],
	 "stopped_at": {"action": "create-claim", "ref": "h1"},
	 "error": "scripted failure"
	}`)
	if !reflect.DeepEqual(receipt, wantStopped) {
		t.Errorf("stopped receipt = %v\nwant %v", receipt, wantStopped)
	}

	store.failAt, store.writes = 0, nil
	receipt, _, err = runUpsert(t, &storeLoader{api: store}, incidentDraft)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`hypothesis "The pool is exhausted"`,
		`impact "Checkout fails for some users"`,
		`link obs-2 clm-5 context rationale=""`,
		`link obs-1 clm-4 supports rationale="timing aligns"`,
		`promote obs-1 "Raise the pool size"`,
		`action "Page the database owner"`,
	}
	if got := store.describeWrites(t); !reflect.DeepEqual(got, want) {
		t.Errorf("rerun writes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if want := map[string]any{"o1": "obs-1", "o2": "obs-2"}; !reflect.DeepEqual(receipt["reused"], want) {
		t.Errorf("rerun reused = %v", receipt["reused"])
	}

	store.writes = nil
	receipt, _, err = runUpsert(t, &storeLoader{api: store}, incidentDraft)
	if err != nil {
		t.Fatal(err)
	}
	reused, _ := receipt["reused"].(map[string]any)
	skipped, _ := receipt["skipped_links"].([]any)
	if len(store.writes) != 0 || len(reused) != 6 || len(skipped) != 2 {
		t.Errorf("third run must be a no-op: writes=%q receipt=%v", store.describeWrites(t), receipt)
	}
	if n := len(store.body.Observations) + len(store.body.Claims) + len(store.body.Actions) + len(store.body.EvidenceLinks); n != 8 {
		t.Errorf("container holds %d items; want 8 with no duplicates", n)
	}
}

func TestUpsertModelDryRunListsPlannedWrites(t *testing.T) {
	store := &modelStore{body: containerBody{Container: "INC-1", Revision: "0"}}
	receipt, _, err := runUpsert(t, &storeLoader{api: store}, incidentDraft, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.writes) != 0 {
		t.Fatalf("dry run wrote: %q", store.describeWrites(t))
	}
	want := decodeSingleJSONValue(t, `{
	 "type": "gcx.irm.opmodel.model_upsert", "schema_version": "1", "status": "planned", "container": "INC-1",
	 "model_revision": "0", "revision": "0",
	 "entities": {}, "reused": {}, "evidence_links": [], "skipped_links": [],
	 "planned": [
	  {"action": "create-observation", "ref": "o1"},
	  {"action": "create-observation", "ref": "o2"},
	  {"action": "create-claim", "ref": "h1"},
	  {"action": "create-claim", "ref": "i1"},
	  {"action": "attach-evidence-link", "source": "o2", "target": "i1"},
	  {"action": "attach-evidence-link", "source": "o1", "target": "h1"},
	  {"action": "promote-observation-to-action", "ref": "a1"},
	  {"action": "create-action", "ref": "a2"}
	 ]
	}`)
	if !reflect.DeepEqual(receipt, want) {
		t.Errorf("dry-run receipt = %v\nwant %v", receipt, want)
	}
}

func TestUpsertModelValidationReportsEveryViolation(t *testing.T) {
	for _, tc := range []struct {
		name, draft string
		want        []string
	}{
		{
			name: "fields",
			draft: `{"entities": [
			 {"ref": "o1", "type": "observation", "text": "x"},
			 {"ref": "o1", "type": "hypothesis", "text": "y", "source_ref": "s"},
			 {"ref": "c1", "type": "conclusion", "text": " "},
			 {"ref": "a1", "type": "action", "text": "z", "prompted_by": "c1", "existing_id": ""}
			], "evidence_links": [
			 {"source": "o1", "target": "o1", "stance": "bears_on"},
			 {"source": "o1", "target": "o1", "stance": "supports"}
			]}`,
			want: []string{
				"/entities/1/ref:", "/entities/1/source_ref:", "/entities/2/type:", "/entities/2/text:",
				"/entities/3/existing_id:", "/entities/3/prompted_by:",
				"/evidence_links/0/target:", "/evidence_links/0/stance:", "/evidence_links/1/target:", "/evidence_links/1:",
			},
		},
		{name: "missing arrays", draft: `{}`, want: []string{"/entities:", "/evidence_links:"}},
		{name: "unknown field", draft: `{"entities": [], "evidence_links": [], "gaps": []}`, want: []string{"gaps"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loader := &storeLoader{api: &modelStore{}}
			_, _, err := runUpsert(t, loader, tc.draft)
			details := detailsOf(t, err)
			for _, path := range tc.want {
				if !strings.Contains(details, path) {
					t.Errorf("missing %q in:\n%s", path, details)
				}
			}
			if loader.loads != 0 {
				t.Error("an invalid draft reached the client")
			}
		})
	}
}

func TestUpsertModelTreatsOnlyMissingContainerAsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name, readBody string
		readStatus     int
		saved          bool
	}{
		{"missing container", `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"container INC-1 not found","code":404}`, 404, true},
		{"other not found", `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"route not found","code":404}`, 404, false},
		{"server error", `boom`, 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			client := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(tc.readStatus)
					_, _ = io.WriteString(w, tc.readBody)
					return
				}
				posts++
				if r.URL.Path != "/opmodel"+containerBasePath("INC-1")+"/observations" {
					t.Errorf("unexpected write %s", r.URL.Path)
				}
				_, _ = io.WriteString(w, `{"observation_id":"obs-new","revision":"1"}`)
			})
			draft := `{"entities": [{"ref": "o1", "type": "observation", "text": "x"}], "evidence_links": []}`
			receipt, _, err := runUpsert(t, &storeLoader{api: client}, draft)
			if !tc.saved {
				if err == nil || posts != 0 || receipt != nil {
					t.Fatalf("read failure must stop before writes: err=%v posts=%d receipt=%v", err, posts, receipt)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if posts != 1 || receipt["status"] != "saved" || receipt["model_revision"] != nil || receipt["revision"] != "1" {
				t.Errorf("posts=%d receipt=%v", posts, receipt)
			}
		})
	}
}
