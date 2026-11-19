package errortracking_test

import (
	"cmp"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type domainRun struct {
	Exit     int      `json:"exit"`
	Error    string   `json:"error"`
	Connects []string `json:"connects"`
}

func TestUpsertUploadDomainRunsInChild(t *testing.T) {
	server, upload := os.Getenv("ET_DOMAIN_SERVER"), os.Getenv("ET_DOMAIN_UPLOAD")
	if server == "" {
		return
	}
	var mu sync.Mutex
	var connects []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			mu.Lock()
			connects = append(connects, r.Host)
			mu.Unlock()
			http.Error(w, "no tunnels here", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/bootdata" {
			if os.Getenv("ET_DOMAIN_NO_STACK_ID") != "" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, `{"settings":{"namespace":"stacks-123"}}`)
			return
		}
		if r.URL.Path == "/api/instances/test" {
			stack := os.Getenv("ET_DOMAIN_STACK")
			if stack == "none" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, strings.ReplaceAll(stack, "{server}", server))
			return
		}
		_, _ = io.WriteString(w, `{"id":314,"name":"web","sourceMapUploadUrl":"https://`+upload+`"}`)
	}))
	defer proxy.Close()
	dir := scratchEnv(t)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	file := write(t, dir, "main.js.map", okMap)
	cfg := configFor(t, dir, "http://"+server)
	cloudEnv(t, proxy.URL)
	t.Setenv("GRAFANA_CLOUD_TOKEN", "synthetic-cloud-credential")
	_, err := runUpsert(t, io.Discard, "--config", cfg, "sourcemaps", "upsert", "--project", "314", "--release", "v1", "-f", file)
	run := domainRun{Exit: exitCode(err), Connects: connects}
	if err != nil {
		run.Error = rendered(t, err)
	}
	b, err := json.Marshal(run)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("ET_DOMAIN_OUT"), b, 0o600))
}

func TestUpsertUploadDomain(t *testing.T) {
	const elsewhere = `{"id":123,"slug":"test","url":"https://other.example.com"}`
	const atServer = `{"id":123,"slug":"test","url":"https://{server}"}`
	const stack456 = `{"id":456,"slug":"test","url":"https://{server}"}`
	for _, tc := range []struct {
		name, server, upload string
		allowed              bool
		noStackID, noLookup  bool
		stack, want          string
	}{
		{name: "a stack on its own domain uploads to that domain", server: "x.grafana.net", upload: "upload.grafana.net", allowed: true},
		{name: "a suffix shared with other owners is not one domain", server: "acme.co.uk", upload: "evil.co.uk", allowed: false},
		{name: "a custom domain uploads within itself", server: "grafana.acme.co.uk", upload: "upload.acme.co.uk", allowed: true},
		{name: "a custom domain does not reach another domain", server: "grafana.acme.co.uk", upload: "upload.grafana.net", allowed: false},
		{name: "another host under the stack's own domain is allowed", server: "a.grafana.net", upload: "upload.grafana.net", allowed: true},
		{name: "a shared hosting host may upload to itself", server: "app.herokuapp.com", upload: "app.herokuapp.com", allowed: true},
		{name: "a Cloud stack on a private suffix uploads to its sibling when the lookup confirms the ID and the host", server: "stack.herokuapp.com", upload: "symbol-upload.herokuapp.com", allowed: true},
		{name: "and so when the server reports no stack ID and the lookup names the server", server: "stack.herokuapp.com", upload: "symbol-upload.herokuapp.com", noStackID: true, stack: stack456, allowed: true},
		{name: "a stack ID the server reports never confirms a lookup naming another host", server: "stack.herokuapp.com", upload: "symbol-upload.herokuapp.com", stack: elsewhere},
		{name: "a stack ID the server reports never confirms a lookup with another ID", server: "stack.herokuapp.com", upload: "symbol-upload.herokuapp.com", stack: stack456},
		{name: "without a stack ID, a lookup naming another host is refused", server: "stack.herokuapp.com", upload: "symbol-upload.herokuapp.com", noStackID: true, stack: elsewhere},
		{name: "without a stack ID, a failed lookup sends nothing", server: "stack.herokuapp.com", upload: "symbol-upload.herokuapp.com", noStackID: true, noLookup: true,
			want: `failed to get stack info for "test"`},
		{name: "with a stack ID, a failed lookup sends nothing", server: "stack.herokuapp.com", upload: "symbol-upload.herokuapp.com", noLookup: true,
			want: `failed to get stack info for "test"`},
		{name: "on its own domain, a failed lookup sends nothing", server: "x.grafana.net", upload: "upload.grafana.net", noLookup: true,
			want: `failed to get stack info for "test"`},
		{name: "a sibling two labels down is not a sibling", server: "stack.herokuapp.com", upload: "a.symbol-upload.herokuapp.com", noStackID: true, stack: stack456},
		{name: "another site under github.io is refused", server: "x.github.io", upload: "foo.github.io", noStackID: true, stack: elsewhere},
		{name: "another app under azurewebsites.net is refused", server: "app.azurewebsites.net", upload: "evil.azurewebsites.net", noStackID: true, stack: elsewhere},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir() + "/run.json"
			stack := cmp.Or(tc.stack, atServer)
			if tc.noLookup {
				stack = "none"
			}
			self, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.CommandContext(t.Context(), self, "-test.run=^TestUpsertUploadDomainRunsInChild$", "-test.count=1")
			cmd.Env = append(os.Environ(), "ET_DOMAIN_SERVER="+tc.server, "ET_DOMAIN_UPLOAD="+tc.upload, "ET_DOMAIN_OUT="+out, "ET_DOMAIN_STACK="+stack)
			if tc.noStackID {
				cmd.Env = append(cmd.Env, "ET_DOMAIN_NO_STACK_ID=1")
			}
			log, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", log)
			data, err := os.ReadFile(out)
			require.NoError(t, err, "%s", log)
			var run domainRun
			require.NoError(t, json.Unmarshal(data, &run))
			if tc.allowed {
				assert.NotContains(t, run.Error, "outside the domain")
				assert.Equal(t, []string{tc.upload + ":443"}, run.Connects, "the upload went to its host")
				return
			}
			assert.Equal(t, 1, run.Exit)
			want := cmp.Or(tc.want, "the project's upload address is on "+tc.upload+", outside the domain of the Grafana server "+tc.server)
			assert.Contains(t, run.Error, want)
			assert.Empty(t, run.Connects, "nothing is sent")
			assert.NotContains(t, run.Error, "synthetic-cloud-credential", "the token is not printed")
		})
	}
}
