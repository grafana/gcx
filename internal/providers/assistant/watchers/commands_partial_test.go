package watchers //nolint:testpackage // Verify command result ownership and codec behavior.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
)

type listFixture struct {
	items            []clientwatchers.Watcher
	collectionStatus int
	detailStatus     map[string]int
	enrollmentStatus map[string]int
}

func partialListServer(t *testing.T, fixture listFixture) *httptest.Server {
	t.Helper()
	const collection = "/api/plugins/grafana-assistant-app/resources/api/v1/watcher-agents"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == collection {
			if fixture.collectionStatus != 0 {
				w.WriteHeader(fixture.collectionStatus)
				return
			}
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"agents": fixture.items}}))
			return
		}
		ref := strings.TrimPrefix(r.URL.Path, collection+"/")
		if id, enrollment := strings.CutSuffix(ref, "/auto-calibration"); enrollment {
			if status := fixture.enrollmentStatus[id]; status != 0 {
				w.WriteHeader(status)
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"message": "raw-secret-value\nSuggestions: ╭diagnostic-box╮"}))
				return
			}
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"enabled": false}}))
			return
		}
		if status := fixture.detailStatus[ref]; status != 0 {
			w.WriteHeader(status)
			return
		}
		for _, item := range fixture.items {
			if item.ID == ref {
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": item}))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func partialListClient(t *testing.T, fixture listFixture) *clientwatchers.Client {
	t.Helper()
	server := partialListServer(t, fixture)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
	require.NoError(t, err)
	return clientwatchers.NewClient(base)
}

func listItem(id, title string) clientwatchers.Watcher {
	item := fixture()
	item.ID = id
	item.Name = title
	return item
}

func decodeSingleListResult(t *testing.T, source io.Reader) listResult {
	t.Helper()
	decoder := json.NewDecoder(source)
	var result listResult
	require.NoError(t, decoder.Decode(&result))
	var extra any
	require.ErrorIs(t, decoder.Decode(&extra), io.EOF, "list must emit exactly one JSON value")
	return result
}

func TestListPartialFailuresKeepReadableItems(t *testing.T) {
	good := listItem("11111111-1111-4111-8111-111111111111", "Readable")
	denied := listItem("22222222-2222-4222-8222-222222222222", "Denied")
	gone := listItem("33333333-3333-4333-8333-333333333333", "Gone")
	client := partialListClient(t, listFixture{items: []clientwatchers.Watcher{good, denied, gone}, detailStatus: map[string]int{gone.ID: 404}, enrollmentStatus: map[string]int{denied.ID: 403}})
	cmd, out := outputCommand(t)
	err := runList(cmd, client, "default", &listOpts{IO: cmdio.Options{OutputFormat: "json"}})
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
	require.ErrorIs(t, err, clientwatchers.ErrPermissionDenied)
	assert.Nil(t, fail.ErrorToDetailedError(err), "reporter must not render a second document")
	result := decodeSingleListResult(t, out)
	require.Len(t, result.Items, 1)
	assert.False(t, result.Coverage.Complete)
	assert.Equal(t, "gcx.partial_result", result.Type)
	assert.Equal(t, "1", result.SchemaVersion)
	require.NotNil(t, result.Error)
	assert.Equal(t, gcxerrors.ExitPartialFailure, result.Error.ExitCode)
	assert.NotEmpty(t, result.Error.Suggestions)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, denied.ID, result.Failed[0].ID)
	assert.Equal(t, "denied", result.Failed[0].Name)
	assert.Equal(t, denied.Name, result.Failed[0].Title)
	assert.Contains(t, result.Failed[0].Message, "access denied")
	assertPlainListMessage(t, result.Failed[0].Message)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, gone.ID, result.Skipped[0].ID)
	assert.Contains(t, result.Skipped[0].Message, "disappeared")
}

