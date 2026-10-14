//nolint:testpackage // Acceptance checks exercise the private wrapper and invocation-scoped plans.
package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/assistant/watchers/watcherstest"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type adapterReadRecorder struct {
	mu    sync.Mutex
	paths []string
}

func (r *adapterReadRecorder) Serve(fixture *watcherstest.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			r.mu.Lock()
			r.paths = append(r.paths, req.URL.Path)
			r.mu.Unlock()
		}
		fixture.ServeHTTP(w, req)
	}
}
func (r *adapterReadRecorder) Reads(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, p := range r.paths {
		if p == path {
			count++
		}
	}
	return count
}
func (r *adapterReadRecorder) Total() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.paths) }

type adapterProbeGuard struct {
	adapter.ResourceAdapter

	probes atomic.Int32
}

func (a *adapterProbeGuard) Get(context.Context, string, metav1.GetOptions) (*unstructured.Unstructured, error) {
	a.probes.Add(1)
	return nil, errors.New("generic existence probe is forbidden")
}
func (a *adapterProbeGuard) List(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	a.probes.Add(1)
	return nil, errors.New("generic natural-key/configuration list is forbidden")
}

type adapterAcceptanceRegistry struct{}

func (adapterAcceptanceRegistry) SupportedResources() resources.Descriptors {
	return resources.Descriptors{WatcherDescriptor()}
}
func (adapterAcceptanceRegistry) PreferredResources() resources.Descriptors {
	return resources.Descriptors{WatcherDescriptor()}
}
func acceptanceAdapter(t *testing.T, fixture *watcherstest.Server) (*manifestAdapter, *adapterReadRecorder) {
	t.Helper()
	recorder := &adapterReadRecorder{}
	client := clientForServer(t, recorder.Serve(fixture))
	crud := NewTypedCRUDForClient(client, "example")
	return &manifestAdapter{ResourceAdapter: crud.AsAdapter(), client: client}, recorder
}
func acceptanceRouter(a *manifestAdapter) *adapter.ResourceClientRouter {
	return adapter.NewResourceClientRouter(nil, map[schema.GroupVersionKind]adapter.Factory{WatcherDescriptor().GroupVersionKind(): func(context.Context) (adapter.ResourceAdapter, error) { return a, nil }})
}
func acceptanceManifest(t *testing.T, title, id string) *resources.Resource {
	t.Helper()
	w := minimalWatcher(t)
	w.Title = title
	data, err := json.Marshal(w)
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, json.Unmarshal(data, &spec))
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": WatcherAPIVersion, "kind": WatcherKind, "metadata": map[string]any{"name": adapter.SlugifyName(title)}, "spec": spec}}
	if id != "" {
		obj.SetAnnotations(map[string]string{WatcherIDAnnotation: id})
	}
	return resources.MustFromUnstructured(obj)
}
func acceptancePush(t *testing.T, a *manifestAdapter, input *resources.Resources, dryRun bool) *remote.OperationSummary {
	t.Helper()
	summary, err := remote.NewPusher(acceptanceRouter(a), adapterAcceptanceRegistry{}).Push(t.Context(), remote.PushRequest{Resources: input, DryRun: dryRun, MaxConcurrency: 3})
	require.NoError(t, err)
	return summary
}

