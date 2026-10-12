package remote //nolint:testpackage // Exercise the retry policy and its injected backoff.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
)

type retryTestClient struct {
	mutate   func(*unstructured.Unstructured, []string) (*unstructured.Unstructured, error)
	deleteFn func(metav1.DeleteOptions) error
	readErr  error
	reads    int
}

func (f *retryTestClient) Create(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return f.mutate(obj, opts.DryRun)
}
func (f *retryTestClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return f.mutate(obj, opts.DryRun)
}
func (f *retryTestClient) Delete(_ context.Context, _ resources.Descriptor, _ string, opts metav1.DeleteOptions) error {
	return f.deleteFn(opts)
}
func (f *retryTestClient) Get(_ context.Context, _ resources.Descriptor, _ string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	f.reads++
	return nil, f.readErr
}
func (f *retryTestClient) GetMultiple(_ context.Context, _ resources.Descriptor, _ []string, _ metav1.GetOptions) ([]unstructured.Unstructured, error) {
	f.reads++
	return nil, f.readErr
}
func (f *retryTestClient) List(_ context.Context, _ resources.Descriptor, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	f.reads++
	return nil, f.readErr
}

func retryTestConflict(message string) error {
	return apierrors.NewConflict(schema.GroupResource{Group: "example.test", Resource: "things"}, "one", errors.New(message))
}

func retryTestNativeDescriptor(kind, version string) resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: version},
		Kind:         kind,
		Plural:       map[string]string{"TimeInterval": "timeintervals", "TemplateGroup": "templategroups"}[kind],
	}
}

func TestConflictRetryMutationScope(t *testing.T) {
	type scopeCase struct {
		name      string
		desc      resources.Descriptor
		wantCalls int
	}
	tests := make([]scopeCase, 0, 11)
	tests = append(tests, []scopeCase{
		{name: "dashboard", desc: guardDashboardDesc(), wantCalls: 1},
		{name: "folder", desc: resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1beta1"}, Kind: "Folder"}, wantCalls: 1},
		{name: "routing tree", desc: resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: "v0alpha1"}, Kind: "RoutingTree"}, wantCalls: 1},
	}...)
	for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
		for _, version := range []string{"v0alpha1", "v1beta1"} {
			desc := retryTestNativeDescriptor(kind, version)
			tests = append(tests, scopeCase{name: kind + "/" + version, desc: desc, wantCalls: 2})
			desc.GroupVersion.Group = "other.grafana.app"
			tests = append(tests, scopeCase{name: "other group/" + kind + "/" + version, desc: desc, wantCalls: 1})
		}
	}
	for _, tc := range tests {
		for _, verb := range []string{"create", "update", "delete"} {
			t.Run(tc.name+"/"+verb, func(t *testing.T) {
				calls := 0
				conflict := retryTestConflict("stale resource version")
				obj := namedObject("one")
				obj.SetResourceVersion("unchanged-version")
				invoke := func() error {
					calls++
					if calls == 1 {
						return conflict
					}
					return nil
				}
				fake := &retryTestClient{
					mutate: func(got *unstructured.Unstructured, _ []string) (*unstructured.Unstructured, error) {
						require.Equal(t, "unchanged-version", got.GetResourceVersion())
						return got, invoke()
					},
					deleteFn: func(metav1.DeleteOptions) error { return invoke() },
				}
				client := newConflictRetryClient(fake)
				client.backoff = wait.Backoff{Steps: 2}
				var err error
				switch verb {
				case "create":
					_, err = client.Create(t.Context(), tc.desc, obj, metav1.CreateOptions{})
				case "update":
					_, err = client.Update(t.Context(), tc.desc, obj, metav1.UpdateOptions{})
				case "delete":
					err = client.Delete(t.Context(), tc.desc, "one", metav1.DeleteOptions{})
				}
				require.Equal(t, tc.wantCalls, calls)
				if tc.wantCalls == 1 {
					require.Same(t, conflict, err, "non-opted kinds preserve the original conflict")
				} else {
					require.NoError(t, err)
				}
				require.Zero(t, fake.reads, "conflict retries must not refetch the resource")
			})
		}
	}
}