func TestListSkipOnlyIsHonestSuccess(t *testing.T) {
	for _, readable := range []bool{false, true} {
		t.Run(map[bool]string{false: "all disappeared", true: "one readable"}[readable], func(t *testing.T) {
			gone := listItem("33333333-3333-4333-8333-333333333333", "Gone")
			items := []clientwatchers.Watcher{gone}
			if readable {
				items = append(items, listItem("11111111-1111-4111-8111-111111111111", "Readable"))
			}
			cmd, out := outputCommand(t)
			require.NoError(t, runList(cmd, partialListClient(t, listFixture{items: items, detailStatus: map[string]int{gone.ID: 404}}), "default", &listOpts{IO: cmdio.Options{OutputFormat: "json"}}))
			result := decodeSingleListResult(t, out)
			assert.False(t, result.Coverage.Complete)
			assert.NotNil(t, result.Items)
			assert.Empty(t, result.Failed)
			assert.Nil(t, result.Error)
			require.Len(t, result.Skipped, 1)
			assert.Equal(t, gone.ID, result.Skipped[0].ID)
			if readable {
				assert.Len(t, result.Items, 1)
			} else {
				assert.Empty(t, result.Items)
			}
		})
	}
}

func TestListAllItemFailuresStillEmitOnePartialResult(t *testing.T) {
	for _, status := range []int{403, 404, 501} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			denied := listItem("22222222-2222-4222-8222-222222222222", "Unreadable")
			cmd, out := outputCommand(t)
			err := runList(cmd, partialListClient(t, listFixture{items: []clientwatchers.Watcher{denied}, enrollmentStatus: map[string]int{denied.ID: status}}), "default", &listOpts{IO: cmdio.Options{OutputFormat: "json"}})
			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			result := decodeSingleListResult(t, out)
			assert.False(t, result.Coverage.Complete)
			assert.Empty(t, result.Items)
			assert.NotNil(t, result.Items)
			assert.Len(t, result.Failed, 1)
			assert.Empty(t, result.Skipped)
			require.NotNil(t, result.Error)
			assert.Equal(t, 4, result.Error.ExitCode)
		})
	}
}

func TestListCollectionFailuresAndCancellationRemainFatal(t *testing.T) {
	for _, status := range []int{403, 501} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			cmd, out := outputCommand(t)
			err := runList(cmd, partialListClient(t, listFixture{collectionStatus: status}), "default", &listOpts{IO: cmdio.Options{OutputFormat: "json"}})
			require.Error(t, err)
			var emitted *gcxerrors.EmittedError
			assert.NotErrorAs(t, err, &emitted)
			assert.Empty(t, out.String())
			if status == 403 {
				require.ErrorIs(t, err, clientwatchers.ErrPermissionDenied)
			} else {
				require.ErrorIs(t, err, clientwatchers.ErrCapabilityUnavailable)
			}
		})
	}
	cmd, out := outputCommand(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd.SetContext(ctx)
	err := runList(cmd, partialListClient(t, listFixture{}), "default", &listOpts{IO: cmdio.Options{OutputFormat: "json"}})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, out.String())
}

func TestListHumanOutputReportsIncompleteCoverage(t *testing.T) {
	for _, format := range []string{"text", "table", "wide"} {
		t.Run(format, func(t *testing.T) {
			good := listItem("11111111-1111-4111-8111-111111111111", "Readable")
			denied := listItem("22222222-2222-4222-8222-222222222222", "Denied")
			gone := listItem("33333333-3333-4333-8333-333333333333", "Gone")
			opts := &listOpts{}
			opts.setup((&cobra.Command{}).Flags())
			opts.IO.OutputFormat = format
			cmd, out := outputCommand(t)
			err := runList(cmd, partialListClient(t, listFixture{items: []clientwatchers.Watcher{good, denied, gone}, detailStatus: map[string]int{gone.ID: 404}, enrollmentStatus: map[string]int{denied.ID: 403}}), "default", opts)
			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			assert.Contains(t, out.String(), "Readable")
			assert.Contains(t, out.String(), "Coverage incomplete: 1 failed, 1 skipped")
			assert.Contains(t, out.String(), "Failed denied (ID "+denied.ID+")")
			assert.Contains(t, out.String(), "Skipped gone (ID "+gone.ID+")")
		})
	}
}

type rejectingWriter struct{}