func TestAdapterPushPreflightScopesMixedSelectionsToWatchers(t *testing.T) {
	fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{fixtureWatcher("id-1", "Original Title")}}}
	a, _ := acceptanceAdapter(t, fixture)
	watcher := acceptanceManifest(t, "Health Check", "id-1")
	dashboardDescriptor := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"},
		Kind:         "Dashboard", Singular: "dashboard", Plural: "dashboards",
	}
	dashboard := resources.MustFromUnstructured(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": dashboardDescriptor.GroupVersion.String(), "kind": dashboardDescriptor.Kind,
		"metadata": map[string]any{"name": "health-check"},
		"spec":     map[string]any{"title": "Health Check", "panels": []any{}},
	}})
	originalDashboard := dashboard.Object.DeepCopy()
	check, err := a.NewPushPreflight(t.Context(), []adapter.PushSelection{
		{Descriptor: WatcherDescriptor(), Items: []*resources.Resource{watcher}},
		{Descriptor: dashboardDescriptor, Items: []*resources.Resource{dashboard}},
	})
	require.NoError(t, err)
	require.NoError(t, check(t.Context(), dashboard))
	require.Equal(t, originalDashboard.Object, dashboard.Object.Object)
	require.NoError(t, check(t.Context(), watcher))
	require.NotEmpty(t, watcher.Annotations()[pushPlanAnnotation])
	obj := watcher.ToUnstructured()
	require.NoError(t, a.PushEnvelope(t.Context(), &obj, metav1.CreateOptions{}))
	require.Equal(t, 1, fixture.CollectionReads(false))
	require.Equal(t, 1, fixture.CollectionReads(true))
	requests := fixture.MutationRequests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPut, requests[0].Method)
	require.Equal(t, testCollectionPath+"/id-1", requests[0].Path)
}

func TestAdapterOwnedBatchRetainsSameNamesAndEnumeratesOnce(t *testing.T) {
	fixture := &watcherstest.Server{AllowWrites: true,
		Current:          [][]watchers.Watcher{{fixtureWatcher("id-current", "Health Check")}, {fixtureWatcher("id-unrelated", "Unrelated")}},
		Archived:         [][]watchers.Watcher{{fixtureWatcher("id-archived", "Health / Check")}, {fixtureWatcher("id-other-archived", "Other Archived")}},
		DetailStatus:     map[string]int{"id-unrelated": http.StatusNotImplemented, "id-other-archived": http.StatusNotImplemented},
		EnrollmentStatus: map[string]int{"id-unrelated": http.StatusNotImplemented, "id-other-archived": http.StatusNotImplemented},
	}
	a, reads := acceptanceAdapter(t, fixture)
	guard := &adapterProbeGuard{ResourceAdapter: a.ResourceAdapter}
	a.ResourceAdapter = guard
	input := resources.NewResources()
	input.AddDistinct(acceptanceManifest(t, "Health Check", ""))
	input.AddDistinct(acceptanceManifest(t, "Health / Check", ""))
	require.Equal(t, 2, input.Len())
	summary := acceptancePush(t, a, input, false)
	require.Equal(t, 2, summary.SuccessCount())
	require.Zero(t, summary.FailedCount())
	require.Zero(t, guard.probes.Load())
	require.Equal(t, 2, fixture.CollectionReads(false))
	require.Equal(t, 2, fixture.CollectionReads(true))
	require.Zero(t, reads.Reads(testCollectionPath+"/id-unrelated"))
	require.Zero(t, fixture.EnrollmentReads("id-unrelated"))
	require.Zero(t, reads.Reads(testCollectionPath+"/id-other-archived"))
	requests := fixture.MutationRequests()
	require.Len(t, requests, 2)
	paths := []string{requests[0].Path, requests[1].Path}
	require.ElementsMatch(t, []string{testCollectionPath + "/id-current", testCollectionPath + "/id-archived"}, paths)
	// The same authored envelopes reapply without further configuration writes.
	require.Equal(t, 2, acceptancePush(t, a, input, false).SuccessCount())
	require.Equal(t, 2, fixture.MutationCalls())
	require.Equal(t, 4, fixture.CollectionReads(false))
	require.Equal(t, 4, fixture.CollectionReads(true))
}

