package archive_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/logs/archive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	prodID     = "0190a7e2-0000-7000-8000-000000000001"
	euID       = "0190a7e2-0000-7000-8000-000000000002"
	paymentsID = "0190a7e2-0000-7000-8000-0000000000a1"
	billingID  = "0190a7e2-0000-7000-8000-0000000000a2"
)

type fakeArchiveAPI struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]string

	validateStatus int
	validateBody   string

	replies map[string]fakeReply
}

type fakeReply struct {
	status int
	body   string
}

func (f *fakeArchiveAPI) record(r *http.Request) string {
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, key)
	if f.bodies == nil {
		f.bodies = map[string]string{}
	}
	f.bodies[key] = string(body)
	return key
}

func (f *fakeArchiveAPI) archiveRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fakeArchiveAPI) body(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[key]
}

func destinationFixture(id, name string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"description":"","type":"s3","storage_config":{"s3":{"bucket_name":"acme-logs","region":"us-east-1","path_prefix":"grafana"}},"created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z"}`, id, name)
}

func streamFixture(id, destinationID, name, selector string) string {
	return fmt.Sprintf(`{"id":%q,"archive_destination_id":%q,"name":%q,"signal":"logs","stream_selector":%q,"created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z"}`, id, destinationID, name, selector)
}

func (f *fakeArchiveAPI) handler(w http.ResponseWriter, r *http.Request) {
	key := f.record(r)
	w.Header().Set("Content-Type", "application/json")
	reply := func(status int, body string) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}

	if r, ok := f.replies[key]; ok {
		reply(r.status, r.body)
		return
	}

	switch key {
	case "GET /archive-replay/api/v1/cell-details":
		reply(http.StatusOK, `{"result":{"cloud_provider":"aws","region":"us-east-1","aws":{"arn":"arn:aws:iam::123456789012:role/grafana-archive"},"gcp":{"service_account":""},"azure":{"application_id":""}}}`)
	case "GET /archive-replay/api/v1/archives":
		reply(http.StatusOK, `{"result":[`+destinationFixture(prodID, "prod-archive")+`,`+destinationFixture(euID, "eu-archive")+`]}`)
	case "GET /archive-replay/api/v1/archives/" + prodID:
		reply(http.StatusOK, `{"result":`+destinationFixture(prodID, "prod-archive")+`}`)
	case "GET /archive-replay/api/v1/archives/" + euID:
		reply(http.StatusOK, `{"result":`+destinationFixture(euID, "eu-archive")+`}`)
	case "POST /archive-replay/api/v1/archives":
		if f.validateStatus != 0 {
			reply(f.validateStatus, f.validateBody)
			return
		}
		reply(http.StatusCreated, `{"result":`+destinationFixture(prodID, "prod-archive")+`}`)
	case "POST /archive-replay/api/v1/archives/validate":
		if f.validateStatus != 0 {
			reply(f.validateStatus, f.validateBody)
			return
		}
		reply(http.StatusOK, `{"result":"validation passed"}`)
	case "DELETE /archive-replay/api/v1/archives/" + prodID:
		w.WriteHeader(http.StatusNoContent)
	case "GET /archive-replay/api/v1/streams":
		reply(http.StatusOK, `{"result":[`+
			streamFixture(paymentsID, prodID, "payments", `{service="payments"}`)+`,`+
			streamFixture(billingID, euID, "billing", `{service="billing"}`)+`]}`)
	case "GET /archive-replay/api/v1/streams/" + paymentsID:
		reply(http.StatusOK, `{"result":`+streamFixture(paymentsID, prodID, "payments", `{service="payments"}`)+`}`)
	case "POST /archive-replay/api/v1/streams":
		reply(http.StatusCreated, `{"result":`+streamFixture(paymentsID, prodID, "payments", `{service="payments"}`)+`}`)
	case "PUT /archive-replay/api/v1/streams/" + paymentsID:
		var req map[string]string
		_ = json.Unmarshal([]byte(f.body(key)), &req)
		reply(http.StatusOK, `{"result":`+streamFixture(paymentsID, req["archive_destination_id"], req["name"], req["stream_selector"])+`}`)
	case "DELETE /archive-replay/api/v1/streams/" + paymentsID:
		w.WriteHeader(http.StatusNoContent)
	default:
		reply(http.StatusNotFound, `{"message":"not found"}`)
	}
}

func newTestLoader(t *testing.T, api *fakeArchiveAPI) *providers.ConfigLoader {
	t.Helper()
	t.Setenv("GCX_AUTO_APPROVE", "")

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/instances/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":1,"hlInstanceId":42,"hlInstanceUrl":%q}`, srv.URL)
	})
	mux.HandleFunc("/archive-replay/", api.handler)

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("contexts:\n  default:\n    cloud:\n      token: test-token\n      stack: teststack\n      api-url: %s\ncurrent-context: default\n", srv.URL)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgPath)
	return loader
}

