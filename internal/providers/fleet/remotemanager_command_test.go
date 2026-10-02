package fleet //nolint:testpackage // Drives unexported remote-manager command constructors.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func humanMode(t *testing.T) {
	t.Helper()
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })
}

func remoteManagerServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.StripPrefix(fleetProxyPrefix, handler))
	t.Cleanup(server.Close)
	return server
}

func decodeBody(t *testing.T, r *http.Request, dest any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		t.Errorf("decode body: %v", err)
	}
}

func execFleet(t *testing.T, cmd *cobra.Command, args []string, in string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(in))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestClusterListDiscoversFieldsWithoutAPICall(t *testing.T) {
	humanMode(t)
	called := false
	server := remoteManagerServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterListCommand()
	stdout, _, err := execFleet(t, cmd, []string{"--json", "list"}, "")
	require.NoError(t, err)
	assert.False(t, called)
	fields := strings.Fields(stdout)
	assert.Contains(t, fields, "id")
	assert.Contains(t, fields, "name")
	assert.Contains(t, fields, "namespace")
}

func TestClusterListEmptyJSONIsAnArray(t *testing.T) {
	humanMode(t)
	server := remoteManagerServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/remotemanager.v1.RemoteManagerService/ListClusters", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterListCommand()
	stdout, _, err := execFleet(t, cmd, []string{"-o", "json", "--limit", "0"}, "")
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, stdout)
}

func TestClusterListTruncatesAfterFetchingTheFullSet(t *testing.T) {
	humanMode(t)
	server := remoteManagerServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"clusters": []map[string]any{
				{"id": "a", "name": "one"},
				{"id": "b", "name": "two"},
			},
		})
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterListCommand()
	stdout, stderr, err := execFleet(t, cmd, []string{"-o", "json", "--limit", "1"}, "")
	require.NoError(t, err)
	var clusters []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &clusters))
	require.Len(t, clusters, 1)
	assert.Contains(t, stderr, "showing first 1 of 2")
}

func TestClusterCreateValidatesIDBeforeAPICall(t *testing.T) {
	humanMode(t)
	called := false
	server := remoteManagerServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterCreateCommand()
	_, _, err := execFleet(t, cmd, []string{"--id", "  "}, "")
	require.ErrorContains(t, err, "--id is required")
	assert.False(t, called)
}

func TestClusterUpdateSendsReplacement(t *testing.T) {
	humanMode(t)
	var got map[string]string
	server := remoteManagerServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeBody(t, r, &got)
		_ = json.NewEncoder(w).Encode(got)
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterUpdateCommand()
	stdout, _, err := execFleet(t, cmd, []string{"cluster-a", "--name", "staging", "-o", "json"}, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"id": "cluster-a", "name": "staging", "namespace": ""}, got)
	assert.Contains(t, stdout, `"namespace": ""`)
}

func TestClusterDeleteConfirmation(t *testing.T) {
	humanMode(t)

	t.Run("declined delete does not call the API", func(t *testing.T) {
		called := false
		server := remoteManagerServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		}))
		cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterDeleteCommand()
		stdout, _, err := execFleet(t, cmd, []string{"cluster-a"}, "n\n")
		require.NoError(t, err)
		assert.Empty(t, stdout)
		assert.False(t, called)
	})

	t.Run("force deletes", func(t *testing.T) {
		var got map[string]string
		server := remoteManagerServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decodeBody(t, r, &got)
			_, _ = io.WriteString(w, `{}`)
		}))
		cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterDeleteCommand()
		stdout, _, err := execFleet(t, cmd, []string{"cluster-a", "--force", "-o", "json"}, "")
		require.NoError(t, err)
		assert.Equal(t, "cluster-a", got["id"])
		assert.Contains(t, stdout, `"action": "deleted"`)
		assert.Contains(t, stdout, `"kind": "Cluster"`)
	})
}

func TestClusterDeleteAgentModeRequiresForce(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	called := false
	server := remoteManagerServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterDeleteCommand()
	_, _, err := execFleet(t, cmd, []string{"cluster-a"}, "")
	require.ErrorContains(t, err, "--force")
	assert.False(t, called)
}

func TestCollectorCRListOmitsEmptyClusterFilter(t *testing.T) {
	humanMode(t)
	server := remoteManagerServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		assert.JSONEq(t, `{}`, string(body))
		_, _ = io.WriteString(w, `{"collectorCrs":[]}`)
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newCollectorCRListCommand()
	stdout, _, err := execFleet(t, cmd, []string{"--cluster", "  ", "-o", "json", "--limit", "0"}, "")
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, stdout)
}

func TestCollectorCRCreateRejectsBothSpecFormsBeforeAPICall(t *testing.T) {
	humanMode(t)
	called := false
	server := remoteManagerServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newCollectorCRCreateCommand()
	_, _, err := execFleet(t, cmd, []string{"--id", "cr-a", "--cluster", "cluster-a", "--spec", "a", "--spec-file", "spec.yaml"}, "")
	require.ErrorContains(t, err, "mutually exclusive")
	assert.False(t, called)
}

func TestCollectorCRCreateKeepsSpecBytesAndReturnsRevision(t *testing.T) {
	humanMode(t)
	var got map[string]string
	server := remoteManagerServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeBody(t, r, &got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": got["id"], "clusterId": got["clusterId"], "name": got["name"],
			"namespace": got["namespace"], "release": got["release"], "spec": got["spec"],
			"revision": "rev-1",
		})
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newCollectorCRCreateCommand()
	stdout, _, err := execFleet(t, cmd, []string{
		"--id", "cr-a",
		"--cluster", "cluster-a",
		"--name", "metrics",
		"--spec-file", "-",
		"-o", "json",
	}, "spec: {}\n")
	require.NoError(t, err)
	assert.Equal(t, "spec: {}\n", got["spec"])
	assert.NotContains(t, got, "revision")
	assert.Contains(t, stdout, `"revision": "rev-1"`)
	assert.Contains(t, stdout, `"spec": "spec: {}\n"`)
}

func TestCollectorCRUpdateRequiresClusterBeforeAPICall(t *testing.T) {
	humanMode(t)
	called := false
	server := remoteManagerServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newCollectorCRUpdateCommand()
	_, _, err := execFleet(t, cmd, []string{"cr-a"}, "")
	require.ErrorContains(t, err, "--cluster is required")
	assert.False(t, called)
}

func TestCollectorCRListNegativeLimitBeforeAPICall(t *testing.T) {
	humanMode(t)
	called := false
	server := remoteManagerServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newCollectorCRListCommand()
	_, _, err := execFleet(t, cmd, []string{"--limit", "-1"}, "")
	require.ErrorContains(t, err, "limit")
	assert.False(t, called)
}

func TestClusterListTable(t *testing.T) {
	humanMode(t)
	withPlainColors(t)
	server := remoteManagerServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"clusters": []map[string]any{{"id": "cluster-a", "name": "prod", "namespace": "alloy"}},
		})
	}))
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: server.URL}}).newClusterListCommand()
	stdout, _, err := execFleet(t, cmd, []string{"--limit", "0"}, "")
	require.NoError(t, err)
	assert.Contains(t, stdout, "ID")
	assert.Contains(t, stdout, "NAMESPACE")
	assert.Contains(t, stdout, "cluster-a")
	assert.Contains(t, stdout, "alloy")
}