func TestAdapterBatchRefusesEveryDuplicateOffender(t *testing.T) {
	for _, tt := range []struct {
		name        string
		existing    []watchers.Watcher
		titles, ids []string
	}{
		{"same selected ID", []watchers.Watcher{fixtureWatcher("id-1", "Original")}, []string{"First Rename", "Second Rename"}, []string{"id-1", "id-1"}},
		{"same existing title", []watchers.Watcher{fixtureWatcher("id-1", "Existing")}, []string{"Existing", "Existing"}, []string{"", "stale"}},
		{"same new title", nil, []string{"New Title", "New Title"}, []string{"", ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{append([]watchers.Watcher{}, tt.existing...)}}
			a, reads := acceptanceAdapter(t, fixture)
			input := resources.NewResources()
			for i, title := range tt.titles {
				input.AddDistinct(acceptanceManifest(t, title, tt.ids[i]))
			}
			summary := acceptancePush(t, a, input, false)
			require.Equal(t, 2, summary.FailedCount())
			require.Zero(t, summary.SuccessCount())
			require.Zero(t, fixture.MutationCalls())
			for _, failure := range summary.Failures() {
				require.ErrorContains(t, failure.Error, "multiple Watcher manifests select the same target")
			}
			require.Equal(t, 1, fixture.CollectionReads(false))
			require.Equal(t, 1, fixture.CollectionReads(true))
			require.Equal(t, 2, reads.Total())
		})
	}
}

func TestAdapterIDTitleAndArchivedUpsertSelection(t *testing.T) {
	for _, tt := range []struct {
		name, title, id, target string
		archived                bool
		wantError               string
	}{
		{name: "matching ID renames", title: "Renamed", id: "id-1", target: "id-1"},
		{name: "stale ID exact title fallback", title: "Existing", id: "stale-id", target: "id-1"},
		{name: "archived exact title updates", title: "Existing", target: "id-1", archived: true},
		{name: "ID rename occupied current", title: "Occupied", id: "id-1", wantError: "occupied title"},
		{name: "ID rename occupied archived", title: "Archived Occupied", id: "id-1", wantError: "occupied title"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := fixtureWatcher("id-1", "Existing")
			fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{fixtureWatcher("id-2", "Occupied")}}, Archived: [][]watchers.Watcher{{fixtureWatcher("id-3", "Archived Occupied")}}}
			if tt.archived {
				fixture.Archived[0] = append(fixture.Archived[0], original)
			} else {
				fixture.Current[0] = append(fixture.Current[0], original)
			}
			a, _ := acceptanceAdapter(t, fixture)
			summary := acceptancePush(t, a, resources.NewResources(acceptanceManifest(t, tt.title, tt.id)), false)
			if tt.wantError != "" {
				require.Equal(t, 1, summary.FailedCount())
				require.ErrorContains(t, summary.Failures()[0].Error, tt.wantError)
				require.Zero(t, fixture.MutationCalls())
				return
			}
			require.Equal(t, 1, summary.SuccessCount())
			require.Zero(t, summary.FailedCount())
			requests := fixture.MutationRequests()
			require.Len(t, requests, 1)
			require.Equal(t, http.MethodPut, requests[0].Method)
			require.Equal(t, testCollectionPath+"/"+tt.target, requests[0].Path)
			require.Equal(t, 1, fixture.CollectionReads(false))
			require.Equal(t, 1, fixture.CollectionReads(true))
		})
	}
}

