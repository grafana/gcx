//nolint:testpackage // exercises the provider command and wire boundaries together
package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/retry"
	"github.com/spf13/cobra"
)

type wireOpModelLoader struct {
	fakeOpModelLoader

	client *irmResourceClient
}

func (l *wireOpModelLoader) LoadOpModel(context.Context, time.Duration) (OpModelAPI, error) {
	l.loads++
	return l.client, nil
}

type opModelWireCase struct {
	name     string
	args     []string
	path     string
	body     string
	response string
}

func coreSemanticsCases() []opModelWireCase {
	return []opModelWireCase{
		{"observation context", []string{"observations", "create", "--text", "observed", "--source-ref", "slack:1", "--observed-start", "2026-08-21T14:00:00Z", "--observed-end", "2026-08-21T14:01:00Z", "--scope", "checkout", "--original-author-source-id", "U1"}, "/observations", `{"text":"observed","source_ref":"slack:1","observed_start":"2026-08-21T14:00:00Z","observed_end":"2026-08-21T14:01:00Z","scope":"checkout","original_author":{"source_id":"U1"}}`, `{"observation_id":"obs-new","revision":"2"}`},
		{"claim creation", []string{"claims", "create", "--type", "hypothesis", "--text", "cause"}, "/claims", `{"type":"hypothesis","text":"cause"}`, `{"claim_id":"claim-new","revision":"2"}`},
		{"action creation", []string{"actions", "create", "--text", "restart", "--expected-result", "healthy", "--assignee-kind", "human", "--assignee-id", "responder", "--assignee-name", "Responder"}, "/actions", `{"text":"restart","expected_result":"healthy","assignee":{"kind":"human","id":"responder","display_name":"Responder"}}`, `{"action_id":"action-new","status":"proposed","revision":"2"}`},
		{"promotion", []string{"observations", "promote", "obs-1", "--to", "hypothesis"}, "/observations/obs-1/promote", `{"type":"hypothesis"}`, `{"claim_id":"claim-new","revision":"2"}`},
		{"action origin", []string{"observations", "promote-to-action", "obs-1", "--text", "restart"}, "/observations/obs-1/promote-to-action", `{"text":"restart"}`, `{"action_id":"action-new","status":"proposed","revision":"2"}`},
		{"text preserves optional content", []string{"entities", "edit-text", "obs-1", "--text", "corrected"}, "/entities/obs-1/edit", `{"text":"corrected"}`, `{"revision":"2"}`},
		{"observation full replacement clears", []string{"observations", "edit", "obs-1", "--text", "corrected"}, "/observations/obs-1/edit", `{"content":{"text":"corrected"}}`, `{"revision":"2"}`},
		{"observation full replacement context", []string{"observations", "edit", "obs-1", "--text", "corrected", "--source-ref", "source", "--scope", "", "--original-author-name", "Author"}, "/observations/obs-1/edit", `{"content":{"text":"corrected","source_ref":"source","scope":"","original_author":{"display_name":"Author"}}}`, `{"revision":"2"}`},
		{"action full replacement clears", []string{"actions", "edit", "act-1", "--text", "corrected"}, "/actions/act-1/edit", `{"content":{"text":"corrected"}}`, `{"revision":"2"}`},
		{"action full replacement empty result", []string{"actions", "edit", "act-1", "--text", "corrected", "--expected-result", ""}, "/actions/act-1/edit", `{"content":{"text":"corrected","expected_result":""}}`, `{"revision":"2"}`},
		{"impact assessment", []string{"claims", "assess", "clm-1", "--impact-assessment", "confirmed", "--reason", "reproduced"}, "/claims/clm-1/assess", `{"impact_assessment":"confirmed","reason":"reproduced"}`, `{"revision":"2"}`},
		{"hypothesis assessment", []string{"claims", "assess", "clm-1", "--hypothesis-assessment", "accepted"}, "/claims/clm-1/assess", `{"hypothesis_assessment":"accepted"}`, `{"revision":"2"}`},
		{"independent recovery", []string{"claims", "recovery", "clm-1", "--recovery", "mitigated", "--reason", "fewer failures; some remain"}, "/claims/clm-1/recovery", `{"recovery":"mitigated","reason":"fewer failures; some remain"}`, `{"revision":"2"}`},
		{"coordination preserves assignee", []string{"actions", "coordinate", "act-1", "--status", "blocked", "--reason", "waiting"}, "/actions/act-1/coordinate", `{"status":"blocked","reason":"waiting"}`, `{"revision":"2"}`},
		{"coordination clears assignee preserves status", []string{"actions", "coordinate", "act-1", "--clear-assignee"}, "/actions/act-1/coordinate", `{"set_assignee":true}`, `{"revision":"2"}`},
		{"coordination assigns preserves status", []string{"actions", "coordinate", "act-1", "--assignee-kind", "agent", "--assignee-id", "worker"}, "/actions/act-1/coordinate", `{"set_assignee":true,"assignee":{"kind":"agent","id":"worker"}}`, `{"revision":"2"}`},
		{"observation verification", []string{"observations", "verify", "obs-1", "--verification", "human_verified", "--reason", "checked"}, "/observations/obs-1/verify", `{"verification":"human_verified","reason":"checked"}`, `{"revision":"2"}`},
		{"evidence attachment", []string{"evidence-links", "attach", "--source", "obs-1", "--target", "clm-1", "--stance", "challenges", "--rationale", "different timing"}, "/evidence-links", `{"source_id":"obs-1","target_id":"clm-1","stance":"challenges","rationale":"different timing"}`, `{"evidence_id":"evidence-new","revision":"2"}`},
		{"evidence verification", []string{"evidence-links", "verify", "e-1", "--verification", "disputed", "--reason", "checked"}, "/evidence-links/e-1/verify", `{"verification":"disputed","reason":"checked"}`, `{"revision":"2"}`},
		{"withdrawal has no version", []string{"evidence-links", "withdraw", "e-1", "--reason", "misread"}, "/evidence-links/e-1/withdraw", `{"reason":"misread"}`, `{"revision":"2"}`},
	}
}