func (rejectingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestListEncodeFailureDoesNotReturnEmittedError(t *testing.T) {
	denied := listItem("22222222-2222-4222-8222-222222222222", "Denied")
	cmd, _ := outputCommand(t)
	cmd.SetOut(rejectingWriter{})
	err := runList(cmd, partialListClient(t, listFixture{items: []clientwatchers.Watcher{denied}, enrollmentStatus: map[string]int{denied.ID: 403}}), "default", &listOpts{IO: cmdio.Options{OutputFormat: "json"}})
	require.ErrorIs(t, err, io.ErrClosedPipe)
	var emitted *gcxerrors.EmittedError
	assert.NotErrorAs(t, err, &emitted)
}

func assertPlainListMessage(t *testing.T, message string) {
	t.Helper()
	for _, unexpected := range []string{"\n", "╭", "╮", "┌", "│", "Suggestions:", "raw-secret-value"} {
		assert.NotContains(t, message, unexpected)
	}
}

func TestPublicListPartialResultFiniteDocument(t *testing.T) {
	for _, format := range []string{"json", "yaml", "agents"} {
		t.Run(format, func(t *testing.T) {
			good := listItem("11111111-1111-4111-8111-111111111111", "Readable")
			denied := listItem("22222222-2222-4222-8222-222222222222", "Denied")
			server := partialListServer(t, listFixture{items: []clientwatchers.Watcher{good, denied}, enrollmentStatus: map[string]int{denied.ID: 403}})
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte("current-context: test\ncontexts:\n  test:\n    grafana:\n      server: "+server.URL+"\n      token: fixture-token\n      stack-id: 12345\n"), 0600))
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(configPath)
			cmd := Commands(loader)
			_, out := outputCommand(t)
			cmd.SetOut(out)
			cmd.SetErr(io.Discard)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs([]string{"list", "-o", format})
			err := cmd.ExecuteContext(t.Context())
			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			assert.Equal(t, 4, emitted.Code)
			assert.Nil(t, fail.ErrorToDetailedError(err))
			decoder := yaml.NewYAMLOrJSONDecoder(out, 4096)
			var result listResult
			require.NoError(t, decoder.Decode(&result))
			var additional any
			require.ErrorIs(t, decoder.Decode(&additional), io.EOF)
			require.Len(t, result.Items, 1)
			assert.False(t, result.Coverage.Complete)
			require.Len(t, result.Failed, 1)
			assertPlainListMessage(t, result.Failed[0].Message)
			require.NotNil(t, result.Error)
			assert.Equal(t, 4, result.Error.ExitCode)
		})
	}
}

func TestPublicListFieldSelectionRetainsIncompleteMetadata(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "churn", true: "failed"}[failure], func(t *testing.T) {
			good := listItem("11111111-1111-4111-8111-111111111111", "Readable")
			omitted := listItem("22222222-2222-4222-8222-222222222222", "Omitted")
			fixture := listFixture{items: []clientwatchers.Watcher{good, omitted}}
			if failure {
				fixture.enrollmentStatus = map[string]int{omitted.ID: 403}
			} else {
				fixture.detailStatus = map[string]int{omitted.ID: 404}
			}
			server := partialListServer(t, fixture)
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("current-context: test\ncontexts:\n  test:\n    grafana:\n      server: "+server.URL+"\n      token: fixture-token\n      stack-id: 12345\n"), 0600))
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(path)
			cmd := Commands(loader)
			_, out := outputCommand(t)
			cmd.SetOut(out)
			cmd.SetErr(io.Discard)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs([]string{"list", "--json", "metadata.name"})
			err := cmd.ExecuteContext(t.Context())
			if failure {
				var emitted *gcxerrors.EmittedError
				require.ErrorAs(t, err, &emitted)
				assert.Equal(t, 4, emitted.Code)
			} else {
				require.NoError(t, err)
			}
			result := decodeSingleListResult(t, out)
			require.Len(t, result.Items, 1)
			assert.NotContains(t, result.Items[0], "spec")
			assert.False(t, result.Coverage.Complete)
			assert.Equal(t, "caller-visible", result.Coverage.Scope)
			if failure {
				require.NotNil(t, result.Error)
				assert.Equal(t, 4, result.Error.ExitCode)
				assert.Equal(t, "gcx.partial_result", result.Type)
				assert.Equal(t, "1", result.SchemaVersion)
				require.Len(t, result.Failed, 1)
				assert.Equal(t, omitted.ID, result.Failed[0].ID)
			} else {
				assert.Nil(t, result.Error)
				require.Len(t, result.Skipped, 1)
				assert.Equal(t, omitted.ID, result.Skipped[0].ID)
			}
		})
	}
}