func TestAdapterIdentityDeletionIgnoresUnavailableConfiguration(t *testing.T) {
	for _, file := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			t.Run(map[bool]string{false: "selector", true: "file"}[file]+map[bool]string{false: " apply", true: " dry-run"}[dryRun], func(t *testing.T) {
				fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{fixtureWatcher("id-1", "Health Check")}}, Archived: [][]watchers.Watcher{{fixtureWatcher("id-2", "Health / Check")}}, DetailStatus: map[string]int{"id-1": http.StatusNotImplemented, "id-2": http.StatusNotImplemented}, EnrollmentStatus: map[string]int{"id-1": http.StatusNotImplemented, "id-2": http.StatusNotImplemented}}
				a, reads := acceptanceAdapter(t, fixture)
				router := acceptanceRouter(a)
				input := resources.NewResources()
				if file {
					input.AddDistinct(acceptanceManifest(t, "Health Check", "id-1"))
					input.AddDistinct(acceptanceManifest(t, "Health / Check", "id-2"))
				} else {
					summary, err := remote.NewPuller(router, adapterAcceptanceRegistry{}).Pull(t.Context(), remote.PullRequest{Filters: resources.Filters{{Descriptor: WatcherDescriptor(), Type: resources.FilterTypeMultiple, ResourceUIDs: []string{"id-1", "id-2"}}}, IdentityOnlyDelete: true, Resources: input, StopOnError: true})
					require.NoError(t, err)
					require.Equal(t, 2, summary.SuccessCount())
					require.Equal(t, 2, input.Len())
				}
				summary, err := remote.NewDeleterWithClient(router, adapterAcceptanceRegistry{}).Delete(t.Context(), remote.DeleteRequest{Resources: input, ResolveLocalIdentities: file, DryRun: dryRun})
				require.NoError(t, err)
				require.Zero(t, summary.FailedCount())
				if dryRun {
					require.Equal(t, 2, summary.SkippedCount())
					require.Zero(t, fixture.MutationCalls())
				} else {
					require.Equal(t, 2, summary.SuccessCount())
					require.Equal(t, 2, fixture.MutationCalls())
				}
				require.Zero(t, reads.Reads(testCollectionPath+"/id-1"))
				require.Zero(t, reads.Reads(testCollectionPath+"/id-2"))
				require.Zero(t, fixture.EnrollmentReads("id-1"))
				require.Zero(t, fixture.EnrollmentReads("id-2"))
			})
		}
	}
}

func TestAdapterDeleteRefusesAmbiguousNamesAndStrictStaleIDs(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ref, id string
		file    bool
		want    string
	}{
		{name: "ambiguous selector", ref: "health-check", want: "ambiguous"},
		{name: "ambiguous file name", ref: "health-check", file: true, want: "ambiguous"},
		{name: "stale arbitrary ID cannot become name", ref: "health-check", id: "unique-existing", file: true, want: "not found"},
		{name: "stale UUID ID", ref: "health-check", id: "00000000-0000-4000-8000-000000000099", file: true, want: "not found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{fixtureWatcher("id-1", "Health Check"), fixtureWatcher("id-3", "Unique Existing")}}, Archived: [][]watchers.Watcher{{fixtureWatcher("id-2", "Health / Check")}}}
			a, reads := acceptanceAdapter(t, fixture)
			filter := resources.Filter{Descriptor: WatcherDescriptor(), Type: resources.FilterTypeSingle, ResourceUIDs: []string{tt.ref}}
			var manifests []unstructured.Unstructured
			if tt.file {
				obj := acceptanceManifest(t, "Health Check", tt.id).Object
				obj.SetName(tt.ref)
				manifests = []unstructured.Unstructured{obj}
			}
			items, err := a.SelectDelete(t.Context(), filter, manifests)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
			require.Empty(t, items)
			require.Zero(t, fixture.MutationCalls())
			require.Equal(t, 2, reads.Total())
		})
	}
}

