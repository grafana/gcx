package remote_test

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/process"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/grafana/grafana/pkg/apimachinery/utils"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var errManagerIdentityChanged = errors.New("manager identity changed")

// identityCheckingPushClient models the server rule for an existing manager.
// It also records the request so a successful push must send the right identity.
type identityCheckingPushClient struct {
	live      *unstructured.Unstructured
	gotCreate *unstructured.Unstructured
	gotUpdate *unstructured.Unstructured
	getCalls  int
	listCalls int
}

func (c *identityCheckingPushClient) Create(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	c.gotCreate = obj.DeepCopy()
	return obj, nil
}

func (c *identityCheckingPushClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	c.gotUpdate = obj.DeepCopy()
	if c.live == nil {
		return nil, errors.New("update without a live resource")
	}
	liveIdentity := c.live.GetAnnotations()[utils.AnnoKeyManagerIdentity]
	outgoingIdentity := obj.GetAnnotations()[utils.AnnoKeyManagerIdentity]
	if liveIdentity != "" && outgoingIdentity != "" && liveIdentity != outgoingIdentity {
		return nil, errManagerIdentityChanged
	}
	return obj, nil
}

func (c *identityCheckingPushClient) Get(_ context.Context, desc resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	c.getCalls++
	if c.live != nil && c.live.GetName() == name {
		return c.live.DeepCopy(), nil
	}
	return nil, apierrors.NewNotFound(desc.GroupVersionResource().GroupResource(), name)
}

func (c *identityCheckingPushClient) List(_ context.Context, _ resources.Descriptor, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	c.listCalls++
	if c.live == nil {
		return &unstructured.UnstructuredList{}, nil
	}
	return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*c.live.DeepCopy()}}, nil
}

func identityTestObject(group, name string, annotations map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": group + "/v1",
		"kind":       "TestResource",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]any{"name": "same-natural-key"},
	}}
	if annotations != nil {
		obj.SetAnnotations(annotations)
	}
	return obj
}

func identityAnnotations(kind utils.ManagerKind, identity string) map[string]string {
	return map[string]string{
		utils.AnnoKeyManagerKind:     string(kind),
		utils.AnnoKeyManagerIdentity: identity,
	}
}

func pushIdentityTest(t *testing.T, group string, local, live *unstructured.Unstructured, appendManager bool) (*identityCheckingPushClient, *remote.OperationSummary, error) {
	t.Helper()
	desc := makeTestDescriptor(group)
	client := &identityCheckingPushClient{live: live}
	var processors []remote.Processor
	if appendManager {
		processors = append(processors, &process.ManagerFieldsAppender{})
	}
	summary, err := remote.NewPusher(client, &mockPushRegistry{supportedResources: resources.Descriptors{desc}}).Push(t.Context(), remote.PushRequest{
		Resources:      resources.NewResources(resources.MustFromUnstructured(local)),
		Processors:     processors,
		MaxConcurrency: 1,
		StopOnError:    true,
		IncludeManaged: true,
	})
	return client, summary, err
}

func TestPusherPreservesLiveKubectlManagerIdentity(t *testing.T) {
	for _, match := range []string{"direct", "natural-key"} {
		for _, liveIdentity := range []string{"grafanactl", "gcx", "gcx/1.2.3"} {
			t.Run(match+"/"+liveIdentity, func(t *testing.T) {
				group := "manager-" + match + ".test.grafana.app"
				if match == "natural-key" {
					adapter.RegisterNaturalKey(schema.GroupVersionKind{Group: group, Version: "v1", Kind: "TestResource"}, adapter.SpecFieldKey("name"))
				}
				localName := "remote-id"
				if match == "natural-key" {
					localName = "source-id"
				}
				local := identityTestObject(group, localName, nil)
				live := identityTestObject(group, "remote-id", identityAnnotations(resources.ResourceManagerKind, liveIdentity))
				live.SetResourceVersion("42")

				client, summary, err := pushIdentityTest(t, group, local, live, true)
				require.NoError(t, err)
				require.Equal(t, 1, summary.SuccessCount())
				require.Nil(t, client.gotCreate)
				require.NotNil(t, client.gotUpdate)
				require.Equal(t, "remote-id", client.gotUpdate.GetName())
				require.Equal(t, "42", client.gotUpdate.GetResourceVersion())
				require.Equal(t, liveIdentity, client.gotUpdate.GetAnnotations()[utils.AnnoKeyManagerIdentity])
				require.Equal(t, string(resources.ResourceManagerKind), client.gotUpdate.GetAnnotations()[utils.AnnoKeyManagerKind])
				require.Equal(t, "true", client.gotUpdate.GetAnnotations()[utils.AnnoKeyManagerAllowsEdits])
				require.Equal(t, 1, client.getCalls)
				if match == "natural-key" {
					require.Equal(t, 1, client.listCalls)
				} else {
					require.Zero(t, client.listCalls)
				}
			})
		}
	}
}

