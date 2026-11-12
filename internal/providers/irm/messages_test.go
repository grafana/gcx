//nolint:testpackage // exercises private message command and wire types
package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
)

func TestMessageClientPostsIndependentMessage(t *testing.T) {
	client := newTestOpMemClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/messages" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["incident_id"] != "INC-1" || body["body"] != "finding" || body["see"] != "dashboard:db" {
			t.Errorf("body=%v", body)
		}
		agent, _ := body["drafted_by"].(map[string]any)
		if agent["id"] != "codex" || agent["display_name"] != "Codex" {
			t.Errorf("agent=%v", agent)
		}
		for _, key := range []string{"citations", "container", "revision", "actor"} {
			if _, ok := body[key]; ok {
				t.Errorf("unexpected %s", key)
			}
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"message_id":"incident:saved"}`))
	})
	resp, err := client.CreateMessage(context.Background(), createMessageRequest{IncidentID: "INC-1", Body: "finding", See: "dashboard:db", DraftedBy: &messageAgent{ID: "codex", DisplayName: "Codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.MessageID != "incident:saved" {
		t.Fatalf("response=%+v", resp)
	}
}

func TestMessageClientStatusToExitCode(t *testing.T) {
	for _, tt := range []struct {
		status   int
		wantExit int
	}{
		{http.StatusBadRequest, fail.ExitGeneralError},
		{http.StatusConflict, fail.ExitGeneralError},
		{http.StatusUnprocessableEntity, fail.ExitGeneralError},
		{http.StatusUnauthorized, fail.ExitAuthFailure},
		{http.StatusForbidden, fail.ExitAuthFailure},
		{http.StatusInternalServerError, fail.ExitGeneralError},
		{http.StatusGatewayTimeout, fail.ExitGeneralError},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			client := newTestOpMemClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"scripted failure"}`))
			})
			_, err := client.CreateMessage(context.Background(), createMessageRequest{IncidentID: "INC-1", Body: "finding"})
			assertDetailedExit(t, err, tt.wantExit)
			var derr *fail.DetailedError
			if !errors.As(err, &derr) {
				t.Fatalf("expected DetailedError, got %T", err)
			}
			if want := fmt.Sprintf("message sharing failed with HTTP %d", tt.status); derr.Summary != want || derr.Details != "scripted failure" {
				t.Errorf("summary=%q details=%q", derr.Summary, derr.Details)
			}
		})
	}
}

func TestMessageClientNotFoundShapes(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantSummary string
	}{
		{"server status", `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404,"message":"incident INC-1 not found"}`, "message sharing failed with HTTP 404"},
		{"route not registered", "404 page not found\n", fail.SummaryEndpointNotAvailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestOpMemClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := client.CreateMessage(context.Background(), createMessageRequest{IncidentID: "INC-1", Body: "finding"})
			assertDetailedExit(t, err, fail.ExitGeneralError)
			var derr *fail.DetailedError
			_ = errors.As(err, &derr)
			if derr.Summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", derr.Summary, tc.wantSummary)
			}
		})
	}
}