func TestAdapterDryRunLocalValidationHasZeroIO(t *testing.T) {
	for _, tt := range []struct {
		name        string
		edit        func(*resources.Resource)
		wantError   string
		wantWarning bool
	}{
		{name: "valid absent sources and unfamiliar value", edit: func(res *resources.Resource) {
			spec := testObject(t, res.Object.Object["spec"])
			spec["sensitivity"] = "future-value"
			spec["notifications"] = map[string]any{"webhook": map[string]any{"enabled": true, "url": map[string]any{"fromFile": filepath.Join(t.TempDir(), "missing-secret")}, "bearerToken": map[string]any{"fromEnv": "WATCHER_ADAPTER_TEST_UNSET_SECRET"}}}
		}, wantWarning: true},
		{name: "unknown field", edit: func(res *resources.Resource) { testObject(t, res.Object.Object["spec"])["unknown"] = true }, wantError: "unknown field"},
		{name: "invalid required prompt", edit: func(res *resources.Resource) { testObject(t, res.Object.Object["spec"])["prompt"] = "" }, wantError: "requires a nonblank"},
		{name: "invalid secret shape", edit: func(res *resources.Resource) {
			testObject(t, res.Object.Object["spec"])["notifications"] = map[string]any{"webhook": map[string]any{"url": map[string]any{"fromEnv": "MISSING", "clear": true}}}
		}, wantError: "exactly one"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &watcherstest.Server{CurrentStatus: http.StatusForbidden, ArchivedStatus: http.StatusForbidden}
			a, reads := acceptanceAdapter(t, fixture)
			res := acceptanceManifest(t, "Example", "stale")
			tt.edit(res)
			summary := acceptancePush(t, a, resources.NewResources(res), true)
			if tt.wantError != "" {
				require.Equal(t, 1, summary.FailedCount())
				require.ErrorContains(t, summary.Failures()[0].Error, tt.wantError)
			} else {
				require.Equal(t, 1, summary.SkippedCount())
				require.Zero(t, summary.FailedCount())
			}
			if tt.wantWarning {
				require.Len(t, summary.Warnings(), 1)
				require.Contains(t, summary.Warnings()[0], "spec.sensitivity")
				require.Contains(t, summary.Warnings()[0], "future-value")
				require.Contains(t, summary.Warnings()[0], "known values:")
			}
			require.Zero(t, reads.Total())
			require.Zero(t, fixture.MutationCalls())
		})
	}
}

func TestAdapterPullHooksAndPartialCoverageRemainAvailable(t *testing.T) {
	fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{fixtureWatcher("id-good", "Good"), fixtureWatcher("id-bad-detail", "Bad Detail"), fixtureWatcher("id-bad-enrollment", "Bad Enrollment")}}, DetailStatus: map[string]int{"id-bad-detail": http.StatusForbidden}, EnrollmentStatus: map[string]int{"id-bad-enrollment": http.StatusNotImplemented}}
	a, _ := acceptanceAdapter(t, fixture)
	var resourceAdapter adapter.ResourceAdapter = a
	require.Implements(t, (*adapter.MultipleGetter)(nil), resourceAdapter)
	require.Implements(t, (*adapter.PullPreflighter)(nil), resourceAdapter)
	destination := resources.NewResources()
	summary, err := remote.NewPuller(acceptanceRouter(a), adapterAcceptanceRegistry{}).Pull(t.Context(), remote.PullRequest{Filters: resources.Filters{{Descriptor: WatcherDescriptor(), Type: resources.FilterTypeAll}}, Resources: destination})
	require.NoError(t, err)
	require.Equal(t, 1, summary.SuccessCount())
	require.Equal(t, 2, summary.FailedCount())
	require.Equal(t, 1, destination.Len())
	require.Equal(t, "id-good", destination.AsList()[0].Annotations()[WatcherIDAnnotation])
	for _, failure := range summary.Failures() {
		require.NotNil(t, failure.Resource)
		require.NotEmpty(t, failure.Resource.Annotations()[WatcherIDAnnotation])
		require.True(t, strings.Contains(failure.Error.Error(), "read") || strings.Contains(failure.Error.Error(), "enrollment"))
	}
}

func TestAdapterUpsertTitleCollisionIncludesEveryVisibleCandidate(t *testing.T) {
	fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{fixtureWatcher("id-first", "Same Title")}, {fixtureWatcher("id-later", "Same Title")}}, Archived: [][]watchers.Watcher{{fixtureWatcher("id-archived", "Same Title")}}}
	a, reads := acceptanceAdapter(t, fixture)
	summary := acceptancePush(t, a, resources.NewResources(acceptanceManifest(t, "Same Title", "stale-id")), false)
	require.Equal(t, 1, summary.FailedCount())
	require.Zero(t, summary.SuccessCount())
	require.Zero(t, fixture.MutationCalls())
	for _, id := range []string{"id-first", "id-later", "id-archived"} {
		require.Contains(t, summary.Failures()[0].Error.Error(), id)
	}
	require.Equal(t, 2, fixture.CollectionReads(false))
	require.Equal(t, 1, fixture.CollectionReads(true))
	require.Equal(t, 3, reads.Total())
}

