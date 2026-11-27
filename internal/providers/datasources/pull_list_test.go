package datasources_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	dsclient "github.com/grafana/gcx/internal/datasources"
	provds "github.com/grafana/gcx/internal/providers/datasources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// summaryTransport mimics the legacy API: List returns summaries without
// basic-auth or secret-name fields, GetByUID returns the full object.
type summaryTransport struct {
	dsclient.Transport

	full    map[string]*dsclient.Datasource
	getErrs map[string]error
}

func (s *summaryTransport) List(context.Context) ([]*dsclient.Datasource, error) {
	var out []*dsclient.Datasource
	for _, uid := range []string{"a", "b", "c"} {
		if _, ok := s.full[uid]; !ok {
			continue
		}
		out = append(out, &dsclient.Datasource{UID: uid, Name: s.full[uid].Name, Type: s.full[uid].Type})
	}
	return out, nil
}

func (s *summaryTransport) GetByUID(_ context.Context, uid string) (*dsclient.Datasource, error) {
	if err := s.getErrs[uid]; err != nil {
		return nil, err
	}
	return s.full[uid], nil
}

func newFull(uid string) *dsclient.Datasource {
	return &dsclient.Datasource{
		UID: uid, Name: uid, Type: "prometheus", BasicAuth: true, BasicAuthUser: "bob",
		WithCredentials: true, SecureJSONFields: map[string]bool{"basicAuthPassword": true},
	}
}

func TestListForPullReadsEachDatasourceInFull(t *testing.T) {
	tr := &summaryTransport{full: map[string]*dsclient.Datasource{"a": newFull("a"), "b": newFull("b")}}
	a := provds.NewAdapterForTest(tr, "default")

	// Plain List stays a cheap summary.
	summary, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, summary.Items, 2)
	spec, _ := summary.Items[0].Object["spec"].(map[string]any)
	assert.NotContains(t, spec, "basicAuthUser")

	lister, ok := a.(adapter.PullLister)
	require.True(t, ok)
	full, err := lister.ListForPull(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, full.Items, 2)
	for _, item := range full.Items {
		spec, _ := item.Object["spec"].(map[string]any)
		assert.Equal(t, "bob", spec["basicAuthUser"])
		assert.Equal(t, true, spec["withCredentials"])
		secure, _ := item.Object["secure"].(map[string]any)
		assert.Contains(t, secure, "basicAuthPassword")
	}

	// Pull output equals a single Get for the same datasource.
	got, err := a.Get(t.Context(), "a", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, got.Object, full.Items[0].Object)
}

func TestListForPullKeepsGoingAfterIndividualFailures(t *testing.T) {
	boom := errors.New("upstream exploded")
	tr := &summaryTransport{
		full: map[string]*dsclient.Datasource{"a": newFull("a"), "b": newFull("b"), "c": newFull("c")},
		getErrs: map[string]error{
			"b": boom,
			"c": dsclient.NewAPIError("get datasource", "c", http.StatusNotFound, nil),
		},
	}
	lister, ok := provds.NewAdapterForTest(tr, "default").(adapter.PullLister)
	require.True(t, ok)

	list, err := lister.ListForPull(t.Context(), metav1.ListOptions{})
	require.Error(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "a", list.Items[0].GetName())

	partial, ok := errors.AsType[adapter.PartialReadError](err)
	require.True(t, ok)
	require.Len(t, partial.ReadFailures(), 1)
	require.ErrorIs(t, partial.ReadFailures()[0].Err, boom)
	assert.Contains(t, partial.ReadFailures()[0].Err.Error(), `"b"`)
	assert.Equal(t, 1, partial.SkippedReads(), "a datasource deleted since the list is skipped")
}

func TestListForPullSkippedOnlyErrorOmitsFailureCount(t *testing.T) {
	tr := &summaryTransport{
		full: map[string]*dsclient.Datasource{"a": newFull("a"), "c": newFull("c")},
		getErrs: map[string]error{
			"c": dsclient.NewAPIError("get datasource", "c", http.StatusNotFound, nil),
		},
	}
	lister, ok := provds.NewAdapterForTest(tr, "default").(adapter.PullLister)
	require.True(t, ok)

	_, err := lister.ListForPull(t.Context(), metav1.ListOptions{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "0 datasource(s)")
	assert.Contains(t, err.Error(), "1 datasource(s) deleted during the pull were skipped")
}
