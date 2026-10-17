package watchers //nolint:testpackage // Exercise the real command constructors and mutation codecs.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/watcher"
	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/assistant/watchers/watcherstest"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func mutationFixture(t *testing.T, fixture http.Handler) *providers.ConfigLoader {
	t.Helper()
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("current-context: test\ncontexts:\n  test:\n    grafana:\n      server: "+server.URL+"\n      token: fixture-token\n      stack-id: 12345\n"), 0600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(path)
	return loader
}
func mutationCommand(t *testing.T, loader *providers.ConfigLoader, args []string, input string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := Commands(loader)
	out, diagnostic := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(diagnostic)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd, out, diagnostic
}
func commandManifest(title, id, fields string) string {
	annotations := ""
	if id != "" {
		annotations = "  annotations:\n    " + watcher.WatcherIDAnnotation + ": " + id + "\n"
	}
	return "apiVersion: " + watcher.WatcherAPIVersion + "\nkind: Watcher\nmetadata:\n  name: checkout-health\n" + annotations + "spec:\n  title: " + title + "\n  prompt: Watch errors\n" + fields
}
func savedCommandFixture() clientwatchers.Watcher {
	return clientwatchers.Watcher{ID: "11111111-1111-4111-8111-111111111111", Name: "Checkout health", Prompt: "Watch errors", TriggerIntervalSeconds: 600, Sensitivity: "balanced", Status: "draft"}
}

func TestPublicMutationStaticValidation(t *testing.T) {
	testutils.SetAgentMode(t, false)
	cases := []struct {
		name  string
		args  []string
		input string
	}{
		{"missing file", []string{"create"}, ""},
		{"empty file", []string{"create", "--file="}, ""},
		{"blank file", []string{"create", "--file= "}, ""},
		{"invalid output", []string{"create", "-f", "-", "-o", "unknown-format"}, commandManifest("Checkout health", "", "")},
		{"empty update file", []string{"update", "checkout-health", "--file="}, ""},
		{"extra create argument", []string{"create", "extra", "-f", "-"}, commandManifest("Checkout health", "", "")},
		{"missing update argument", []string{"update", "-f", "-"}, commandManifest("Checkout health", "", "")},
		{"empty update argument", []string{"update", " ", "-f", "-"}, commandManifest("Checkout health", "", "")},
		{"extra update argument", []string{"update", "one", "two", "-f", "-"}, commandManifest("Checkout health", "", "")},
		{"missing delete argument", []string{"delete", "--force"}, ""},
		{"empty delete argument", []string{"delete", " ", "--force"}, ""},
		{"empty stdin", []string{"create", "-f", "-"}, ""},
		{"multiple manifests", []string{"create", "-f", "-"}, commandManifest("Checkout health", "", "") + "---\n" + commandManifest("Other health", "", "")},
		{"unknown field", []string{"create", "-f", "-"}, commandManifest("Checkout health", "", "  unexpected: true\n")},
		{"unknown envelope field", []string{"create", "-f", "-"}, commandManifest("Checkout health", "", "") + "unexpected: true\n"},
		{"blank metadata name", []string{"create", "-f", "-"}, strings.Replace(commandManifest("Checkout health", "", ""), "name: checkout-health", "name: \" \"", 1)},
		{"missing metadata", []string{"create", "-f", "-"}, strings.Replace(commandManifest("Checkout health", "", ""), "metadata:\n  name: checkout-health\n", "", 1)},
		{"unsupported API version", []string{"create", "-f", "-"}, strings.Replace(commandManifest("Checkout health", "", ""), "v1alpha1", "v2", 1)},
		{"unsupported kind", []string{"create", "-f", "-"}, strings.Replace(commandManifest("Checkout health", "", ""), "kind: Watcher", "kind: Other", 1)},
		{"invalid field type", []string{"update", "checkout-health", "-f", "-"}, commandManifest("Checkout health", "", "  interval: 3\n")},
		{"invalid dependency", []string{"create", "-f", "-"}, commandManifest("Checkout health", "", "  notifications:\n    slack:\n      enabled: true\n")},
		{"missing file path", []string{"create", "-f", filepath.Join(t.TempDir(), "absent.yaml")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &watcherstest.Server{AllowWrites: true}
			cmd, out, _ := mutationCommand(t, mutationFixture(t, fixture), tc.args, tc.input)
			require.Error(t, cmd.ExecuteContext(t.Context()))
			require.Empty(t, out.String())
			require.Zero(t, fixture.CollectionReads(false)+fixture.CollectionReads(true))
			require.Zero(t, fixture.MutationCalls())
		})
	}
}