func TestOpModelCoreSemanticsCommandWireContract(t *testing.T) {
	setAgentMode(t, true)
	for _, tc := range coreSemanticsCases() {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/opmodel"+containerBasePath("INC-1")+tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				var body, want map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
					t.Fatal(err)
				}
				want["actor"] = map[string]any{"kind": "human", "id": "responder"}
				if !reflect.DeepEqual(body, want) {
					t.Errorf("body = %#v; want %#v", body, want)
				}
				_, _ = io.WriteString(w, tc.response)
			})
			loader := &wireOpModelLoader{client: client}
			args := append(append([]string{}, tc.args...), "--container", "INC-1", "--actor-kind", "human", "--actor-id", "responder")
			stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newOpModelCmd(loader) }, args...)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("calls = %d", calls)
			}
			doc := decodeSingleJSONValue(t, stdout)
			if doc["schema_version"] != "2" || doc["type"] != "gcx.irm.opmodel.mutation" || doc["revision"] != "2" {
				t.Errorf("envelope = %v", doc)
			}
			if tc.name == "promotion" {
				result, ok := doc["result"].(map[string]any)
				if !ok {
					t.Fatalf("missing result: %v", doc)
				}
				if !reflect.DeepEqual(result, map[string]any{"claim_id": "claim-new"}) {
					t.Errorf("promotion must return only claim identity: %v", result)
				}
			}
		})
	}
}

func TestOpModelRetiredPreconditionFlagsRejectedBeforeIO(t *testing.T) {
	for _, tc := range coreSemanticsCases() {
		for _, flag := range []string{
			"expected-content-version", "expected-source-version", "expected-target-version",
			"expected-assessment-revision", "expected-recovery-revision",
			"expected-coordination-revision", "expected-verification-revision", "expected-revision",
		} {
			t.Run(tc.name+"/"+flag, func(t *testing.T) {
				args := append(append([]string{}, tc.args...), "--"+flag, "1")
				err := assertOpModelRejectedBeforeIO(t, args)
				if !strings.Contains(err.Error(), "unknown flag: --"+flag) {
					t.Fatalf("expected unknown flag error, got %v", err)
				}
			})
		}
	}
}