func TestIncidentShareCommand(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		agent   string
		invalid bool
	}{
		{name: "human", args: []string{"INC-1", "--body", "finding", "--see", "dashboard:db"}},
		{name: "agent", args: []string{"INC-1", "--body", "finding", "--see", "dashboard:db", "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}, agent: "Codex"},
		{name: "agent ID fallback", args: []string{"INC-1", "--body", "finding", "--see", "dashboard:db", "--drafted-by-agent", "codex"}, agent: "codex"},
		{name: "blank incident", args: []string{" ", "--body", "finding"}, invalid: true},
		{name: "blank body", args: []string{"INC-1", "--body", " "}, invalid: true},
		{name: "missing body", args: []string{"INC-1"}, invalid: true},
		{name: "agent name only", args: []string{"INC-1", "--body", "finding", "--drafted-by-agent-name", "Codex"}, invalid: true},
		{name: "blank agent", args: []string{"INC-1", "--body", "finding", "--drafted-by-agent", " "}, invalid: true},
		{name: "empty agent", args: []string{"INC-1", "--body", "finding", "--drafted-by-agent", ""}, invalid: true},
		{name: "invalid timeout", args: []string{"INC-1", "--body", "finding", "--timeout", "0s"}, invalid: true},
		{name: "blank see", args: []string{"INC-1", "--body", "finding", "--see", " "}, invalid: true},
		{name: "empty see", args: []string{"INC-1", "--body", "finding", "--see", ""}, invalid: true},
		{name: "see prefix", args: []string{"INC-1", "--body", "finding", "--see", "See: dashboard:db"}, invalid: true},
		{name: "indented lowercase see prefix", args: []string{"INC-1", "--body", "finding", "--see", "  see:dashboard:db"}, invalid: true},
		{name: "see prefix on dry run", args: []string{"INC-1", "--body", "finding", "--see", "SEE: dashboard:db", "--dry-run"}, invalid: true},
		{name: "empty expect digest", args: []string{"INC-1", "--body", "finding", "--expect-digest", ""}, invalid: true},
		{name: "blank expect digest", args: []string{"INC-1", "--body", "finding", "--expect-digest", "  "}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, true)
			api := &fakeOpMem{}
			loader := &fakeOpMemLoader{op: api}
			stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, tc.args...)
			if tc.invalid {
				requireUsageExit(t, err)
				if loader.loads != 0 || api.calls != 0 {
					t.Fatal("invalid input reached client")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if loader.loads != 1 || api.calls != 1 {
				t.Fatalf("loads=%d calls=%d", loader.loads, api.calls)
			}
			if loader.gotTimeout != 30*time.Second {
				t.Errorf("default timeout = %s, want 30s", loader.gotTimeout)
			}
			req := reqAs[createMessageRequest](t, api.reqs, 0)
			if req.IncidentID != "INC-1" || req.Body != "finding" || req.See != "dashboard:db" {
				t.Errorf("request=%+v", req)
			}
			if tc.agent == "" {
				if req.DraftedBy != nil {
					t.Errorf("unexpected agent=%+v", req.DraftedBy)
				}
			} else if req.DraftedBy == nil || req.DraftedBy.ID != "codex" || req.DraftedBy.DisplayName != tc.agent {
				t.Errorf("agent=%+v", req.DraftedBy)
			}
			doc := decodeSingleJSONValue(t, stdout)
			assertShareReceipt(t, doc, "INC-1", "upd-1")
			if _, exists := doc["revision"]; exists {
				t.Error("message has model revision")
			}
		})
	}
}

func TestIncidentShareRefusesReplyWithoutMessageID(t *testing.T) {
	setAgentMode(t, true)
	api := &fakeOpMem{messageResponse: &createMessageResponse{}}
	loader := &fakeOpMemLoader{op: api}
	stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, "INC-7", "--body", "finding")
	assertDetailedExit(t, err, fail.ExitGeneralError)
	if api.calls != 1 {
		t.Errorf("calls=%d; want one send", api.calls)
	}
	if stdout != "" {
		t.Errorf("stdout = %q; want no receipt without a message id", stdout)
	}
	var derr *fail.DetailedError
	if errors.As(err, &derr) && !strings.Contains(strings.Join(derr.Suggestions, " "), "gcx irm incidents list-activity INC-7 --kind keyUpdateAdded --limit 100") {
		t.Errorf("suggestions = %q; want the list-activity check for INC-7", derr.Suggestions)
	}
}

func TestIncidentShareRejectsCredentialsBeforeLoadingClient(t *testing.T) {
	for _, args := range [][]string{{"INC-1", "--body", "finding"}, {"INC-1", "--body", "finding", "--dry-run"}} {
		authErr := errors.New("sharing requires an OAuth user login")
		loader := &fakeOpMemLoader{op: &fakeOpMem{}, sharingAuthErr: authErr}
		stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, args...)
		if !errors.Is(err, authErr) || loader.loads != 0 || stdout != "" {
			t.Fatalf("args=%v: expected rejection before client loading, got err=%v loads=%d stdout=%s", args, err, loader.loads, stdout)
		}
	}
}

