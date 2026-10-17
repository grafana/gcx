package watchers //nolint:testpackage // Verify public command routing, receipts and codec coverage.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func commandVersion(version int64, definition string) clientwatchers.VersionDetail {
	return clientwatchers.VersionDetail{VersionSummary: clientwatchers.VersionSummary{Version: version, CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Actor: clientwatchers.VersionActor{Type: "user", UserID: "fixture-user"}, ChangeKind: "update", ChangeSummary: "Changed definition", ChangedFields: []string{"prompt"}}, Definition: json.RawMessage(definition)}
}
func TestVersionCommandsValidateBeforeIO(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, args := range [][]string{
		{"list-versions"}, {"list-versions", ""}, {"list-versions", "one", "two"}, {"list-versions", "one", "--limit=0"}, {"list-versions", "one", "--limit=101"}, {"list-versions", "one", "--before-version=0"}, {"list-versions", "one", "--before-version="}, {"list-versions", "one", "--before-version=-1"},
		{"versions", "get", "one", "0"}, {"versions", "get", "one", ""}, {"versions", "get", "one", "9223372036854775808"}, {"versions", "get", " ", "1"}, {"versions", "get", "one", "1", "--output="},
		{"versions", "diff", "one", "1"}, {"versions", "diff", "one", "1", "-2"}, {"versions", "diff", "one", "1", "2", "extra"},
		{"versions", "restore", "one", "1"}, {"versions", "restore", "one", "1", "--expected-current-version=0"}, {"versions", "restore", "one", "1", "--expected-current-version="}, {"versions", "restore", "one", "1", "--expected-current-version=2", "--summary=" + strings.Repeat("\u754c", 1001)},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			loader, log := lifecycleCommandFixture(t, savedCommandFixture(), nil)
			cmd, out, _ := mutationCommand(t, loader, args, "")
			require.Error(t, cmd.ExecuteContext(t.Context()))
			require.Empty(t, out.String())
			require.Empty(t, log.snapshot())
		})
	}
}
func TestVersionListCoverageUnderSelection(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, empty := range []bool{false, true} {
		t.Run(strconv.FormatBool(empty), func(t *testing.T) {
			item := savedCommandFixture()
			loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if strings.HasSuffix(r.URL.Path, "/revisions") {
					assert.Equal(t, "1", r.URL.Query().Get("limit"))
					assert.Equal(t, "9", r.URL.Query().Get("beforeVersion"))
					rows := []clientwatchers.VersionSummary{}
					page := map[string]any{"revisions": rows}
					if !empty {
						page["revisions"] = []clientwatchers.VersionSummary{commandVersion(8, `{}`).VersionSummary}
						page["nextBeforeVersion"] = 8
					}
					lifecycleEnvelope(w, page)
					return true
				}
				return false
			})
			cmd, out, _ := mutationCommand(t, loader, []string{"list-versions", item.ID, "--limit=1", "--before-version=9", "--json", "version", "-o", "json"}, "")
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			var got map[string]any
			decodeLifecycleOutput(t, out, &got)
			coverage, ok := got["coverage"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "caller-visible-retained-finalized-history", coverage["scope"])
			require.EqualValues(t, 9, coverage["beforeVersion"])
			require.Equal(t, false, coverage["snapshot"])
			rows, ok := got["items"].([]any)
			require.True(t, ok)
			if empty {
				require.Empty(t, rows)
				require.NotContains(t, coverage, "nextBeforeVersion")
			} else {
				require.Len(t, rows, 1)
				require.EqualValues(t, 8, coverage["nextBeforeVersion"])
				require.Equal(t, map[string]any{"version": float64(8)}, rows[0])
			}
			require.Len(t, log.snapshot(), 2)
		})
	}
}
func TestVersionReadsAndDefinitionOnlyDiff(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, format := range []string{"json", "yaml", "agents", "text", "table", "wide"} {
		for _, verb := range []string{"get", "diff"} {
			t.Run(verb+"/"+format, func(t *testing.T) {
				item := savedCommandFixture()
				loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
					if strings.Contains(r.URL.Path, "/revisions/") {
						v := commandVersion(1, `{"prompt":"same","count":9007199254740993}`)
						if strings.HasSuffix(r.URL.Path, "/2") {
							v.Version = 2
							v.ChangeSummary = "Different audit only"
							v.Actor.UserID = "another-user"
						}
						lifecycleEnvelope(w, v)
						return true
					}
					return false
				})
				args := []string{"versions", verb, item.ID, "1"}
				if verb == "diff" {
					args = append(args, "2")
				}
				args = append(args, "-o", format)
				cmd, out, _ := mutationCommand(t, loader, args, "")
				require.NoError(t, cmd.ExecuteContext(t.Context()))
				require.NotEmpty(t, out.String())
				if format == "json" || format == "yaml" || format == "agents" {
					if verb == "get" {
						var got versionGetResult
						decodeLifecycleOutput(t, out, &got)
						require.Contains(t, string(got.Version.Definition), "9007199254740993")
					} else {
						var got versionDiffResult
						decodeLifecycleOutput(t, out, &got)
						require.Equal(t, "definition-only", got.Scope)
						require.Empty(t, got.Changes)
						require.NotNil(t, got.Changes)
					}
				}
				expected := 2
				if verb == "diff" {
					expected = 3
				}
				require.Len(t, log.snapshot(), expected)
			})
		}
	}
}
func TestVersionRestoreReceipts(t *testing.T) {
	testutils.SetAgentMode(t, false)
	cases := []struct {
		name, format, outcome, status string
		code                          int
		unknown, readbackFail         bool
	}{
		{name: "accepted draft", format: "json", outcome: "accepted", status: "draft"},
		{name: "accepted yaml", format: "yaml", outcome: "accepted", status: "paused"},
		{name: "human", format: "text", outcome: "accepted", status: "draft"},
		{name: "lost readback", format: "json", outcome: "saved-unverified", status: "paused", code: 4, readbackFail: true},
		{name: "later active", format: "json", outcome: "saved-unverified", status: "running", code: 4},
		{name: "unknown response", format: "json", outcome: "unknown", status: "paused", code: 1, unknown: true},
		{name: "unknown yaml", format: "yaml", outcome: "unknown", status: "paused", code: 1, unknown: true},
		{name: "unknown human", format: "text", outcome: "unknown", status: "paused", code: 1, unknown: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := savedCommandFixture()
			item.Status = "paused"
			item.DefinitionVersion = 2
			var submitted atomic.Bool
			loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method == http.MethodPost {
					assert.True(t, strings.HasSuffix(r.URL.Path, "/revisions/1/restore"))
					submitted.Store(true)
					if tc.unknown {
						_, _ = fmt.Fprint(w, `{"data":`)
						return true
					}
					v := commandVersion(3, `{"prompt":"restored","futureSetting":{"enabled":true}}`)
					target := int64(1)
					v.RollbackFromVersion = &target
					lifecycleEnvelope(w, v)
					return true
				}
				if r.Method == http.MethodGet && r.URL.Path == lifecycleAPIPath+"/"+item.ID && submitted.Load() {
					if tc.readbackFail {
						w.WriteHeader(http.StatusForbidden)
						return true
					}
					observed := item
					observed.DefinitionVersion = 3
					observed.Status = tc.status
					lifecycleEnvelope(w, observed)
					return true
				}
				return false
			})
			cmd, out, _ := mutationCommand(t, loader, []string{"versions", "restore", item.ID, "1", "--expected-current-version=2", "--summary=fixture reason", "--force", "-o", tc.format}, "")
			err := cmd.ExecuteContext(t.Context())
			if tc.code == 0 {
				require.NoError(t, err)
			} else {
				var emitted *gcxerrors.EmittedError
				require.ErrorAs(t, err, &emitted)
				require.Equal(t, tc.code, emitted.Code)
			}
			switch {
			case tc.format == "text" && tc.unknown:
				require.NotContains(t, out.String(), "Restored:")
				require.NotContains(t, out.String(), "Retained:")
			case tc.format == "text":
				require.Contains(t, out.String(), "Restored: futureSetting, prompt")
			default:
				var got versionRestoreResult
				decodeLifecycleOutput(t, out, &got)
				require.Equal(t, "gcx.mutation", got.Type)
				require.Equal(t, tc.outcome, got.Outcome)
				require.Equal(t, item.ID, got.Target.ID)
				require.EqualValues(t, 2, got.ExpectedCurrentVersion)
				require.EqualValues(t, 1, got.RequestedVersion)
				if tc.unknown {
					require.Nil(t, got.Scope)
					require.Nil(t, got.Changed)
					require.Nil(t, got.SavedVersion)
					require.Contains(t, got.Guidance, "retry")
				} else {
					require.NotNil(t, got.Scope)
					require.Equal(t, []string{"futureSetting", "prompt"}, got.Scope.Restored)
					require.NotNil(t, got.Changed)
					require.True(t, *got.Changed)
					require.EqualValues(t, 3, got.SavedVersion.Version)
				}
			}
			posts := 0
			for _, r := range log.snapshot() {
				if r.Method == http.MethodPost {
					posts++
					require.Contains(t, r.Body, `"expectedDefinitionVersion":2`)
					require.Contains(t, r.Body, `"summary":"fixture reason"`)
				}
			}
			require.Equal(t, 1, posts)
		})
	}
}
func TestVersionRestoreConfirmationAndPreconditions(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, tc := range []struct {
		name, state, input string
		expected           int64
		force              bool
	}{
		{"running", "running", "", 2, true}, {"draft", "draft", "", 2, true}, {"calibrating", "calibrating", "", 2, true}, {"stale", "paused", "", 1, true}, {"declined", "paused", "n\n", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := savedCommandFixture()
			item.Status = tc.state
			item.DefinitionVersion = 2
			loader, log := lifecycleCommandFixture(t, item, nil)
			args := []string{"versions", "restore", item.ID, "1", "--expected-current-version=" + strconv.FormatInt(tc.expected, 10), "-o", "json"}
			if tc.force {
				args = append(args, "--force")
			}
			cmd, out, _ := mutationCommand(t, loader, args, tc.input)
			require.Error(t, cmd.ExecuteContext(t.Context()))
			require.Empty(t, out.String())
			require.Len(t, log.snapshot(), 1)
		})
	}
}