func assertOpModelRejectedBeforeIO(t *testing.T, args []string) error {
	t.Helper()
	loader := &fakeOpModelLoader{op: &fakeOpModel{}}
	args = append(append([]string{}, args...), "--container", "INC-1", "--actor-kind", "agent", "--actor-id", "codex")
	stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newOpModelCmd(loader) }, args...)
	if err == nil || loader.loads != 0 || stdout != "" {
		t.Fatalf("err=%v loads=%d stdout=%s", err, loader.loads, stdout)
	}
	return err
}

func TestOpModelInvalidInputsAndRetiredSurface(t *testing.T) {
	for _, args := range [][]string{
		{"observations", "create", "--text", ""},
		{"observations", "create", "--text", "x", "--observed-start", ""},
		{"observations", "create", "--text", "x", "--observed-end", "2026-08-21T14:00:00Z"},
		{"observations", "create", "--text", "x", "--observed-start", "2026-08-21T14:01:00Z", "--observed-end", "2026-08-21T14:00:00Z"},
		{"observations", "create", "--text", "x", "--original-author-source-id", " "},
		{"claims", "assess", "c"},
		{"claims", "assess", "c", "--impact-assessment", "confirmed", "--hypothesis-assessment", "accepted"},
		{"claims", "assess", "c", "--impact-assessment", "accepted"},
		{"claims", "assess", "c", "--impact-assessment", ""},
		{"claims", "assess", "c", "--impact-assessment", "", "--hypothesis-assessment", "accepted"},
		{"claims", "recovery", "c", "--recovery", "mitigated", "--reason", " "},
		{"actions", "coordinate", "a"},
		{"actions", "coordinate", "a", "--status", ""},
		{"actions", "coordinate", "a", "--status", "", "--clear-assignee"},
		{"actions", "coordinate", "a", "--status", "finished"},
		{"actions", "coordinate", "a", "--clear-assignee", "--assignee-kind", "human", "--assignee-id", "u"},
		{"actions", "create", "--text", "x", "--assignee-kind", "human", "--assignee-id", ""},
		{"actions", "create", "--text", "x", "--assignee-name", "u"},
		{"evidence-links", "attach", "--source", "o", "--target", "o", "--stance", "supports"},
		{"evidence-links", "attach", "--source", "o", "--target", "c", "--stance", "bears_on"},
		{"evidence-links", "verify", "e", "--verification", "agent_corroborated"},
		{"claims", "settle", "c"}, {"actions", "advance", "a"}, {"bearings", "attach"},
		{"observations", "promote", "o", "--to", "impact", "--stance", "supports"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { _ = assertOpModelRejectedBeforeIO(t, args) })
	}
}

type opModelRoundTripFunc func(*http.Request) (*http.Response, error)

func (f opModelRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIRMMutationsAreNotReplayedButReadsCanRetry(t *testing.T) {
	for _, operation := range []string{"model", "message", "read"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			client := httputils.NewDefaultClient(context.Background())
			client.Transport = &retry.Transport{MinBackoff: time.Nanosecond, Base: opModelRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"revision":"1"}`))}, nil
			})}
			api := newIRMResourceClient(client, "http://example.invalid")
			var err error
			switch operation {
			case "model":
				_, err = api.CreateObservation(context.Background(), createObservationRequest{Container: "c", Text: "x"})
			case "message":
				_, err = api.CreateMessage(context.Background(), createMessageRequest{IncidentID: "c", Body: "x"})
			case "read":
				_, err = api.GetContainer(context.Background(), getContainerRequest{Container: "c"})
			}
			if operation == "read" {
				if calls != 2 || err != nil {
					t.Fatalf("read calls=%d err=%v", calls, err)
				}
			} else if calls != 1 || err == nil {
				t.Fatalf("mutation calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestOpModelConflictDoesNotRetryOrSuggestImpersonation(t *testing.T) {
	calls := 0
	api := newTestOpModelClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"only a person can assess a claim","reason":"Conflict","code":409}`)
	})
	_, err := api.AssessClaim(context.Background(), assessClaimRequest{Container: "c", ClaimID: "c", ImpactAssessment: "confirmed"})
	if err == nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	text := fmt.Sprint(err)
	for _, required := range []string{"only a person can assess a claim", "reconsider", "Do not automatically"} {
		if !strings.Contains(text, required) {
			t.Errorf("missing %q in %s", required, text)
		}
	}
	if strings.Contains(text, "pass --actor-kind human") {
		t.Fatal("must not suggest impersonation")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("conflict is not cancellation")
	}
}