func TestPublicCreateFileAndUpdateIdentity(t *testing.T) {
	testutils.SetAgentMode(t, false)
	fixture := &watcherstest.Server{AllowWrites: true}
	loader := mutationFixture(t, fixture)
	path := filepath.Join(t.TempDir(), "watcher.yaml")
	require.NoError(t, os.WriteFile(path, []byte(commandManifest("Checkout health", "", "  interval: 20m\n  sensitivity: relaxed\n")), 0600))
	cmd, out, _ := mutationCommand(t, loader, []string{"create", "-f", path, "-o", "json"}, "")
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	var created mutationResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &created))
	require.Equal(t, "gcx.mutation", created.Type)
	require.Equal(t, "1", created.SchemaVersion)
	require.Equal(t, "created", created.Action)
	require.Equal(t, "checkout-health", created.Target.Name)
	require.NotEmpty(t, created.Target.ID)
	require.True(t, *created.Changed)
	requests := fixture.MutationRequests()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodPost, requests[0].Method)
	var payload clientwatchers.ConfigurationRequest
	require.NoError(t, json.Unmarshal(requests[0].Body, &payload))
	require.Equal(t, "Checkout health", *payload.Name)
	require.Equal(t, 1200, *payload.TriggerIntervalSeconds)
	require.Equal(t, "relaxed", *payload.Sensitivity)
	require.NotContains(t, string(requests[0].Body), "status")

	cmd, out, _ = mutationCommand(t, loader, []string{"create", "-f", path, "-o", "json"}, "")
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "already exists")
	require.Empty(t, out.String())
	require.Equal(t, 2, fixture.MutationCalls())

	cmd, out, _ = mutationCommand(t, loader, []string{"update", created.Target.ID, "-f", "-", "-o", "json"}, commandManifest("Renamed health", "22222222-2222-4222-8222-222222222222", ""))
	require.ErrorContains(t, cmd.ExecuteContext(t.Context()), "conflicts")
	require.Empty(t, out.String())
	require.Equal(t, 2, fixture.MutationCalls())

	cmd, out, _ = mutationCommand(t, loader, []string{"update", created.Target.ID, "-f", "-", "-o", "json"}, commandManifest("Renamed health", created.Target.ID, ""))
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	var updated mutationResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &updated))
	require.Equal(t, "updated", updated.Action)
	require.Equal(t, created.Target.ID, updated.Target.ID)
	require.Equal(t, "renamed-health", updated.Target.Name)
	require.True(t, *updated.Changed)
	require.Equal(t, http.MethodPut, fixture.MutationRequests()[2].Method)
	require.True(t, strings.HasSuffix(fixture.MutationRequests()[2].Path, "/"+created.Target.ID))
}

func TestPublicMutationFormatParityAndNoop(t *testing.T) {
	for _, output := range []string{"text", "table", "wide", "json", "yaml", "agents"} {
		t.Run(output, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			item := savedCommandFixture()
			fixture := &watcherstest.Server{AllowWrites: true, Current: [][]clientwatchers.Watcher{{item}}}
			loader := mutationFixture(t, fixture)
			cmd, out, _ := mutationCommand(t, loader, []string{"update", "checkout-health", "-f", "-", "-o", output}, commandManifest("Checkout health", item.ID, ""))
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			require.Contains(t, out.String(), "unchanged")
			require.Contains(t, out.String(), item.ID)
			require.Contains(t, out.String(), "checkout-health")
			require.Zero(t, fixture.MutationCalls())
			if output == "json" || output == "yaml" || output == "agents" {
				decoder := yaml.NewYAMLOrJSONDecoder(out, 4096)
				var got mutationResult
				require.NoError(t, decoder.Decode(&got))
				require.Equal(t, "unchanged", got.Action)
				require.False(t, *got.Changed)
				require.Equal(t, "unchanged", got.Outcome.Configuration)
			}
		})
	}
}

