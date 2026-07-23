package settings_test

// Guards the resources-tier half of the #1048 contract: the lazy adapter
// factory constructs a zero-value ConfigLoader, so an explicitly selected
// config file must reach it through ctx threading (what the generic
// `gcx resources ... --config` path sets up via config.ContextWithConfigFile).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/appo11y/settings"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewLazyFactory_HonorsContextThreadedConfigFile(t *testing.T) {
	testutils.SandboxConfigEnv(t)

	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auths = append(auths, req.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
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

	ctx := internalconfig.ContextWithConfigFile(context.Background(), cfgPath)
	adapterInst, err := settings.NewLazyFactory()(ctx)
	require.NoError(t, err)

	obj, err := adapterInst.Get(ctx, "default", metav1.GetOptions{})
	require.NoError(t, err)

	require.NotEmpty(t, auths, "the adapter must call the config file's server")
	for _, a := range auths {
		assert.Equal(t, "Bearer threaded-token", a, "requests must carry the threaded config's token")
	}
	assert.Equal(t, "stacks-33333", obj.GetNamespace(),
		"the envelope namespace must come from the threaded config's stack")
}