func TestOpModelEventCursorsRemainOpaqueAndIndependentOfHead(t *testing.T) {
	setAgentMode(t, true)
	for _, tc := range []struct {
		name, since, response, next string
	}{
		{"sparse page", "2", `{"revision":"90","events":[{"revision":"7","id":"e7","type":"observation_created","payload":{"text":"observed"}},{"revision":"23","id":"e23","type":"text_edited","payload":{"text":"corrected"}}]}`, "23"},
		{"opaque page", "cursor/+previous", `{"revision":"a/head","events":[{"revision":"z/last","id":"e1","type":"observation_created","payload":{}}]}`, "z/last"},
		{"caught up", "23", `{"revision":"90","events":[{"revision":"90","id":"e90","type":"text_edited","payload":{}}]}`, ""},
		{"empty page", "90", `{"revision":"90","events":[]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/opmodel"+containerBasePath("INC-1")+"/events" || r.URL.Query().Get("since_revision") != tc.since || r.URL.Query().Get("page_size") != "2" {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				_, _ = io.WriteString(w, tc.response)
			})
			loader := &wireOpModelLoader{client: client}
			stdout, stderr, err := runOpModelTree(t, func() *cobra.Command { return newOpModelCmd(loader) }, "containers", "list-events", "INC-1", "--since-revision", tc.since, "--page-size", "2")
			if err != nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			var want map[string]any
			if err := json.Unmarshal([]byte(tc.response), &want); err != nil {
				t.Fatal(err)
			}
			want["type"], want["schema_version"], want["container"] = "gcx.irm.opmodel.events", "1", "INC-1"
			if got := decodeSingleJSONValue(t, stdout); !reflect.DeepEqual(got, want) {
				t.Errorf("events = %v; want %v", got, want)
			}
			if tc.next == "" {
				if strings.Contains(stderr, "More events") {
					t.Errorf("unexpected continuation: %s", stderr)
				}
			} else if !strings.Contains(stderr, "--since-revision "+tc.next) || strings.Contains(stderr, "--since-revision "+fmt.Sprint(want["revision"])) {
				t.Errorf("continuation must use last consumed event, not head: %s", stderr)
			}
		})
	}
}

func TestOpModelNoEventWritePreservesReturnedHead(t *testing.T) {
	setAgentMode(t, true)
	// The client cannot infer whether a write appended an event. Even repeated
	// successful writes must return the server's head without incrementing it.
	calls := 0
	client := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/actions/a/coordinate") {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		_, _ = io.WriteString(w, `{"revision":"head/unchanged"}`)
	})
	loader := &wireOpModelLoader{client: client}
	for range 2 {
		stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newOpModelCmd(loader) }, "actions", "coordinate", "a", "--container", "INC-1", "--status", "done", "--actor-kind", "agent", "--actor-id", "recorder")
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeSingleJSONValue(t, stdout)["revision"]; got != "head/unchanged" {
			t.Fatalf("revision = %v", got)
		}
	}
	if calls != 2 {
		t.Fatalf("expected one request per invocation, got %d", calls)
	}
}

func TestOpModelContainerRetainsCompleteProjection(t *testing.T) {
	setAgentMode(t, true)
	// HTTP projection fields at IRM b0f40e543538. Exact round-trip comparison
	// catches dropped judgments, versions, provenance, and zero revisions.
	const projection = `{
 "container":"INC-1","revision":"42",
 "observations":[{"id":"o","text":"observed","source_ref":"slack:1","observed_start":"2026-08-21T14:00:00Z","observed_end":"2026-08-21T14:01:00Z","scope":"","original_author":{"source_id":"U1"},"content_version":2,"created_by":{"kind":"agent","id":"recorder"},"created_at":"2026-08-21T15:00:00Z","verification":"human_verified","verification_revision":1,"verification_judgment":{"by":{"kind":"human","id":"reviewer"},"at":"2026-08-21T15:01:00Z","reason":"checked","subject_version":1,"subject_freshness":{"subject_needs_reassessment":true,"reason":"subject_content_changed","judged_version":1,"current_version":2}}},
 {"id":"o2","text":"unknown context","content_version":1,"created_by":{"kind":"agent","id":"recorder"},"verification":"unverified","verification_revision":0}],
 "claims":[{"id":"i","type":"impact","text":"harm","content_version":2,"origin":{"observation_id":"o","content_version":1},"impact_assessment":"confirmed","recovery":"ended","assessment_revision":1,"recovery_revision":1,"assessment_judgment":{"by":{"kind":"human","id":"reviewer"},"reason":"reproduced","subject_version":1,"subject_freshness":{"subject_needs_reassessment":true,"reason":"subject_content_changed","judged_version":1,"current_version":2}},"recovery_judgment":{"by":{"kind":"human","id":"reviewer"},"reason":"harm ended","subject_version":2,"subject_freshness":{"subject_needs_reassessment":false,"judged_version":2,"current_version":2}},"created_by":{"kind":"agent","id":"recorder"},"created_at":"2026-08-21T15:00:00Z"},
 {"id":"h","type":"hypothesis","text":"cause","content_version":1,"hypothesis_assessment":"unassessed","assessment_revision":0,"recovery_revision":0,"created_by":{"kind":"agent","id":"recorder"}}],
 "actions":[{"id":"a","text":"restart","status":"done","content_version":1,"origin":{"observation_id":"o","content_version":1},"expected_result":"healthy","assignee":{"kind":"human","id":"operator","display_name":"Operator"},"coordination_revision":2,"created_by":{"kind":"agent","id":"recorder"},"created_at":"2026-08-21T15:00:00Z","advanced_by":{"kind":"agent","id":"recorder"},"advanced_at":"2026-08-21T16:00:00Z"}],
 "evidence_links":[{"id":"e","source_id":"o","target_id":"h","source_version":1,"target_version":1,"content_version":1,"stance":"supports","rationale":"timing","attached_by":{"kind":"agent","id":"recorder"},"attached_at":"2026-08-21T15:00:00Z","verification":"human_verified","verification_revision":1,"verification_judgment":{"by":{"kind":"human","id":"reviewer"},"reason":"checked","subject_version":1,"subject_freshness":{"subject_needs_reassessment":false,"judged_version":1,"current_version":1}},"withdrawal":{"by":{"kind":"human","id":"reviewer"},"at":"2026-08-21T17:00:00Z","reason":"misread","subject_version":1,"subject_freshness":{"subject_needs_reassessment":false,"judged_version":1,"current_version":1}}}]
 }`
	client := newTestOpModelClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, projection) })
	loader := &wireOpModelLoader{client: client}
	stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newOpModelCmd(loader) }, "containers", "get", "INC-1")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeSingleJSONValue(t, stdout)
	var want map[string]any
	if err := json.Unmarshal([]byte(projection), &want); err != nil {
		t.Fatal(err)
	}
	want["type"], want["schema_version"] = "gcx.irm.opmodel.container", "2"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projection changed:\ngot %v\nwant %v", got, want)
	}
}

func TestSharesVersion2StdinAndLegacyRejection(t *testing.T) {
	setAgentMode(t, true)
	// Exact JSON example from the companion share-finding skill migration.
	raw, err := os.ReadFile("testdata/share-finding-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg sharePackage
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}
	if violations := validateSharePackage(&pkg); len(violations) != 0 {
		t.Fatalf("skill fixture: %v", violations)
	}
	raw, err = json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"v2", string(raw), true},
		{"missing evidence links", `{"version":2,"container":"INC-1","message":{"text":"Finding"},"entities":[{"ref":"h1","type":"hypothesis","text":"Possible cause"}],"message_refs":["h1"],"curated":true}`, false},
		{"null evidence links", `{"version":2,"container":"INC-1","message":{"text":"Finding"},"entities":[{"ref":"h1","type":"hypothesis","text":"Possible cause"}],"evidence_links":null,"message_refs":["h1"],"curated":true}`, false},
		{"empty evidence links", `{"version":2,"container":"INC-1","message":{"text":"Finding"},"entities":[{"ref":"h1","type":"hypothesis","text":"Possible cause"}],"evidence_links":[],"message_refs":["h1"],"curated":true}`, true},
		{"v1", strings.Replace(string(raw), `"version":2`, `"version":1`, 1), false},
		{"v1 bearings", strings.ReplaceAll(strings.Replace(string(raw), `"version":2`, `"version":1`, 1), `"evidence_links"`, `"bearings"`), false},
		{"renumbered legacy", strings.ReplaceAll(string(raw), `"evidence_links"`, `"bearings"`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := &fakeOpModel{}
			loader := &fakeOpModelLoader{op: op}
			cmd := newSharesCmd(loader)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			cmd.SetIn(strings.NewReader(tc.body))
			var stdout strings.Builder
			cmd.SetOut(&stdout)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"publish", "-f", "-", "--agent-id", "codex"})
			err := cmd.Execute()
			if !tc.valid {
				if err == nil || loader.loads != 0 || stdout.Len() != 0 {
					t.Fatalf("err=%v loads=%d stdout=%s", err, loader.loads, stdout.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			doc := decodeSingleJSONValue(t, stdout.String())
			if doc["schema_version"] != "2" || doc["evidence_link_ids"] == nil || doc["message_id"] == nil {
				t.Fatalf("receipt=%v", doc)
			}
			for _, retired := range []string{"bearing_ids", "update_id", "thread_update_id"} {
				if _, ok := doc[retired]; ok {
					t.Errorf("retired receipt field %s", retired)
				}
			}
		})
	}
}

func TestPublishShareEvidenceFailureRetainsPartialIdentities(t *testing.T) {
	pkg := validBasePackage()
	op := &fakeOpModel{failAt: 5}
	receipt := publishShare(context.Background(), op, op, &pkg, "codex", "Codex")
	if op.calls != 7 || len(receipt.EntityIDs) != 4 || len(receipt.EvidenceLinkIDs) != 1 || receipt.MessageID == "" || receipt.Summary.Failed != 1 || receipt.Summary.Succeeded != 6 {
		t.Fatalf("calls=%d receipt=%+v", op.calls, receipt)
	}
}

func TestPublishShareCurationDoesNotJudgeOrInventContext(t *testing.T) {
	pkg := validBasePackage()
	op := &fakeOpModel{}
	publishShare(context.Background(), op, op, &pkg, "codex", "Codex")
	for _, req := range op.reqs {
		raw, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		for field := range body {
			if strings.HasPrefix(field, "expected_") && field != "expected_result" {
				t.Errorf("retired write gate %q in %T", field, req)
			}
		}
		switch r := req.(type) {
		case createObservationRequest:
			if r.Actor.Kind != "agent" || r.ObservedStart != nil || r.OriginalAuthor != nil {
				t.Errorf("invented attribution or context: %+v", r)
			}
		case createClaimRequest:
			if r.Actor.Kind != "agent" {
				t.Errorf("actor=%+v", r.Actor)
			}
		case promoteToActionRequest:
			if r.Actor.Kind != "agent" {
				t.Errorf("promotion=%+v", r)
			}
		case attachEvidenceRequest:
			if r.Actor.Kind != "agent" {
				t.Errorf("evidence=%+v", r)
			}
		case createMessageRequest:
			body, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "citations") || strings.Contains(string(body), "thread") {
				t.Errorf("message invented delivery: %s", body)
			}
		default:
			t.Fatalf("curation must not add judgments: %T", req)
		}
	}
}
