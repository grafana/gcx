package resources //nolint:testpackage // Exercises the search command through its production Cobra factory.

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/rest"
)

type searchTestLoader struct {
	cfg   config.NamespacedRESTConfig
	calls int
}

func setSearchTestAgentMode(t *testing.T, value string) {
	t.Helper()
	previous := agent.IsAgentMode()
	t.Setenv("GCX_AGENT_MODE", value)
	agent.ResetForTesting()
	t.Cleanup(func() { agent.SetFlag(previous) })
}

func (l *searchTestLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.calls++
	return l.cfg, nil
}

func TestSearchRejectsInvalidInputBeforeLoadingConfig(t *testing.T) {
	setSearchTestAgentMode(t, "false")
	cases := [][]string{
		{}, {""}, {"  "}, {string([]byte{0xff})}, {"x", "y"},
		{"x", "--limit=-1"}, {"x", "--limit=0"}, {"x", "--limit=201"},
		{"x", "--resource=dashboard"}, {"x", "--resource=*.grafana.app"},
		{"x", "--resource=dashboards.dashboard.grafana.app", "--resource=dashboards.dashboard.grafana.app"},
		{"x", "--folder=*"}, {"x", "--folder=team", "--folder=team"},
		{"x", "--folder=", "--folder="}, {"x", "--folder=general", "--folder="},
	}
	for _, args := range cases {
		loader := &searchTestLoader{}
		cmd := searchCmd(loader)
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		err := cmd.Execute()
		var usage *gcxerrors.UsageError
		require.ErrorAs(t, err, &usage, "%v: %v", args, err)
		require.Zero(t, loader.calls, "%v", args)
	}
}

func TestSearchUsesOneLimitErrorBeforeLoadingConfig(t *testing.T) {
	for _, limit := range []string{"-1", "0", "201"} {
		loader := &searchTestLoader{}
		cmd := searchCmd(loader)
		cmd.SetArgs([]string{"q", "--limit=" + limit})
		err := cmd.Execute()
		var usage *gcxerrors.UsageError
		require.ErrorAs(t, err, &usage)
		require.ErrorContains(t, err, "--limit must be between 1 and 200")
		require.Zero(t, loader.calls)
	}
}

func TestSearchRejectsNormalizedDuplicateFoldersBeforeLoadingConfig(t *testing.T) {
	for _, tc := range []struct {
		folders []string
		value   string
	}{
		{folders: []string{"team", "team"}, value: "team"},
		{folders: []string{"", ""}, value: ""},
		{folders: []string{"general", ""}, value: ""},
	} {
		loader := &searchTestLoader{}
		cmd := searchCmd(loader)
		args := make([]string, 0, 1+len(tc.folders))
		args = append(args, "q")
		for _, folder := range tc.folders {
			args = append(args, "--folder="+folder)
		}
		cmd.SetArgs(args)
		err := cmd.Execute()
		var usage *gcxerrors.UsageError
		require.ErrorAs(t, err, &usage)
		require.ErrorContains(t, err, "duplicate --folder "+strconv.Quote(tc.value))
		require.Zero(t, loader.calls)
	}
}