func TestPublicPartialCompletionFormatsAndSecretSafety(t *testing.T) {
	for _, output := range []string{"text", "table", "wide", "json", "yaml", "agents"} {
		t.Run(output, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			t.Setenv("WATCHER_URL", "https://sentinel-secret.example.invalid/hook")
			t.Setenv("WATCHER_TOKEN", "sentinel-secret-token")
			item := savedCommandFixture()
			fixture := &watcherstest.Server{AllowWrites: true, Current: [][]clientwatchers.Watcher{{item}}, EnrollmentWriteStatus: map[string]int{item.ID: 500}, ErrorBody: "sentinel-secret-token https://sentinel-secret.example.invalid/hook raw-private-payload"}
			fields := "  description: Updated description\n  automaticRecalibration:\n    enabled: true\n  notifications:\n    webhook:\n      enabled: true\n      url:\n        fromEnv: WATCHER_URL\n      bearerToken:\n        fromEnv: WATCHER_TOKEN\n"
			cmd, out, diagnostic := mutationCommand(t, mutationFixture(t, fixture), []string{"update", item.ID, "-f", "-", "-o", output}, commandManifest("Checkout health", item.ID, fields))
			err := cmd.ExecuteContext(t.Context())
			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			require.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			for _, content := range []string{out.String(), diagnostic.String(), err.Error()} {
				require.NotContains(t, content, "sentinel-secret")
				require.NotContains(t, content, "raw-private-payload")
			}
			require.Contains(t, out.String(), item.ID)
			require.Contains(t, out.String(), "saved")
			require.Contains(t, out.String(), "failed")
			require.Equal(t, 2, fixture.MutationCalls())
			if output == "json" || output == "yaml" || output == "agents" {
				decoder := yaml.NewYAMLOrJSONDecoder(out, 4096)
				var got mutationResult
				require.NoError(t, decoder.Decode(&got))
				require.Equal(t, "gcx.mutation", got.Type)
				require.Equal(t, "saved", got.Outcome.Configuration)
				require.Equal(t, "failed", got.Outcome.Enrollment)
				require.NotEmpty(t, got.Error)
				var extra any
				require.Error(t, decoder.Decode(&extra))
			}
		})
	}
}

func TestPublicDeleteConfirmationAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, ref, input   string
		force, auto, agent bool
		cancel             bool
	}{
		{name: "force ID", ref: savedCommandFixture().ID, force: true},
		{name: "force name", ref: "checkout-health", force: true},
		{name: "confirm", ref: "checkout-health", input: "yes\n"},
		{name: "cancel", ref: "checkout-health", input: "no\n", cancel: true},
		{name: "auto approve", ref: "checkout-health", auto: true},
		{name: "agent refuses", ref: "checkout-health", agent: true, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SetAgentMode(t, tc.agent)
			t.Setenv("GCX_AUTO_APPROVE", map[bool]string{false: "false", true: "true"}[tc.auto])
			item := savedCommandFixture()
			fixture := &watcherstest.Server{AllowWrites: true, Current: [][]clientwatchers.Watcher{{item}}, DetailStatus: map[string]int{item.ID: 501}, EnrollmentStatus: map[string]int{item.ID: 501}}
			args := []string{"delete", tc.ref, "-o", "json"}
			if tc.force {
				args = append(args, "--force")
			}
			cmd, out, _ := mutationCommand(t, mutationFixture(t, fixture), args, tc.input)
			err := cmd.ExecuteContext(t.Context())
			if tc.cancel {
				require.Error(t, err)
				if tc.agent {
					require.ErrorIs(t, err, providers.ErrAgentModeRequiresForce)
				} else {
					var detailed *gcxerrors.DetailedError
					require.ErrorAs(t, err, &detailed)
					require.Equal(t, gcxerrors.ExitCancelled, *detailed.ExitCode)
				}
				require.Zero(t, fixture.MutationCalls())
				require.Zero(t, fixture.CollectionReads(false)+fixture.CollectionReads(true))
				require.Empty(t, out.String())
				return
			}
			require.NoError(t, err)
			var got mutationResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			require.Equal(t, "deleted", got.Action)
			require.Equal(t, item.ID, got.Target.ID)
			require.Equal(t, "checkout-health", got.Target.Name)
			require.Equal(t, 1, fixture.MutationCalls())
			require.Equal(t, http.MethodDelete, fixture.MutationRequests()[0].Method)
			require.Zero(t, fixture.EnrollmentReads(item.ID))
			require.Equal(t, 1, fixture.CollectionReads(false))
			require.Equal(t, 1, fixture.CollectionReads(true))
		})
	}
}