func TestAdapterPullCrossPartitionCollisionAndExplicitIDException(t *testing.T) {
	for _, ref := range []string{"", "id-current", "id-archived"} {
		t.Run(map[string]string{"": "collection collision", "id-current": "explicit current ID", "id-archived": "explicit archived ID"}[ref], func(t *testing.T) {
			fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{fixtureWatcher("id-current", "Health Check")}}, Archived: [][]watchers.Watcher{{fixtureWatcher("id-archived", "Health / Check")}}}
			a, _ := acceptanceAdapter(t, fixture)
			filter := resources.Filter{Descriptor: WatcherDescriptor(), Type: resources.FilterTypeAll}
			if ref != "" {
				filter.Type = resources.FilterTypeSingle
				filter.ResourceUIDs = []string{ref}
			}
			destination := resources.NewResources()
			summary, err := remote.NewPuller(acceptanceRouter(a), adapterAcceptanceRegistry{}).Pull(t.Context(), remote.PullRequest{Filters: resources.Filters{filter}, Resources: destination})
			require.NoError(t, err)
			if ref == "" {
				require.Equal(t, 1, summary.FailedCount())
				require.Zero(t, destination.Len())
				require.ErrorContains(t, summary.Failures()[0].Error, "ambiguous")
				require.Contains(t, summary.Failures()[0].Error.Error(), "id-archived")
				return
			}
			require.Zero(t, summary.FailedCount())
			require.Equal(t, 1, summary.SuccessCount())
			require.Equal(t, ref, destination.AsList()[0].Annotations()[WatcherIDAnnotation])
		})
	}
}

func TestAdapterMultipleGetRetainsSuccessfulReferences(t *testing.T) {
	fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{fixtureWatcher("id-good", "Good"), fixtureWatcher("id-denied", "Denied")}}, DetailStatus: map[string]int{"id-denied": http.StatusForbidden}}
	a, _ := acceptanceAdapter(t, fixture)
	destination := resources.NewResources()
	summary, err := remote.NewPuller(acceptanceRouter(a), adapterAcceptanceRegistry{}).Pull(t.Context(), remote.PullRequest{Filters: resources.Filters{{Descriptor: WatcherDescriptor(), Type: resources.FilterTypeMultiple, ResourceUIDs: []string{"id-good", "id-denied"}}}, Resources: destination})
	require.NoError(t, err)
	require.Equal(t, 1, summary.SuccessCount())
	require.Equal(t, 1, summary.FailedCount())
	require.Equal(t, 1, destination.Len())
	require.Equal(t, "id-good", destination.AsList()[0].Annotations()[WatcherIDAnnotation])
	require.Contains(t, summary.Failures()[0].Error.Error(), "id-denied")
}

func TestAdapterExplicitIDsRetainExistingDuplicateTitles(t *testing.T) {
	fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{{ID: "one", Name: "Existing duplicate", Prompt: "Watch errors", TriggerIntervalSeconds: 600, Sensitivity: "balanced"}, {ID: "two", Name: "Existing duplicate", Prompt: "Watch errors", TriggerIntervalSeconds: 600, Sensitivity: "balanced"}}}}
	a, _ := acceptanceAdapter(t, fixture)
	input := resources.NewResources()
	input.AddDistinct(acceptanceManifest(t, "Existing duplicate", "one"))
	input.AddDistinct(acceptanceManifest(t, "Existing duplicate", "two"))
	summary := acceptancePush(t, a, input, false)
	require.Zero(t, summary.FailedCount())
	require.Equal(t, 2, summary.SuccessCount())
	require.Zero(t, fixture.MutationCalls())
}