type cmdResult struct {
	stdout string
	stderr string
	err    error
}

// Agent mode must be set before the tree is built: output defaults are
// resolved at flag-binding time.
func runArchiveCmd(t *testing.T, loader *providers.ConfigLoader, agentMode bool, stdin string, args ...string) cmdResult {
	t.Helper()

	prior := agent.IsAgentMode()
	agent.SetFlag(agentMode)
	t.Cleanup(func() { agent.SetFlag(prior) })

	root := archive.Commands(loader)
	root.SilenceUsage = true
	root.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return cmdResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func decodeOneJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout is not valid JSON: %q", stdout)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF, "stdout must contain exactly one JSON value: %q", stdout)
	return first
}

func TestPrincipalGet(t *testing.T) {
	api := &fakeArchiveAPI{}
	res := runArchiveCmd(t, newTestLoader(t, api), false, "", "principal", "get")
	require.NoError(t, res.err, "stderr: %s", res.stderr)

	assert.Contains(t, res.stdout, "CLOUD PROVIDER")
	assert.Contains(t, res.stdout, "us-east-1")
	assert.Contains(t, res.stdout, "s3")
	assert.Contains(t, res.stdout, "arn:aws:iam::123456789012:role/grafana-archive")
}

func TestHelp_LeafWithoutLongDescribesCommand(t *testing.T) {
	res := runArchiveCmd(t, &providers.ConfigLoader{}, false, "", "destinations", "list", "--help")
	require.NoError(t, res.err, "stderr: %s", res.stderr)
	assert.Contains(t, res.stdout, "This command is experimental.")
	assert.Contains(t, res.stdout, "List archive destinations.")
}

func TestDestinationsGet(t *testing.T) {
	t.Run("the ID is fetched directly", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "destinations", "get", euID, "-o", "json")
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, euID, doc["id"])
		assert.Equal(t, []string{"GET /archive-replay/api/v1/archives/" + euID}, api.archiveRequests())
	})

	t.Run("a non-canonical UUID is fetched by its canonical form", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "destinations", "get", strings.ToUpper(prodID), "-o", "json")
		require.NoError(t, res.err, "stderr: %s", res.stderr)
		assert.Equal(t, []string{"GET /archive-replay/api/v1/archives/" + prodID}, api.archiveRequests())
	})

	t.Run("a name is not looked up", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "destinations", "get", "eu-archive")

		var apiErr *archive.APIError
		require.ErrorAs(t, res.err, &apiErr)
		assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
		assert.Equal(t, []string{"GET /archive-replay/api/v1/archives/eu-archive"}, api.archiveRequests())
	})
}

func TestDestinationsList_DefaultTableShowsIDFirst(t *testing.T) {
	api := &fakeArchiveAPI{}
	res := runArchiveCmd(t, newTestLoader(t, api), false, "", "destinations", "list")
	require.NoError(t, res.err, "stderr: %s", res.stderr)

	lines := strings.Split(strings.TrimSpace(res.stdout), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, []string{"ID", "NAME", "TYPE", "LOCATION", "REGION", "PATH", "PREFIX"}, strings.Fields(lines[0]))
	assert.Equal(t, []string{prodID, "prod-archive"}, strings.Fields(lines[1])[:2])
	assert.Equal(t, []string{euID, "eu-archive"}, strings.Fields(lines[2])[:2])
}