func TestVersionDiffSelectionPreservesMetadata(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, empty := range []bool{false, true} {
		t.Run(strconv.FormatBool(empty), func(t *testing.T) {
			item := savedCommandFixture()
			loader, _ := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if strings.Contains(r.URL.Path, "/revisions/") {
					version := int64(1)
					definition := `{"prompt":"old"}`
					if strings.HasSuffix(r.URL.Path, "/2") {
						version = 2
						if !empty {
							definition = `{"prompt":"new"}`
						}
					}
					lifecycleEnvelope(w, commandVersion(version, definition))
					return true
				}
				return false
			})
			cmd, out, _ := mutationCommand(t, loader, []string{"versions", "diff", item.ID, "1", "2", "--json", "path,operation", "-o", "json"}, "")
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			var got struct {
				Name        string              `json:"name"`
				ID          string              `json:"id"`
				FromVersion int64               `json:"fromVersion"`
				ToVersion   int64               `json:"toVersion"`
				Scope       string              `json:"scope"`
				Changes     []map[string]string `json:"changes"`
			}
			decodeLifecycleOutput(t, out, &got)
			require.Equal(t, "definition-only", got.Scope)
			require.EqualValues(t, 1, got.FromVersion)
			require.EqualValues(t, 2, got.ToVersion)
			require.Equal(t, item.ID, got.ID)
			require.Equal(t, "checkout-health", got.Name)
			if empty {
				require.Empty(t, got.Changes)
				require.NotNil(t, got.Changes)
			} else {
				require.Equal(t, []map[string]string{{"path": "/prompt", "operation": "replace"}}, got.Changes)
			}
		})
	}
}