func TestIncidentShareDryRunPreviewsWithoutSending(t *testing.T) {
	testDestination := messageDestination{Context: "prod", Server: "https://example.grafana.net", ProxyEndpoint: "https://proxy-a.example"}
	for _, tc := range []struct {
		name string
		args []string
		want map[string]any
	}{
		{
			name: "with see",
			args: []string{"INC-1", "--body", "Database connections are exhausted", "--see", "dashboard:db", "--dry-run"},
			want: map[string]any{"type": "gcx.irm.message_preview", "schema_version": "1", "context": "prod", "server": "https://example.grafana.net", "proxy_endpoint": "https://proxy-a.example", "incident_id": "INC-1", "body": "Database connections are exhausted", "see": "dashboard:db", "preview": "Database connections are exhausted\nSee: dashboard:db"},
		},
		{
			name: "without see",
			args: []string{"INC-1", "--body", "The service has recovered", "--dry-run"},
			want: map[string]any{"type": "gcx.irm.message_preview", "schema_version": "1", "context": "prod", "server": "https://example.grafana.net", "proxy_endpoint": "https://proxy-a.example", "incident_id": "INC-1", "body": "The service has recovered", "preview": "The service has recovered"},
		},
		{
			name: "drafted by agent",
			args: []string{"INC-1", "--body", "The service has recovered", "--drafted-by-agent", "codex", "--dry-run"},
			want: map[string]any{"type": "gcx.irm.message_preview", "schema_version": "1", "context": "prod", "server": "https://example.grafana.net", "proxy_endpoint": "https://proxy-a.example", "incident_id": "INC-1", "body": "The service has recovered", "drafted_by": map[string]any{"id": "codex", "display_name": "codex"}, "preview": "The service has recovered"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, true)
			api := &fakeOpMem{}
			loader := &fakeOpMemLoader{op: api, destination: testDestination}
			stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if api.calls != 0 || loader.loads != 0 {
				t.Fatalf("dry run reached the message API: loads=%d calls=%d", loader.loads, api.calls)
			}
			got := decodeSingleJSONValue(t, stdout)
			digest, _ := got["digest"].(string)
			if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(digest) {
				t.Errorf("digest = %q; want sha256: and 64 lowercase hex characters", digest)
			}
			delete(got, "digest")
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("preview = %v; want %v", got, tc.want)
			}
		})
	}
}

// shareDigest runs a dry run and returns the digest it prints.
func shareDigest(t *testing.T, dest messageDestination, args ...string) string {
	t.Helper()
	setAgentMode(t, true)
	stdout, _, err := runOpMemTree(t, func() *cobra.Command {
		return newIncidentShareCmd(&fakeOpMemLoader{op: &fakeOpMem{}, destination: dest})
	}, append(args, "--dry-run")...)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := decodeSingleJSONValue(t, stdout)["digest"].(string)
	if digest == "" {
		t.Fatalf("preview has no digest: %s", stdout)
	}
	return digest
}