func TestConflictRetryMutations(t *testing.T) {
	for _, verb := range []string{"create", "update", "delete"} {
		for _, tc := range []struct {
			name      string
			failures  []error
			wantCalls int
		}{
			{name: "success", wantCalls: 1},
			{name: "conflicts then success", failures: []error{retryTestConflict("first"), retryTestConflict("second")}, wantCalls: 3},
			{name: "already exists", failures: []error{apierrors.NewAlreadyExists(schema.GroupResource{Resource: "things"}, "one")}, wantCalls: 1},
			{name: "forbidden", failures: []error{apierrors.NewForbidden(schema.GroupResource{Resource: "things"}, "one", errors.New("no"))}, wantCalls: 1},
			{name: "transport", failures: []error{errors.New("connection lost")}, wantCalls: 1},
			{name: "exhausted", failures: []error{retryTestConflict("first"), retryTestConflict("second"), retryTestConflict("last")}, wantCalls: 3},
		} {
			t.Run(verb+"/"+tc.name, func(t *testing.T) {
				calls := 0
				result := namedObject("server-result")
				invoke := func() error {
					calls++
					if calls <= len(tc.failures) {
						return tc.failures[calls-1]
					}
					return nil
				}
				fake := &retryTestClient{
					mutate: func(_ *unstructured.Unstructured, _ []string) (*unstructured.Unstructured, error) {
						return result, invoke()
					},
					deleteFn: func(metav1.DeleteOptions) error { return invoke() },
				}
				client := newConflictRetryClient(fake)
				client.backoff = wait.Backoff{Steps: 3}
				var got *unstructured.Unstructured
				var err error
				switch verb {
				case "create":
					got, err = client.Create(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), namedObject("local"), metav1.CreateOptions{})
				case "update":
					got, err = client.Update(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), namedObject("local"), metav1.UpdateOptions{})
				case "delete":
					err = client.Delete(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), "local", metav1.DeleteOptions{})
				}
				require.Equal(t, tc.wantCalls, calls)
				if tc.name == "conflicts then success" || tc.name == "success" {
					require.NoError(t, err)
				} else {
					require.Same(t, tc.failures[tc.wantCalls-1], err, "preserve the exact last error")
				}
				if verb != "delete" {
					require.Same(t, result, got)
				}
			})
		}
	}
}

func TestConflictRetryReadsPassThrough(t *testing.T) {
	fake := &retryTestClient{readErr: retryTestConflict("read")}
	client := newConflictRetryClient(fake)
	_, err := client.Get(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), "one", metav1.GetOptions{})
	require.Same(t, fake.readErr, err)
	_, err = client.GetMultiple(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), []string{"one"}, metav1.GetOptions{})
	require.Same(t, fake.readErr, err)
	_, err = client.List(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), metav1.ListOptions{})
	require.Same(t, fake.readErr, err)
	require.Equal(t, 3, fake.reads)
}