func TestDestinationsCreate_FromFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "s3",
			args: []string{"--name", "prod-archive", "--type", "s3", "--bucket", "acme-logs", "--region", "us-east-1", "--path-prefix", "grafana"},
			want: `{"name":"prod-archive","type":"s3","storage_config":{"s3":{"bucket_name":"acme-logs","region":"us-east-1","path_prefix":"grafana"}}}`,
		},
		{
			name: "gcs",
			args: []string{"--name", "prod-archive", "--type", "gcs", "--bucket", "acme-logs", "--description", "GCS archive"},
			want: `{"name":"prod-archive","description":"GCS archive","type":"gcs","storage_config":{"gcs":{"bucket_name":"acme-logs"}}}`,
		},
		{
			name: "azure",
			args: []string{"--name", "prod-archive", "--type", "azure", "--storage-account", "acmelogs", "--container", "archive", "--azure-tenant-id", "tenant-1"},
			want: `{"name":"prod-archive","type":"azure","storage_config":{"azure":{"storage_account":"acmelogs","container":"archive","tenant_id":"tenant-1"}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeArchiveAPI{}
			res := runArchiveCmd(t, newTestLoader(t, api), false, "", append([]string{"destinations", "create"}, tc.args...)...)
			require.NoError(t, res.err, "stderr: %s", res.stderr)

			assert.Equal(t, []string{"POST /archive-replay/api/v1/archives"}, api.archiveRequests())
			assert.JSONEq(t, tc.want, api.body("POST /archive-replay/api/v1/archives"))
			assert.Contains(t, res.stderr, `Created archive destination "prod-archive"`)
			doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, prodID, doc["id"])
		})
	}
}

func TestDestinationsCreate_InputValidationBeforeAPI(t *testing.T) {
	dir := t.TempDir()
	wrongProvider := filepath.Join(dir, "wrong-provider.yaml")
	require.NoError(t, os.WriteFile(wrongProvider, []byte("name: prod-archive\ntype: gcs\nstorage_config:\n  s3:\n    bucket_name: acme-logs\n"), 0o600))

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"missing type", []string{"--name", "x", "--bucket", "b"}, "type must be one of"},
		{"missing name", []string{"--type", "gcs", "--bucket", "b"}, "a name is required"},
		{"s3 without region", []string{"--name", "x", "--type", "s3", "--bucket", "b"}, "an s3 destination needs a bucket name and a region"},
		{"azure without container", []string{"--name", "x", "--type", "azure", "--storage-account", "a", "--azure-tenant-id", "t"}, "an azure destination needs"},
		{"flags for another type", []string{"--name", "x", "--type", "gcs", "--bucket", "b", "--region", "us-east-1", "--container", "c"}, "--container, --region cannot be used with --type gcs"},
		{"file with only another type's provider", []string{"-f", wrongProvider}, "storage_config must set only the provider matching the type"},
		{"field flags are checked before the file is read", []string{"-f", filepath.Join(dir, "missing.yaml"), "--type", "gcs"}, "--filename cannot be combined with --type"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeArchiveAPI{}
			res := runArchiveCmd(t, newTestLoader(t, api), false, "", append([]string{"destinations", "create"}, tc.args...)...)

			var usage *gcxerrors.UsageError
			require.ErrorAs(t, res.err, &usage)
			assert.Contains(t, usage.Error(), tc.wantErr)
			assert.Empty(t, api.archiveRequests(), "validation must fail before any API call")
		})
	}
}

func TestDestinationsCreate_BadFileInput(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}

	tests := []struct {
		name    string
		file    string
		stdin   string
		wantErr string
	}{
		{"whitespace-only file", write("blank.yaml", "\n  \n\t\n"), "", "input is empty"},
		{"empty stdin", "-", "", "input is empty"},
		{"oversized file", write("big.yaml", strings.Repeat("#", 1<<20+1)), "", "input exceeds maximum size"},
		{"misspelled field", write("typo.yaml", "name: x\ntype: gcs\nstorage_config:\n  gcs:\n    bucket_name: b\n    pathprefix: p\n"), "", "pathprefix"},
		{"two documents", write("two.yaml", "name: a\ntype: gcs\nstorage_config:\n  gcs:\n    bucket_name: b\n---\nname: c\ntype: gcs\nstorage_config:\n  gcs:\n    bucket_name: d\n"), "", "the file must contain exactly one destination"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeArchiveAPI{}
			res := runArchiveCmd(t, newTestLoader(t, api), false, tc.stdin, "destinations", "create", "-f", tc.file)
			require.ErrorContains(t, res.err, tc.wantErr)
			assert.Empty(t, api.archiveRequests())
		})
	}
}

func TestDestinationsCreate_FromFileRoundTrip(t *testing.T) {
	api := &fakeArchiveAPI{}
	loader := newTestLoader(t, api)

	got := runArchiveCmd(t, loader, false, "", "destinations", "get", prodID, "-o", "yaml")
	require.NoError(t, got.err, "stderr: %s", got.stderr)

	res := runArchiveCmd(t, loader, false, got.stdout, "destinations", "create", "-f", "-")
	require.NoError(t, res.err, "stderr: %s", res.stderr)

	assert.JSONEq(t,
		`{"name":"prod-archive","type":"s3","storage_config":{"s3":{"bucket_name":"acme-logs","region":"us-east-1","path_prefix":"grafana"}}}`,
		api.body("POST /archive-replay/api/v1/archives"),
		"read-only fields from get output are dropped")
}

func TestDestinationsCreate_FailedBucketCheckExplainsTheFix(t *testing.T) {
	api := &fakeArchiveAPI{
		validateStatus: http.StatusForbidden,
		validateBody:   `{"message":"storage_config: ownership verification failed","errors":[{"field":"storage_config","message":"ownership verification failed","code":"ownership_marker_missing"}]}`,
	}
	res := runArchiveCmd(t, newTestLoader(t, api), false, "",
		"destinations", "create", "--name", "prod-archive", "--type", "s3", "--bucket", "acme-logs", "--region", "us-east-1")

	var detailed *gcxerrors.DetailedError
	require.ErrorAs(t, res.err, &detailed)
	assert.Equal(t, gcxerrors.SummaryAPIError, detailed.Summary)
	assert.Nil(t, detailed.ExitCode)
	assert.Equal(t, []string{"POST /archive-replay/api/v1/archives"}, api.archiveRequests())
	assert.Empty(t, res.stdout)
}

func TestDestinationsCreate_DryRun(t *testing.T) {
	args := []string{"destinations", "create", "--name", "prod-archive", "--type", "s3", "--bucket", "acme-logs", "--region", "us-east-1", "--dry-run"}

	t.Run("calls validate only and reports a dry-run mutation", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", args...)
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		assert.Equal(t, []string{"POST /archive-replay/api/v1/archives/validate"}, api.archiveRequests())
		doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "gcx.mutation", doc["type"])
		assert.Equal(t, "validated", doc["action"])
		assert.Equal(t, true, doc["dry_run"])
		assert.Equal(t, false, doc["changed"])
		assert.Contains(t, res.stderr, "passed the bucket check")
	})

	t.Run("a failed bucket check explains the fix", func(t *testing.T) {
		api := &fakeArchiveAPI{
			validateStatus: http.StatusForbidden,
			validateBody:   `{"message":"storage_config: ownership verification failed","errors":[{"field":"storage_config","message":"ownership verification failed: ownership token not found at \"grafana/tenant=42/grafana-archive-ownership\"","code":"ownership_marker_missing"}]}`,
		}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", args...)

		var detailed *gcxerrors.DetailedError
		require.ErrorAs(t, res.err, &detailed)
		assert.Equal(t, gcxerrors.SummaryAPIError, detailed.Summary)
		assert.Nil(t, detailed.ExitCode)
		assert.Contains(t, detailed.Details, "grafana/tenant=42/grafana-archive-ownership")
		assert.Empty(t, res.stdout)
	})
}

func TestDestinationsDelete(t *testing.T) {
	t.Run("--force deletes by ID", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "destinations", "delete", prodID, "--force")
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		assert.Equal(t, []string{
			"GET /archive-replay/api/v1/archives/" + prodID,
			"DELETE /archive-replay/api/v1/archives/" + prodID,
		}, api.archiveRequests())
		assert.Equal(t, "✔ Deleted archive destination \"prod-archive\"\n", res.stdout)
	})

	t.Run("GCX_AUTO_APPROVE deletes without prompting", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		loader := newTestLoader(t, api)
		t.Setenv("GCX_AUTO_APPROVE", "true")
		res := runArchiveCmd(t, loader, false, "", "destinations", "delete", prodID)
		require.NoError(t, res.err, "stderr: %s", res.stderr)
		assert.Equal(t, []string{
			"GET /archive-replay/api/v1/archives/" + prodID,
			"DELETE /archive-replay/api/v1/archives/" + prodID,
		}, api.archiveRequests())
	})

	t.Run("the prompt counts the streams deleted with it", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "y\n", "destinations", "delete", prodID)
		require.NoError(t, res.err, "stderr: %s", res.stderr)
		assert.Contains(t, res.stderr, `Delete archive destination "prod-archive"? Its 1 stream will be deleted too`)
		assert.Contains(t, api.archiveRequests(), "DELETE /archive-replay/api/v1/archives/"+prodID)
	})

	t.Run("a declined prompt is a cancellation", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "n\n", "destinations", "delete", prodID)

		var detailed *gcxerrors.DetailedError
		require.ErrorAs(t, res.err, &detailed)
		require.NotNil(t, detailed.ExitCode)
		assert.Equal(t, gcxerrors.ExitCancelled, *detailed.ExitCode)
		assert.NotContains(t, api.archiveRequests(), "DELETE /archive-replay/api/v1/archives/"+prodID)
		assert.Empty(t, res.stdout)
	})

	t.Run("agent mode without --force is rejected before deleting", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), true, "", "destinations", "delete", prodID)
		require.ErrorIs(t, res.err, providers.ErrAgentModeRequiresForce)
		assert.NotContains(t, api.archiveRequests(), "DELETE /archive-replay/api/v1/archives/"+prodID)
	})
}

func TestStreamsList(t *testing.T) {
	t.Run("all streams with destination names", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "streams", "list", "-o", "json")
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		rows, ok := decodeOneJSONValue(t, res.stdout).([]any)
		require.True(t, ok)
		require.Len(t, rows, 2)
		first, ok := rows[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "payments", first["name"])
		assert.Equal(t, "prod-archive", first["archive_destination_name"])
	})

	t.Run("--destination filters by ID", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "streams", "list", "--destination", euID)
		require.NoError(t, res.err, "stderr: %s", res.stderr)
		assert.Contains(t, res.stdout, "billing")
		assert.NotContains(t, res.stdout, "payments")
	})

	t.Run("--destination filters by a non-canonical UUID", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "streams", "list", "--destination", strings.ToUpper(prodID))
		require.NoError(t, res.err, "stderr: %s", res.stderr)
		assert.Contains(t, res.stdout, "payments")
		assert.NotContains(t, res.stdout, "billing")
	})

	t.Run("--destination with an unknown ID is not found", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "streams", "list", "--destination", "eu-archive")

		var detailed *gcxerrors.DetailedError
		require.ErrorAs(t, res.err, &detailed)
		assert.Equal(t, gcxerrors.SummaryResourceNotFound, detailed.Summary)
		assert.Contains(t, detailed.Details, `"eu-archive"`)
		assert.Equal(t, []string{"List archive destinations: gcx logs archive destinations list"}, detailed.Suggestions)
		assert.Empty(t, res.stdout)
	})

	t.Run("the default table shows the stream ID first", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "streams", "list")
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		lines := strings.Split(strings.TrimSpace(res.stdout), "\n")
		require.Len(t, lines, 3)
		assert.Equal(t, []string{"ID", "NAME", "DESTINATION", "SELECTOR"}, strings.Fields(lines[0]))
		assert.Equal(t, []string{paymentsID, "payments"}, strings.Fields(lines[1])[:2])
		assert.Equal(t, []string{billingID, "billing"}, strings.Fields(lines[2])[:2])
	})
}

func TestEmptyArgumentsRejectedBeforeAPI(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"get with an empty ID", []string{"streams", "get", ""}, "the stream ID cannot be empty"},
		{"get with a whitespace ID", []string{"streams", "get", " \t"}, "the stream ID cannot be empty"},
		{"update with an empty ID", []string{"streams", "update", "", "--name", "payments-v2"}, "the stream ID cannot be empty"},
		{"update with a whitespace ID", []string{"streams", "update", " ", "--name", "payments-v2"}, "the stream ID cannot be empty"},
		{"delete with an empty ID", []string{"streams", "delete", "", "--force"}, "the stream ID cannot be empty"},
		{"delete with a whitespace ID", []string{"streams", "delete", " ", "--force"}, "the stream ID cannot be empty"},
		{"list with an empty destination", []string{"streams", "list", "--destination", ""}, "--destination cannot be empty"},
		{"list with a whitespace destination", []string{"streams", "list", "--destination", " "}, "--destination cannot be empty"},
		{"destination get with an empty ID", []string{"destinations", "get", ""}, "the destination ID cannot be empty"},
		{"destination delete with a whitespace ID", []string{"destinations", "delete", " ", "--force"}, "the destination ID cannot be empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeArchiveAPI{}
			res := runArchiveCmd(t, newTestLoader(t, api), false, "", tc.args...)

			var usage *gcxerrors.UsageError
			require.ErrorAs(t, res.err, &usage)
			assert.Contains(t, usage.Error(), tc.wantErr)
			assert.Empty(t, api.archiveRequests(), "validation must fail before any API call")
		})
	}
}

func TestStreams_UUIDArgumentInCanonicalForm(t *testing.T) {
	upperID := strings.ToUpper(paymentsID)
	stream := "/archive-replay/api/v1/streams/" + paymentsID

	tests := []struct {
		name         string
		args         []string
		wantRequests []string
	}{
		{"get", []string{"get", upperID, "-o", "json"}, []string{"GET " + stream, "GET /archive-replay/api/v1/archives"}},
		{"update", []string{"update", upperID, "--name", "payments-v2"}, []string{"GET " + stream, "PUT " + stream, "GET /archive-replay/api/v1/archives/" + prodID}},
		{"delete", []string{"delete", upperID, "--force"}, []string{"DELETE " + stream}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeArchiveAPI{}
			res := runArchiveCmd(t, newTestLoader(t, api), false, "", append([]string{"streams"}, tc.args...)...)
			require.NoError(t, res.err, "stderr: %s", res.stderr)

			assert.ElementsMatch(t, tc.wantRequests, api.archiveRequests())
			assert.Contains(t, res.stdout, paymentsID)
			assert.NotContains(t, res.stdout, upperID)
		})
	}
}

func TestStreamsCreate(t *testing.T) {
	t.Run("the destination is fetched by ID", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "",
			"streams", "create", "--destination", prodID, "--name", "payments", "--selector", `{service="payments"}`)
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		assert.Equal(t, []string{
			"GET /archive-replay/api/v1/archives/" + prodID,
			"POST /archive-replay/api/v1/streams",
		}, api.archiveRequests())

		assert.JSONEq(t,
			`{"archive_destination_id":"`+prodID+`","name":"payments","signal":"logs","stream_selector":"{service=\"payments\"}"}`,
			api.body("POST /archive-replay/api/v1/streams"))
		doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "prod-archive", doc["archive_destination_name"])
	})

	t.Run("an unknown destination fails before creating", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "",
			"streams", "create", "--destination", "prod-archive", "--name", "payments", "--selector", `{service="payments"}`)

		var apiErr *archive.APIError
		require.ErrorAs(t, res.err, &apiErr)
		assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
		assert.NotContains(t, api.archiveRequests(), "POST /archive-replay/api/v1/streams")
	})

	t.Run("empty values are rejected before any API call", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "",
			"streams", "create", "--destination", prodID, "--name", " ", "--selector", `{service="payments"}`)

		var usage *gcxerrors.UsageError
		require.ErrorAs(t, res.err, &usage)
		assert.Contains(t, usage.Error(), "--name cannot be empty")
		assert.Empty(t, api.archiveRequests())
	})
}

func TestStreamsUpdate(t *testing.T) {
	t.Run("changed flags are merged into the current stream", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "",
			"streams", "update", paymentsID, "--destination", euID)
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		assert.JSONEq(t,
			`{"archive_destination_id":"`+euID+`","name":"payments","stream_selector":"{service=\"payments\"}"}`,
			api.body("PUT /archive-replay/api/v1/streams/"+paymentsID))
		doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "eu-archive", doc["archive_destination_name"])
	})

	t.Run("the destination name is looked up when it did not change", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "",
			"streams", "update", paymentsID, "--selector", `{service=~"payments|billing"}`)
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		assert.JSONEq(t,
			`{"archive_destination_id":"`+prodID+`","name":"payments","stream_selector":"{service=~\"payments|billing\"}"}`,
			api.body("PUT /archive-replay/api/v1/streams/"+paymentsID))
		doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "prod-archive", doc["archive_destination_name"])
	})

	t.Run("no flags is a usage error", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "streams", "update", paymentsID)

		var usage *gcxerrors.UsageError
		require.ErrorAs(t, res.err, &usage)
		assert.Empty(t, api.archiveRequests())
	})
}

func TestStreamsDelete(t *testing.T) {
	t.Run("the prompt names the stream", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "y\n", "streams", "delete", paymentsID)
		require.NoError(t, res.err, "stderr: %s", res.stderr)
		assert.Contains(t, res.stderr, `Delete archive stream "payments" ({service="payments"})?`)
		assert.Contains(t, api.archiveRequests(), "DELETE /archive-replay/api/v1/streams/"+paymentsID)
		assert.Equal(t, "✔ Deleted archive stream \""+paymentsID+"\"\n", res.stdout)
	})

	t.Run("--force skips the lookup", func(t *testing.T) {
		api := &fakeArchiveAPI{}
		res := runArchiveCmd(t, newTestLoader(t, api), false, "", "streams", "delete", paymentsID, "--force")
		require.NoError(t, res.err, "stderr: %s", res.stderr)
		assert.Equal(t, []string{"DELETE /archive-replay/api/v1/streams/" + paymentsID}, api.archiveRequests())
	})
}

func TestAgentMode_OneJSONValue(t *testing.T) {
	commands := [][]string{
		{"principal", "get"},
		{"destinations", "list"},
		{"destinations", "get", prodID},
		{"destinations", "create", "--name", "prod-archive", "--type", "gcs", "--bucket", "acme-logs"},
		{"destinations", "create", "--name", "prod-archive", "--type", "gcs", "--bucket", "acme-logs", "--dry-run"},
		{"destinations", "delete", prodID, "--force"},
		{"streams", "list"},
		{"streams", "get", paymentsID},
		{"streams", "create", "--destination", prodID, "--name", "payments", "--selector", `{service="payments"}`},
		{"streams", "update", paymentsID, "--name", "payments-v2"},
		{"streams", "delete", paymentsID, "--force"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			api := &fakeArchiveAPI{}
			res := runArchiveCmd(t, newTestLoader(t, api), true, "", args...)
			require.NoError(t, res.err, "stderr: %s", res.stderr)
			decodeOneJSONValue(t, res.stdout)
		})
	}
}
