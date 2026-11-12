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
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/retry"
	"github.com/spf13/cobra"
)

type wireOpMemLoader struct {
	fakeOpMemLoader

	client *irmResourceClient
}

func (l *wireOpMemLoader) LoadOpMem(context.Context, time.Duration) (OpMemAPI, error) {
	l.loads++
	return l.client, nil
}

type opMemRoundTripFunc func(*http.Request) (*http.Response, error)

func (f opMemRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIRMMutationsAreNotReplayedButReadsCanRetry(t *testing.T) {
	for _, operation := range []string{"model", "message", "read"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			client := httputils.NewDefaultClient(context.Background())
			client.Transport = &retry.Transport{MinBackoff: time.Nanosecond, Base: opMemRoundTripFunc(func(*http.Request) (*http.Response, error) {
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

func TestOpMemConflictDoesNotRetryOrSuggestImpersonation(t *testing.T) {
	calls := 0
	api := newTestOpMemClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"evidence links need an observation source","reason":"Conflict","code":409}`)
	})
	_, err := api.AttachEvidence(context.Background(), attachEvidenceRequest{Container: "c", SourceID: "h", TargetID: "i", Stance: "supports"})
	if err == nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	text := fmt.Sprint(err)
	for _, required := range []string{"evidence links need an observation source", "reconsider", "Do not automatically"} {
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

func TestOpMemEventCursorsRemainOpaqueAndIndependentOfHead(t *testing.T) {
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
			client := newTestOpMemClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/opmodel"+containerBasePath("INC-1")+"/events" || r.URL.Query().Get("since_revision") != tc.since || r.URL.Query().Get("page_size") != "2" {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				_, _ = io.WriteString(w, tc.response)
			})
			loader := &wireOpMemLoader{client: client}
			stdout, stderr, err := runOpMemTree(t, func() *cobra.Command { return newOpMemCmd(loader) }, "containers", "list-events", "INC-1", "--since-revision", tc.since, "--page-size", "2")
			if err != nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			var want map[string]any
			if err := json.Unmarshal([]byte(tc.response), &want); err != nil {
				t.Fatal(err)
			}
			want["type"], want["schema_version"], want["container"] = "gcx.irm.opmem.events", "1", "INC-1"
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

func TestOpMemContainerRetainsCompleteProjection(t *testing.T) {
	setAgentMode(t, true)
	const projection = `{
 "container":"INC-1","revision":"42",
 "observations":[{"id":"o","text":"observed","source_ref":"slack:1","observed_start":"2026-08-21T14:00:00Z","observed_end":"2026-08-21T14:01:00Z","scope":"","original_author":{"source_id":"U1"},"content_version":2,"created_by":{"kind":"agent","id":"recorder"},"created_at":"2026-08-21T15:00:00Z","verification":"human_verified","verification_revision":1,"verification_judgment":{"by":{"kind":"human","id":"reviewer"},"at":"2026-08-21T15:01:00Z","reason":"checked","subject_version":1,"subject_freshness":{"subject_needs_reassessment":true,"reason":"subject_content_changed","judged_version":1,"current_version":2}}},
 {"id":"o2","text":"unknown context","content_version":1,"created_by":{"kind":"agent","id":"recorder"},"verification":"unverified","verification_revision":0}],
 "claims":[{"id":"i","type":"impact","text":"harm","content_version":2,"origin":{"observation_id":"o","content_version":1},"impact_assessment":"confirmed","recovery":"ended","assessment_revision":1,"recovery_revision":1,"assessment_judgment":{"by":{"kind":"human","id":"reviewer"},"reason":"reproduced","subject_version":1,"subject_freshness":{"subject_needs_reassessment":true,"reason":"subject_content_changed","judged_version":1,"current_version":2}},"recovery_judgment":{"by":{"kind":"human","id":"reviewer"},"reason":"harm ended","subject_version":2,"subject_freshness":{"subject_needs_reassessment":false,"judged_version":2,"current_version":2}},"created_by":{"kind":"agent","id":"recorder"},"created_at":"2026-08-21T15:00:00Z"},
 {"id":"h","type":"hypothesis","text":"cause","content_version":1,"hypothesis_assessment":"unassessed","assessment_revision":0,"recovery_revision":0,"created_by":{"kind":"agent","id":"recorder"}}],
 "actions":[{"id":"a","text":"restart","status":"done","content_version":1,"origin":{"observation_id":"o","content_version":1},"expected_result":"healthy","assignee":{"kind":"human","id":"operator","display_name":"Operator"},"coordination_revision":2,"created_by":{"kind":"agent","id":"recorder"},"created_at":"2026-08-21T15:00:00Z","advanced_by":{"kind":"agent","id":"recorder"},"advanced_at":"2026-08-21T16:00:00Z"}],
 "evidence_links":[{"id":"e","source_id":"o","target_id":"h","source_version":1,"target_version":1,"content_version":1,"stance":"supports","rationale":"timing","attached_by":{"kind":"agent","id":"recorder"},"attached_at":"2026-08-21T15:00:00Z","verification":"human_verified","verification_revision":1,"verification_judgment":{"by":{"kind":"human","id":"reviewer"},"reason":"checked","subject_version":1,"subject_freshness":{"subject_needs_reassessment":false,"judged_version":1,"current_version":1}},"withdrawal":{"by":{"kind":"human","id":"reviewer"},"at":"2026-08-21T17:00:00Z","reason":"misread","subject_version":1,"subject_freshness":{"subject_needs_reassessment":false,"judged_version":1,"current_version":1}}}]
 }`
	client := newTestOpMemClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, projection) })
	loader := &wireOpMemLoader{client: client}
	stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newOpMemCmd(loader) }, "containers", "get", "INC-1")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeSingleJSONValue(t, stdout)
	var want map[string]any
	if err := json.Unmarshal([]byte(projection), &want); err != nil {
		t.Fatal(err)
	}
	want["type"], want["schema_version"] = "gcx.irm.opmem.container", "2"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projection changed:\ngot %v\nwant %v", got, want)
	}
}
