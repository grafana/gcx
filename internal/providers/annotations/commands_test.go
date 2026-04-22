package annotations_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/annotations"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// stubLoader satisfies RESTConfigLoader for tests, pointing at the given
// httptest server.
type stubLoader struct{ host string }

func (s *stubLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: s.host}}, nil
}

type recordingServer struct {
	server  *httptest.Server
	request *http.Request
	body    []byte
}

// newRecordingServer returns a server that records the last request and
// responds with a "[]" JSON body.
func newRecordingServer(t *testing.T) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.request = r
		data, _ := io.ReadAll(r.Body)
		rs.body = data
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(rs.server.Close)
	return rs
}

// runListCmd executes the list subcommand with the given args against the
// recording server.
func runListCmd(t *testing.T, rs *recordingServer, args ...string) error {
	t.Helper()
	cmd := annotations.NewListCommandForTest(&stubLoader{host: rs.server.URL})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"-o", "json"}, args...))
	return cmd.ExecuteContext(context.Background())
}

func TestListCommand_LookbackToFromTo(t *testing.T) {
	rs := newRecordingServer(t)

	before := time.Now()
	err := runListCmd(t, rs, "--lookback", "1h")
	after := time.Now()
	require.NoError(t, err)

	require.NotNil(t, rs.request)
	assert.Equal(t, "/api/annotations", rs.request.URL.Path)

	q := rs.request.URL.Query()
	fromStr := q.Get("from")
	toStr := q.Get("to")
	require.NotEmpty(t, fromStr)
	require.NotEmpty(t, toStr)

	fromMs, err := strconv.ParseInt(fromStr, 10, 64)
	require.NoError(t, err)
	toMs, err := strconv.ParseInt(toStr, 10, 64)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, toMs, before.UnixMilli())
	assert.LessOrEqual(t, toMs, after.UnixMilli())

	// to - from should be ~1h.
	const tolMs = int64(5_000)
	assert.InDelta(t, int64(time.Hour/time.Millisecond), toMs-fromMs, float64(tolMs))
}

func TestListCommand_DefaultLookbackIs24h(t *testing.T) {
	rs := newRecordingServer(t)

	err := runListCmd(t, rs)
	require.NoError(t, err)

	q := rs.request.URL.Query()
	fromMs, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	toMs, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	// to - from should be ~24h.
	const tolMs = int64(5_000)
	assert.InDelta(t, int64(24*time.Hour/time.Millisecond), toMs-fromMs, float64(tolMs))
}

func TestListCommand_ExplicitFromToOverridesLookback(t *testing.T) {
	rs := newRecordingServer(t)

	err := runListCmd(t, rs, "--from", "1000", "--to", "2000")
	require.NoError(t, err)

	q := rs.request.URL.Query()
	assert.Equal(t, "1000", q.Get("from"))
	assert.Equal(t, "2000", q.Get("to"))
}

func TestListCommand_LookbackWithFromOrToFails(t *testing.T) {
	rs := newRecordingServer(t)

	err := runListCmd(t, rs, "--lookback", "1h", "--from", "1000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--lookback")
}

func TestListCommand_TagsAndLimit(t *testing.T) {
	rs := newRecordingServer(t)

	err := runListCmd(t, rs, "--tags", "deploy,prod", "--limit", "25")
	require.NoError(t, err)

	q := rs.request.URL.Query()
	assert.ElementsMatch(t, []string{"deploy", "prod"}, q["tags"])
	assert.Equal(t, "25", q.Get("limit"))
}

func TestListTableCodec_Truncation(t *testing.T) {
	codec := &annotations.ListTableCodec{}
	long := strings.Repeat("x", 120)
	a := annotations.Annotation{
		ID:   1,
		Time: 1700000000000,
		Text: long,
		Tags: []string{"a", "b"},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, []adapter.TypedObject[annotations.Annotation]{{Spec: a}})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "ID")
	assert.Contains(t, out, "TIME")
	assert.Contains(t, out, "DASHBOARD")
	assert.Contains(t, out, "TAGS")
	assert.Contains(t, out, "TEXT")
	// The full-length text must be truncated.
	assert.NotContains(t, out, long)
}

func TestProvider_CommandsShape(t *testing.T) {
	p := &annotations.AnnotationsProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)
	root := cmds[0]
	assert.Equal(t, "annotations", root.Use)

	subNames := make([]string, 0, len(root.Commands()))
	for _, c := range root.Commands() {
		subNames = append(subNames, strings.Fields(c.Use)[0])
	}
	assert.ElementsMatch(t, []string{"list", "get", "create", "update", "delete"}, subNames)
}