func TestSearchPreservesQueryAndMapsOverridesAndRootFolder(t *testing.T) {
	setSearchTestAgentMode(t, "false")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/prefix/apis/search.grafana.app/v0alpha1/namespaces/org-1/global/search/hybrid", r.URL.Path)
		var body bytes.Buffer
		_, _ = body.ReadFrom(r.Body)
		assert.JSONEq(t, `{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchQuery","query":"  memory  ","limit":1,"resources":[{"resource":"dashboards","group":"dashboard.grafana.app"}],"filters":[{"field":"folder","values":["","team"]}]}`, body.String())
		_, _ = w.Write([]byte(`{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[]}`))
	}))
	defer server.Close()
	loader := &searchTestLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL + "/prefix"}, Namespace: "org-1"}}
	cmd := searchCmd(loader)
	cmd.SetArgs([]string{"  memory  ", "--limit=1", "--resource=dashboards.dashboard.grafana.app", "--folder=", "--folder=team", "-o=json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), `"items": []`)
}

func TestSearchRendersOrderedMixedResultsInEveryOutput(t *testing.T) {
	setSearchTestAgentMode(t, "false")
	const payload = `{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[{"resource":{"resource":"dashboards","group":"dashboard.grafana.app","kind":"Dashboard","name":"first"},"score":0.032,"title":"Memory","folder":"team","folderTitle":"Team","managedBy":{"kind":"git","id":"repo"},"chunks":[{"subresource":"panel/5","content":"memory pressure"},{"subresource":"panel/8","content":"heap"}]},{"resource":{"resource":"folders","group":"folder.grafana.app","kind":"Folder","name":"second"},"score":0.02,"title":"Platform","folder":"","chunks":[{"subresource":"","content":"infrastructure"}]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer server.Close()
	for _, tc := range []struct {
		args     []string
		contains []string
		excludes []string
	}{
		{args: nil, contains: []string{"TYPE", "NAME", "TITLE", "FOLDER", "SCORE", "dashboards.dashboard.grafana.app", "folders.folder.grafana.app", "Team", "General", "0.032"}, excludes: []string{"EXCERPT"}},
		{args: []string{"-o=wide"}, contains: []string{"EXCERPT", "SUBRESOURCE", "memory pressure", "panel/5"}},
		{args: []string{"-o=json"}, contains: []string{`"apiVersion"`, `"managedBy"`, `"panel/8"`, `"items"`}},
		{args: []string{"-o=yaml"}, contains: []string{"apiVersion:", "managedBy:", "panel/8"}},
		{args: []string{"--json=list"}, contains: []string{"resource.group", "chunks"}},
		{args: []string{"--json=resource.name,title"}, contains: []string{`"resource.name": "first"`, `"resource.name": "second"`, `"title": "Memory"`}},
		{args: []string{"--jq=.items[0].resource.name"}, contains: []string{`"first"`}},
	} {
		loader := &searchTestLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"}}
		cmd := searchCmd(loader)
		cmd.SetArgs(append([]string{"memory"}, tc.args...))
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute(), "%v", tc.args)
		for _, value := range tc.contains {
			require.Contains(t, out.String(), value, "%v", tc.args)
		}
		for _, value := range tc.excludes {
			require.NotContains(t, out.String(), value, "%v", tc.args)
		}
	}
}

func TestSearchRoundsHumanScoresAndPreservesStructuredScoresAndOrder(t *testing.T) {
	setSearchTestAgentMode(t, "false")
	const payload = `{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[{"resource":{"resource":"dashboards","group":"dashboard.grafana.app","name":"first"},"score":0.6000000000000001,"title":"First","folder":"","chunks":[]},{"resource":{"resource":"folders","group":"folder.grafana.app","name":"second"},"score":0.0000001,"title":"Second","folder":"","chunks":[]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer server.Close()

	for _, format := range []string{"table", "wide", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			cmd := searchCmd(&searchTestLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"}})
			cmd.SetArgs([]string{"memory", "-o=" + format})
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())

			if format == "table" || format == "wide" {
				text := out.String()
				require.Regexp(t, `(?:^|\s)0\.6000(?:\s|$)`, text)
				require.Regexp(t, `(?:^|\s)0\.0000(?:\s|$)`, text)
				require.NotContains(t, text, "0.6000000000000001")
				require.Less(t, strings.Index(text, "first"), strings.Index(text, "second"))
				return
			}

			var result search.Results
			if format == "json" {
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			} else {
				require.NoError(t, yaml.Unmarshal(out.Bytes(), &result))
			}
			require.Len(t, result.Items, 2)
			require.Equal(t, "first", result.Items[0].Resource.Name)
			require.Equal(t, math.Float64bits(0.6000000000000001), math.Float64bits(result.Items[0].Score))
			require.Equal(t, "second", result.Items[1].Resource.Name)
			require.Equal(t, math.Float64bits(0.0000001), math.Float64bits(result.Items[1].Score))
		})
	}
}

func TestSearchPassesQueryAndFoldersBeyondFormerLocalCaps(t *testing.T) {
	setSearchTestAgentMode(t, "false")
	longQuery := strings.Repeat("é", 501)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query   string `json:"query"`
			Limit   int    `json:"limit"`
			Filters []struct {
				Values []string `json:"values"`
			} `json:"filters"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, longQuery, body.Query)
		assert.Equal(t, 200, body.Limit)
		if assert.Len(t, body.Filters, 1) {
			if assert.Len(t, body.Filters[0].Values, 1001) {
				assert.Equal(t, "team-0", body.Filters[0].Values[0])
				assert.Equal(t, "team-1000", body.Filters[0].Values[1000])
			}
		}
		_, _ = w.Write([]byte(`{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[]}`))
	}))
	defer server.Close()
	loader := &searchTestLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"}}
	cmd := searchCmd(loader)
	args := make([]string, 0, 1003)
	args = append(args, longQuery, "--limit=200")
	for i := range 1001 {
		args = append(args, "--folder=team-"+strconv.Itoa(i))
	}
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
}

func TestSearchHelpDeclaresExperimentalContract(t *testing.T) {
	cmd := searchCmd(&searchTestLoader{})
	require.True(t, strings.HasPrefix(cmd.Short, "[experimental]"))
	require.True(t, strings.HasPrefix(cmd.Long, experimentalSearchPreamble))
	require.Equal(t, "experimental", cmd.Annotations["agent.stability"])
	require.Equal(t, "large", cmd.Annotations["agent.token_cost"])
	require.NotEmpty(t, cmd.Annotations["agent.llm_hint"])
}

func TestSearchEmptyTableIsClearAndSuccessful(t *testing.T) {
	setSearchTestAgentMode(t, "false")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[]}`))
	}))
	defer server.Close()
	cmd := searchCmd(&searchTestLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"}})
	cmd.SetArgs([]string{"nothing"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	require.Equal(t, "No resources found\n", out.String())
}

func TestSearchAgentOutputSpillsOneReceiptAndPreservesFullResult(t *testing.T) {
	setSearchTestAgentMode(t, "true")
	t.Setenv("GCX_AGENT_SPILL_BYTES", "100")
	t.Setenv("TMPDIR", t.TempDir())
	const payload = `{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[{"resource":{"group":"folder.grafana.app","resource":"folders","kind":"Folder","name":"platform"},"score":0.032,"title":"Platform","folder":"","chunks":[{"subresource":"","content":"Platform infrastructure"}]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer server.Close()
	cmd := searchCmd(&searchTestLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"}})
	cmd.SetArgs([]string{"platform"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var receipt map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &receipt))
	require.Equal(t, "gcx.spill_reference", receipt["type"])
	path, ok := receipt["spilled_to"].(string)
	require.True(t, ok)
	defer os.Remove(path)
	spilled, err := os.ReadFile(path)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(spilled, &result))
	require.Equal(t, "HybridSearchResults", result["kind"])
	require.Len(t, result["items"], 1)
}