func TestVersionParseNamesRejectedPositional(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, value := range []string{"abc", "0", "", "9223372036854775808"} {
		for _, tc := range []struct {
			name string
			args []string
		}{
			{"VERSION", []string{"versions", "get", "fixture", value}},
			{"FROM_VERSION", []string{"versions", "diff", "fixture", value, "2"}},
			{"TO_VERSION", []string{"versions", "diff", "fixture", "1", value}},
			{"VERSION", []string{"versions", "restore", "fixture", value, "--expected-current-version=2", "--force"}},
		} {
			t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
				loader, log := lifecycleCommandFixture(t, savedCommandFixture(), nil)
				cmd, out, _ := mutationCommand(t, loader, tc.args, "")
				err := cmd.ExecuteContext(t.Context())
				require.ErrorContains(t, err, tc.name+" must be a positive integer")
				require.ErrorContains(t, err, fmt.Sprintf("got %q", value))
				require.ErrorContains(t, err, "gcx assistant watchers list-versions WATCHER")
				require.Empty(t, out.String())
				require.Empty(t, log.snapshot())
			})
		}
	}
}

func TestVersionListHumanCursor(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, before := range []int64{0, 9} {
		t.Run(strconv.FormatInt(before, 10), func(t *testing.T) {
			item := savedCommandFixture()
			loader, _ := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if strings.HasSuffix(r.URL.Path, "/revisions") {
					lifecycleEnvelope(w, map[string]any{"revisions": []clientwatchers.VersionSummary{}})
					return true
				}
				return false
			})
			args := []string{"list-versions", item.ID, "-o", "text"}
			want := "before: newest"
			if before != 0 {
				args = append(args, "--before-version="+strconv.FormatInt(before, 10))
				want = "before: 9"
			}
			cmd, out, _ := mutationCommand(t, loader, args, "")
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			require.Contains(t, out.String(), want)
			require.NotContains(t, out.String(), "before: 0")
		})
	}
}