func TestConflictRetryCopiesMutationInputs(t *testing.T) {
	for _, verb := range []string{"create", "update", "delete"} {
		t.Run(verb, func(t *testing.T) {
			calls := 0
			obj := namedObject("original")
			obj.SetResourceVersion("unchanged-version")
			dryRun := []string{metav1.DryRunAll}
			version := "unchanged-version"
			fake := &retryTestClient{
				mutate: func(got *unstructured.Unstructured, gotDryRun []string) (*unstructured.Unstructured, error) {
					calls++
					require.Equal(t, "original", got.GetName())
					require.Equal(t, "unchanged-version", got.GetResourceVersion())
					require.Equal(t, dryRun, gotDryRun)
					got.SetName("mutated")
					got.SetResourceVersion("mutated")
					gotDryRun[0] = "mutated"
					if calls == 1 {
						return nil, retryTestConflict("retry")
					}
					return got, nil
				},
				deleteFn: func(opts metav1.DeleteOptions) error {
					calls++
					require.Equal(t, dryRun, opts.DryRun)
					require.Equal(t, version, *opts.Preconditions.ResourceVersion)
					opts.DryRun[0] = "mutated"
					*opts.Preconditions.ResourceVersion = "mutated"
					if calls == 1 {
						return retryTestConflict("retry")
					}
					return nil
				},
			}
			client := newConflictRetryClient(fake)
			client.backoff = wait.Backoff{Steps: 2}
			var err error
			switch verb {
			case "create":
				_, err = client.Create(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), obj, metav1.CreateOptions{DryRun: dryRun})
			case "update":
				_, err = client.Update(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), obj, metav1.UpdateOptions{DryRun: dryRun})
			case "delete":
				err = client.Delete(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), "original", metav1.DeleteOptions{DryRun: dryRun, Preconditions: &metav1.Preconditions{ResourceVersion: &version}})
			}
			require.NoError(t, err)
			require.Equal(t, 2, calls)
			require.Equal(t, "original", obj.GetName())
			require.Equal(t, "unchanged-version", obj.GetResourceVersion())
			require.Equal(t, []string{metav1.DryRunAll}, dryRun)
			require.Equal(t, "unchanged-version", version)
		})
	}
}

func TestConflictRetryCancellation(t *testing.T) {
	for _, beforeCall := range []bool{true, false} {
		t.Run(map[bool]string{true: "before first attempt", false: "during backoff"}[beforeCall], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			firstAttempt := make(chan struct{})
			fake := &retryTestClient{deleteFn: func(metav1.DeleteOptions) error {
				calls++
				close(firstAttempt)
				return retryTestConflict("cancel")
			}}
			client := newConflictRetryClient(fake)
			client.backoff = wait.Backoff{Steps: 10, Duration: time.Hour}
			if beforeCall {
				cancel()
			} else {
				go func() {
					<-firstAttempt
					time.Sleep(10 * time.Millisecond)
					cancel()
				}()
			}
			start := time.Now()
			err := client.Delete(ctx, retryTestNativeDescriptor("TimeInterval", "v1beta1"), "one", metav1.DeleteOptions{})
			require.ErrorIs(t, err, context.Canceled)
			require.Less(t, time.Since(start), time.Second)
			if beforeCall {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestConflictRetryInsideDryRunGuard(t *testing.T) {
	for _, verb := range []string{"create", "update", "delete"} {
		t.Run(verb, func(t *testing.T) {
			calls := 0
			invoke := func() error {
				calls++
				if calls == 1 {
					return retryTestConflict("retry")
				}
				return nil
			}
			fake := &retryTestClient{
				mutate: func(obj *unstructured.Unstructured, _ []string) (*unstructured.Unstructured, error) {
					return obj, invoke()
				},
				deleteFn: func(metav1.DeleteOptions) error { return invoke() },
			}
			// Call the same wrapper factory as the production pusher and deleter.
			client := newGuardedRetryDynamicClient(fake, GuardConfig{})
			guard, ok := client.(*dryRunGuard)
			require.True(t, ok, "dry-run guard must be the outermost wrapper")
			retrying, ok := guard.inner.(*conflictRetryClient)
			require.True(t, ok, "mutations allowed by the guard must retry conflicts")
			retrying.backoff = wait.Backoff{Steps: 2}
			mutate := func(dryRun []string) error {
				var err error
				switch verb {
				case "create":
					_, err = client.Create(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), namedObject("one"), metav1.CreateOptions{DryRun: dryRun})
				case "update":
					_, err = client.Update(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), namedObject("one"), metav1.UpdateOptions{DryRun: dryRun})
				case "delete":
					err = client.Delete(t.Context(), retryTestNativeDescriptor("TimeInterval", "v1beta1"), "one", metav1.DeleteOptions{DryRun: dryRun})
				}
				return err
			}
			require.ErrorIs(t, mutate(dryRun()), errDryRunUnverified)
			require.Zero(t, calls, "blocked dry-runs must not reach the mutation client")
			require.NoError(t, mutate(nil))
			require.Equal(t, 2, calls, "a permitted mutation retries the conflict")
		})
	}
}