func TestPublicDeleteAPIErrorAndNameCollision(t *testing.T) {
	testutils.SetAgentMode(t, false)
	item := savedCommandFixture()
	for _, tc := range []struct {
		name      string
		collision bool
		expected  string
		mutations int
	}{
		{"denied", false, "access denied", 1},
		{"ambiguous", true, "ambiguous", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &watcherstest.Server{AllowWrites: true, Current: [][]clientwatchers.Watcher{{item}}, DeleteStatus: map[string]int{item.ID: 403}, ErrorBody: "sentinel-secret raw-server-payload"}
			if tc.collision {
				other := item
				other.ID = "22222222-2222-4222-8222-222222222222"
				other.Name = "Checkout-health"
				fixture.Current[0] = append(fixture.Current[0], other)
			}
			cmd, out, _ := mutationCommand(t, mutationFixture(t, fixture), []string{"delete", "checkout-health", "--force", "-o", "json"}, "")
			err := cmd.ExecuteContext(t.Context())
			require.ErrorContains(t, err, tc.expected)
			if !tc.collision {
				require.ErrorIs(t, err, clientwatchers.ErrPermissionDenied)
			}
			require.NotContains(t, err.Error(), "sentinel-secret")
			require.NotContains(t, err.Error(), "raw-server-payload")
			require.Empty(t, out.String())
			require.Equal(t, tc.mutations, fixture.MutationCalls())
		})
	}
}

func TestPublicMutationAgentDefaultsAndWarnings(t *testing.T) {
	testutils.SetAgentMode(t, true)
	fixture := &watcherstest.Server{AllowWrites: true}
	cmd, out, diagnostic := mutationCommand(t, mutationFixture(t, fixture), []string{"create", "-f", "-"}, commandManifest("Checkout health", "", "  sensitivity: future-value\n"))
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	var got mutationResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Equal(t, "gcx.mutation", got.Type)
	require.Contains(t, diagnostic.String(), "spec.sensitivity")
	require.Contains(t, diagnostic.String(), "future-value")
	require.Contains(t, diagnostic.String(), "sensitive, balanced, relaxed")
	for _, verb := range []string{"create", "update", "delete"} {
		leaf, _, err := cmd.Find([]string{verb})
		require.NoError(t, err)
		require.Equal(t, agent.StabilityExperimental, leaf.Annotations[agent.AnnotationStability])
		require.Equal(t, "small", leaf.Annotations[agent.AnnotationTokenCost])
	}
}

func TestPublicCreateDeleteSupportedFormats(t *testing.T) {
	for _, output := range []string{"text", "table", "wide", "json", "yaml", "agents"} {
		t.Run(output, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			fixture := &watcherstest.Server{AllowWrites: true}
			loader := mutationFixture(t, fixture)
			cmd, out, _ := mutationCommand(t, loader, []string{"create", "-f", "-", "-o", output}, commandManifest("Checkout health", "", ""))
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			require.Contains(t, out.String(), "created")
			require.Contains(t, out.String(), "checkout-health")
			item := fixture.Current[0][0]
			require.Contains(t, out.String(), item.ID)
			cmd, out, _ = mutationCommand(t, loader, []string{"delete", item.ID, "--force", "-o", output}, "")
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			require.Contains(t, out.String(), "deleted")
			require.Contains(t, out.String(), item.ID)
			require.Contains(t, out.String(), "checkout-health")
			require.Equal(t, 3, fixture.MutationCalls())
		})
	}
}

func TestPublicCreatePartialCompletionInAgentMode(t *testing.T) {
	testutils.SetAgentMode(t, true)
	const id = "00000000-0000-4000-8000-000000000001"
	fixture := &watcherstest.Server{AllowWrites: true, EnrollmentWriteStatus: map[string]int{id: 500}, ErrorBody: "sentinel-secret raw-private-payload"}
	cmd, out, _ := mutationCommand(t, mutationFixture(t, fixture), []string{"create", "-f", "-"}, commandManifest("Checkout health", "", "  automaticRecalibration:\n    enabled: true\n"))
	err := cmd.ExecuteContext(t.Context())
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	require.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
	decoder := json.NewDecoder(out)
	var got mutationResult
	require.NoError(t, decoder.Decode(&got))
	require.Equal(t, "created", got.Action)
	require.Equal(t, id, got.Target.ID)
	require.True(t, *got.Changed)
	require.Equal(t, "saved", got.Outcome.Configuration)
	require.Equal(t, "failed", got.Outcome.Enrollment)
	require.Contains(t, got.Error, "gcx assistant watchers get "+id)
	require.NotContains(t, got.Error, "sentinel-secret")
	var extra any
	require.Error(t, decoder.Decode(&extra))
	require.Equal(t, 2, fixture.MutationCalls())
}
