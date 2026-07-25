//nolint:testpackage,modernize // internal fake needs access to unexported types; boolp(true) != new(bool)
package apps

import (
	"context"
	"sync"

	"github.com/grafana/gcx/internal/providers/instrumentation"
)

// fakeAppsClient is a test double for appsClient.
// It tracks calls and allows tests to inject controlled responses.
type fakeAppsClient struct {
	mu sync.Mutex

	// getResponses is the queue of GetAppInstrumentation responses to return.
	// Each call pops the first response; if empty, returns the last response.
	getResponses []getResponse

	// getCalls counts how many times GetAppInstrumentation was called.
	getCalls int

	// setErr is the error to return from SetAppInstrumentation.
	setErr error

	// discoverItems is the list returned by RunK8sDiscovery.
	discoverItems []instrumentation.DiscoveryItem

	// discoverErr is the error to return from RunK8sDiscovery / IsNamespaceDiscovered.
	discoverErr error

	// discoverErrs is a per-call error queue for RunK8sDiscovery: each call
	// pops the first entry (nil = success). When exhausted, discoverErr applies.
	discoverErrs []error

	// setCalls records the arguments passed to SetAppInstrumentation.
	setCalls []setCall

	// pipelines is the list returned by ListPipelines.
	pipelines []instrumentation.Pipeline

	// listPipelinesErr is the error to return from ListPipelines.
	listPipelinesErr error
}

type getResponse struct {
	namespaces []instrumentation.App
	err        error
}

type setCall struct {
	clusterName string
	namespaces  []instrumentation.App
}

func (f *fakeAppsClient) GetAppInstrumentation(_ context.Context, clusterName string) (*instrumentation.GetAppInstrumentationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.getCalls++
	_ = clusterName
	if len(f.getResponses) == 0 {
		return &instrumentation.GetAppInstrumentationResponse{Namespaces: nil}, nil
	}
	resp := f.getResponses[0]
	if len(f.getResponses) > 1 {
		f.getResponses = f.getResponses[1:]
	}
	if resp.err != nil {
		return nil, resp.err
	}
	return &instrumentation.GetAppInstrumentationResponse{Namespaces: resp.namespaces}, nil
}

func (f *fakeAppsClient) SetAppInstrumentation(_ context.Context, clusterName string, namespaces []instrumentation.App, _ instrumentation.BackendURLs) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.setCalls = append(f.setCalls, setCall{clusterName: clusterName, namespaces: namespaces})
	return f.setErr
}

func (f *fakeAppsClient) RunK8sDiscovery(_ context.Context, _ instrumentation.PromHeaders) (*instrumentation.RunK8sDiscoveryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.discoverErrs) > 0 {
		err := f.discoverErrs[0]
		f.discoverErrs = f.discoverErrs[1:]
		if err != nil {
			return nil, err
		}
	} else if f.discoverErr != nil {
		return nil, f.discoverErr
	}
	return &instrumentation.RunK8sDiscoveryResponse{Items: f.discoverItems}, nil
}

func (f *fakeAppsClient) IsNamespaceDiscovered(_ context.Context, _ instrumentation.PromHeaders, cluster, namespace string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.discoverErr != nil {
		return false, f.discoverErr
	}
	for _, item := range f.discoverItems {
		if item.ClusterName == cluster && item.Namespace == namespace {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeAppsClient) ListPipelines(_ context.Context) ([]instrumentation.Pipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.pipelines, f.listPipelinesErr
}

// helpers

func boolp(b bool) *bool { return &b }

// buildNamespaces creates an App slice from the given namespace names with
// all signal flags set to the given value.
//
//nolint:unparam // allOn is always true in current tests; keep the param for future false cases.
func buildNamespaces(allOn bool, names ...string) []instrumentation.App {
	apps := make([]instrumentation.App, 0, len(names))
	for _, name := range names {
		apps = append(apps, instrumentation.App{
			Name:            name,
			Autoinstrument:  boolp(allOn),
			Tracing:         boolp(allOn),
			Logging:         boolp(allOn),
			ProcessMetrics:  boolp(allOn),
			ExtendedMetrics: boolp(allOn),
			Profiling:       boolp(allOn),
		})
	}
	return apps
}