func TestPusherManagerIdentityBoundaries(t *testing.T) {
	const group = "manager-boundaries.test.grafana.app"
	tests := []struct {
		name          string
		localManager  map[string]string
		liveManager   map[string]string
		appendManager bool
		wantIdentity  string
		wantError     error
	}{
		{
			name:          "new resource gets stable gcx identity",
			appendManager: true,
			wantIdentity:  "gcx",
		},
		{
			name:          "unmanaged live resource gets gcx identity",
			liveManager:   map[string]string{},
			appendManager: true,
			wantIdentity:  "gcx",
		},
		{
			name:          "same live identity remains unchanged",
			localManager:  identityAnnotations(resources.ResourceManagerKind, "gcx"),
			liveManager:   identityAnnotations(resources.ResourceManagerKind, "gcx"),
			appendManager: true,
			wantIdentity:  "gcx",
		},
		{
			name:          "foreign live manager is not copied",
			liveManager:   identityAnnotations(utils.ManagerKindTerraform, "terraform-owner"),
			appendManager: true,
			wantIdentity:  "gcx",
			wantError:     errManagerIdentityChanged,
		},
		{
			name:          "foreign outgoing manager is not replaced",
			localManager:  identityAnnotations(utils.ManagerKindTerraform, "terraform-source"),
			liveManager:   identityAnnotations(resources.ResourceManagerKind, "grafanactl"),
			appendManager: true,
			wantIdentity:  "terraform-source",
			wantError:     errManagerIdentityChanged,
		},
		{
			name:          "empty live identity does not replace gcx",
			liveManager:   map[string]string{utils.AnnoKeyManagerKind: string(resources.ResourceManagerKind)},
			appendManager: true,
			wantIdentity:  "gcx",
		},
		{
			name:         "omitted manager fields stay omitted",
			liveManager:  identityAnnotations(resources.ResourceManagerKind, "grafanactl"),
			wantIdentity: "",
		},
		{
			name:         "incomplete outgoing manager stays unchanged",
			localManager: map[string]string{utils.AnnoKeyManagerKind: string(resources.ResourceManagerKind)},
			liveManager:  identityAnnotations(resources.ResourceManagerKind, "grafanactl"),
			wantIdentity: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			local := identityTestObject(group, "item-id", test.localManager)
			var live *unstructured.Unstructured
			if test.liveManager != nil {
				live = identityTestObject(group, "item-id", test.liveManager)
				live.SetResourceVersion("77")
			}
			client, summary, err := pushIdentityTest(t, group, local, live, test.appendManager)
			if test.wantError != nil {
				require.ErrorIs(t, err, test.wantError)
				require.Equal(t, 1, summary.FailedCount())
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, summary.SuccessCount())
			}
			var sent *unstructured.Unstructured
			if live == nil {
				sent = client.gotCreate
				require.Nil(t, client.gotUpdate)
			} else {
				sent = client.gotUpdate
				require.Nil(t, client.gotCreate)
			}
			require.NotNil(t, sent)
			require.Equal(t, test.wantIdentity, sent.GetAnnotations()[utils.AnnoKeyManagerIdentity])
			require.Equal(t, 1, client.getCalls)
		})
	}
}