func writeShareBody(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIncidentShareDigestCoversWhatIsSentAndWhere(t *testing.T) {
	testDestination := messageDestination{Context: "prod", Server: "https://example.grafana.net", ProxyEndpoint: "https://proxy-a.example"}
	base := writeShareBody(t, "base.txt", "finding\n")
	baseArgs := []string{"INC-1", "--body-file", base, "--see", "dashboard:db", "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}
	want := shareDigest(t, testDestination, baseArgs...)
	for _, tc := range []struct {
		name string
		dest messageDestination
		args []string
		same bool
	}{
		{name: "same inputs", same: true},
		{name: "same body without trailing newline", args: []string{"INC-1", "--body-file", writeShareBody(t, "bare.txt", "finding"), "--see", "dashboard:db", "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}, same: true},
		{name: "same body given inline", args: []string{"INC-1", "--body", "finding", "--see", "dashboard:db", "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}, same: true},
		{name: "edited body file", args: []string{"INC-1", "--body-file", writeShareBody(t, "edited.txt", "finding, corrected\n"), "--see", "dashboard:db", "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}},
		{name: "different see", args: []string{"INC-1", "--body-file", base, "--see", "dashboard:api", "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}},
		{name: "no see", args: []string{"INC-1", "--body-file", base, "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}},
		{name: "different incident", args: []string{"INC-2", "--body-file", base, "--see", "dashboard:db", "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}},
		{name: "different agent", args: []string{"INC-1", "--body-file", base, "--see", "dashboard:db", "--drafted-by-agent", "claude", "--drafted-by-agent-name", "Codex"}},
		{name: "different agent name", args: []string{"INC-1", "--body-file", base, "--see", "dashboard:db", "--drafted-by-agent", "codex"}},
		{name: "no agent", args: []string{"INC-1", "--body-file", base, "--see", "dashboard:db"}},
		{name: "different context", dest: messageDestination{Context: "staging", Server: testDestination.Server, ProxyEndpoint: testDestination.ProxyEndpoint}},
		{name: "different server", dest: messageDestination{Context: testDestination.Context, Server: "https://other.grafana.net", ProxyEndpoint: testDestination.ProxyEndpoint}},
		{name: "different proxy endpoint", dest: messageDestination{Context: testDestination.Context, Server: testDestination.Server, ProxyEndpoint: "https://proxy-b.example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest, args := tc.dest, tc.args
			if dest == (messageDestination{}) {
				dest = testDestination
			}
			if args == nil {
				args = baseArgs
			}
			if got := shareDigest(t, dest, args...); (got == want) != tc.same {
				t.Errorf("digest %s, base %s; want same=%v", got, want, tc.same)
			}
		})
	}
}

func TestIncidentShareSendsWhenDigestMatches(t *testing.T) {
	testDestination := messageDestination{Context: "prod", Server: "https://example.grafana.net"}
	path := writeShareBody(t, "update.txt", "finding\n")
	args := []string{"INC-1", "--body-file", path, "--see", "dashboard:db", "--drafted-by-agent", "codex"}
	digest := shareDigest(t, testDestination, args...)

	api := &fakeOpMem{}
	loader := &fakeOpMemLoader{op: api, destination: testDestination}
	stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, append(args, "--expect-digest", digest)...)
	if err != nil {
		t.Fatal(err)
	}
	if loader.loads != 1 || api.calls != 1 {
		t.Fatalf("loads=%d calls=%d; want one send", loader.loads, api.calls)
	}
	want := createMessageRequest{IncidentID: "INC-1", Body: "finding", See: "dashboard:db", DraftedBy: &messageAgent{ID: "codex", DisplayName: "codex"}}
	if req := reqAs[createMessageRequest](t, api.reqs, 0); !reflect.DeepEqual(req, want) {
		t.Errorf("request = %+v; want %+v", req, want)
	}
	assertShareReceipt(t, decodeSingleJSONValue(t, stdout), "INC-1", "upd-1")
}

// assertShareReceipt checks the shared single-mutation shape that output.md
// § 12.1 requires, plus the message id that only a share has.
func assertShareReceipt(t *testing.T, doc map[string]any, incidentID, messageID string) {
	t.Helper()
	want := map[string]any{
		"type": "gcx.mutation", "schema_version": "1", "action": "share",
		"target":  map[string]any{"kind": "Incident", "id": incidentID},
		"changed": true, "message_id": messageID,
	}
	if !reflect.DeepEqual(doc, want) {
		t.Errorf("receipt = %v; want %v", doc, want)
	}
}

func TestIncidentShareRefusesWhenDigestDiffers(t *testing.T) {
	testDestination := messageDestination{Context: "prod", Server: "https://example.grafana.net"}
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, path string) messageDestination
	}{
		{name: "body file edited after preview", change: func(t *testing.T, path string) messageDestination {
			t.Helper()
			if err := os.WriteFile(path, []byte("finding, corrected\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return testDestination
		}},
		{name: "context switched after preview", change: func(*testing.T, string) messageDestination {
			return messageDestination{Context: "staging", Server: "https://staging.grafana.net"}
		}},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s dry-run=%v", tc.name, dryRun), func(t *testing.T) {
				path := writeShareBody(t, "update.txt", "finding\n")
				previewed := shareDigest(t, testDestination, "INC-1", "--body-file", path)
				dest := tc.change(t, path)
				current := shareDigest(t, dest, "INC-1", "--body-file", path)

				api := &fakeOpMem{}
				loader := &fakeOpMemLoader{op: api, destination: dest}
				sendArgs := []string{"INC-1", "--body-file", path, "--expect-digest", previewed}
				if dryRun {
					sendArgs = append(sendArgs, "--dry-run")
				}
				stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, sendArgs...)
				requireUsageExit(t, err)
				var derr *fail.DetailedError
				if !errors.As(err, &derr) || derr.Summary != "the message or its destination changed since the preview" {
					t.Fatalf("err = %v", err)
				}
				if strings.Contains(err.Error()+derr.Details+strings.Join(derr.Suggestions, " "), current) {
					t.Errorf("the error reveals the current digest %s, which would let a caller send unapproved text", current)
				}
				if loader.loads != 0 || api.calls != 0 || stdout != "" {
					t.Fatalf("mismatched digest reached the client: loads=%d calls=%d stdout=%q", loader.loads, api.calls, stdout)
				}
			})
		}
	}
}

func TestIncidentShareBodyFile(t *testing.T) {
	const text = "  Line one: it's `$HOME` and \"quoted\"\n\n  indented line two "
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	lf := write("lf.txt", text+"\n")
	crlf := write("crlf.txt", text+"\r\n\r\n")
	bare := write("bare.txt", text)
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "trailing newline", args: []string{"--body-file", lf}},
		{name: "trailing CRLF lines", args: []string{"--body-file", crlf}},
		{name: "no trailing newline", args: []string{"--body-file", bare}},
		{name: "standard input", args: []string{"--body-file", "-"}, stdin: text + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, true)
			run := func(extra ...string) (string, *fakeOpMem) {
				api := &fakeOpMem{}
				args := append(append([]string{"INC-1", "--see", "dashboard:db"}, tc.args...), extra...)
				stdout, _, err := runNounCmd(t, func() *cobra.Command { return newIncidentShareCmd(&fakeOpMemLoader{op: api}) }, tc.stdin, args...)
				if err != nil {
					t.Fatal(err)
				}
				return stdout, api
			}
			stdout, _ := run("--dry-run")
			preview := decodeSingleJSONValue(t, stdout)
			if preview["body"] != text || preview["preview"] != text+"\nSee: dashboard:db" {
				t.Errorf("preview=%v", preview)
			}
			_, api := run()
			if req := reqAs[createMessageRequest](t, api.reqs, 0); req.Body != text || req.See != "dashboard:db" {
				t.Errorf("sent body=%q see=%q; want %q", req.Body, req.See, text)
			}
		})
	}
}

func TestIncidentShareBodySourceErrors(t *testing.T) {
	dir := t.TempDir()
	blank := filepath.Join(dir, "blank.txt")
	if err := os.WriteFile(blank, []byte(" \n\r\n\t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good.txt")
	if err := os.WriteFile(good, []byte("finding\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")
	for _, tc := range []struct {
		name    string
		args    []string
		stdin   string
		summary string
	}{
		{name: "both flags", args: []string{"--body", "finding", "--body-file", good}, summary: "use only one of --body or --body-file"},
		{name: "neither flag", summary: "one of --body or --body-file is required"},
		{name: "empty path", args: []string{"--body-file", ""}, summary: "--body-file must not be empty; give a path, or - for standard input"},
		{name: "missing file", args: []string{"--body-file", missing}, summary: fmt.Sprintf("cannot read --body-file %q", missing)},
		{name: "blank file", args: []string{"--body-file", blank}, summary: "the message body must not be blank"},
		{name: "blank standard input", args: []string{"--body-file", "-"}, stdin: "\n\n", summary: "the message body must not be blank"},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s dry-run=%v", tc.name, dryRun), func(t *testing.T) {
				api := &fakeOpMem{}
				loader := &fakeOpMemLoader{op: api}
				args := append([]string{"INC-1"}, tc.args...)
				if dryRun {
					args = append(args, "--dry-run")
				}
				stdout, _, err := runNounCmd(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, tc.stdin, args...)
				requireUsageExit(t, err)
				var derr *fail.DetailedError
				if errors.As(err, &derr) && derr.Summary != tc.summary {
					t.Errorf("summary=%q; want %q", derr.Summary, tc.summary)
				}
				if loader.loads != 0 || api.calls != 0 || stdout != "" {
					t.Fatalf("invalid body source reached the client: loads=%d calls=%d stdout=%q", loader.loads, api.calls, stdout)
				}
			})
		}
	}
}
