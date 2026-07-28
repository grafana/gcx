package testutils

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AssertFactoryHonorsThreadedConfigFile proves a lazy adapter factory picks
// up an explicitly selected config file threaded through ctx via
// config.ContextWithConfigFile — the path the generic `gcx resources ...
// --config` command sets up. Lazy factories construct a zero-value
// ConfigLoader, so this is the only channel an explicit --config has into
// them; a factory that loads config any other way silently reads the
// default config instead (the #951/#1048 defect class).
//
// It sandboxes config discovery, stands up a recording server answering
// every request with respBody (plus respHeaders), writes a config file
// pointing at it, then asserts the factory's adapter Get routes to that
// server carrying the config's token and stamps the config's stack
// namespace (stacks-33333) on the returned envelope.
//
// Incompatible with t.Parallel (SandboxConfigEnv uses t.Setenv/t.Chdir).
func AssertFactoryHonorsThreadedConfigFile(t *testing.T, factory adapter.Factory, respBody string, respHeaders map[string]string) {
	t.Helper()
	SandboxConfigEnv(t)

	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		auths = append(auths, req.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		for k, v := range respHeaders {
			w.Header().Set(k, v)
		}
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)

	cfgPath := filepath.Join(t.TempDir(), "threaded.yaml")
	cfg := fmt.Sprintf(`version: 1
stacks:
  main:
    grafana:
      server: %s
      token: threaded-token
      stack-id: 33333
contexts:
  default:
    stack: main
current-context: default
`, srv.URL)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	ctx := config.ContextWithConfigFile(context.Background(), cfgPath)
	adapterInst, err := factory(ctx)
	require.NoError(t, err)

	obj, err := adapterInst.Get(ctx, "default", metav1.GetOptions{})
	require.NoError(t, err)

	mu.Lock()
	got := append([]string(nil), auths...)
	mu.Unlock()
	require.NotEmpty(t, got, "the adapter must call the config file's server")
	for _, a := range got {
		assert.Equal(t, "Bearer threaded-token", a, "requests must carry the threaded config's token")
	}
	assert.Equal(t, "stacks-33333", obj.GetNamespace(),
		"the envelope namespace must come from the threaded config's stack")
}
