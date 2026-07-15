package remote //nolint:testpackage // White-box test for the dry-run guard internals.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeDynamicClient records mutating calls so tests can assert whether the guard
// forwarded or blocked them. getNotFound makes Get return a NotFound error.
type fakeDynamicClient struct {
	creates     []string
	updates     []string
	deletes     []string
	gets        []string
	getNotFound bool
}

func (f *fakeDynamicClient) Create(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	f.creates = append(f.creates, obj.GetName())
	return obj, nil
}

func (f *fakeDynamicClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	f.updates = append(f.updates, obj.GetName())
	return obj, nil
}

func (f *fakeDynamicClient) Get(_ context.Context, desc resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	f.gets = append(f.gets, name)
	if f.getNotFound {
		return nil, apierrors.NewNotFound(desc.GroupVersionResource().GroupResource(), name)
	}
	u := &unstructured.Unstructured{}
	u.SetName(name)
	return u, nil
}

func (f *fakeDynamicClient) Delete(_ context.Context, _ resources.Descriptor, name string, _ metav1.DeleteOptions) error {
	f.deletes = append(f.deletes, name)
	return nil
}

func (f *fakeDynamicClient) GetMultiple(_ context.Context, _ resources.Descriptor, names []string, _ metav1.GetOptions) ([]unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeDynamicClient) List(_ context.Context, _ resources.Descriptor, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

func guardAlertRuleDesc() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "rules.alerting.grafana.app", Version: "v0alpha1"},
		Kind:         "AlertRule",
		Singular:     "alertrule",
		Plural:       "alertrules",
	}
}

func guardDashboardDesc() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard",
		Singular:     "dashboard",
		Plural:       "dashboards",
	}
}

func namedObject(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	return u
}

func dryRun() []string { return []string{metav1.DryRunAll} }

// testAllowlist builds an allowlist from assumed values, asserting none were malformed.
func testAllowlist(t *testing.T, assumed ...string) dryRunAllowlist {
	t.Helper()
	a, invalid := newDryRunAllowlist(assumed)
	require.Empty(t, invalid)
	return a
}

func TestDryRunGuard_BlocksNonHonoringCreate(t *testing.T) {
	fake := &fakeDynamicClient{}
	warn := &bytes.Buffer{}
	guard := newDryRunGuard(fake, testAllowlist(t), warn)

	_, err := guard.Create(context.Background(), guardAlertRuleDesc(), namedObject("rule-1"), metav1.CreateOptions{DryRun: dryRun()})

	require.ErrorIs(t, err, errDryRunUnverified)
	require.Empty(t, fake.creates, "guard must not forward the Create to a non-honoring API")
	require.Contains(t, warn.String(), "alertrules.rules.alerting.grafana.app")
	require.Contains(t, warn.String(), "did not validate the spec", "warning should itemize what was NOT checked")
	require.Contains(t, warn.String(), "No changes were sent")
}

func TestDryRunGuard_BlocksNonHonoringUpdate(t *testing.T) {
	fake := &fakeDynamicClient{}
	guard := newDryRunGuard(fake, testAllowlist(t), &bytes.Buffer{})

	_, err := guard.Update(context.Background(), guardAlertRuleDesc(), namedObject("rule-1"), metav1.UpdateOptions{DryRun: dryRun()})

	require.ErrorIs(t, err, errDryRunUnverified)
	require.Empty(t, fake.updates)
}

func TestDryRunGuard_BlocksNonHonoringDelete_ExistenceChecked(t *testing.T) {
	fake := &fakeDynamicClient{}
	warn := &bytes.Buffer{}
	guard := newDryRunGuard(fake, testAllowlist(t), warn)

	err := guard.Delete(context.Background(), guardAlertRuleDesc(), "rule-1", metav1.DeleteOptions{DryRun: dryRun()})

	require.ErrorIs(t, err, errDryRunUnverified)
	require.Equal(t, []string{"rule-1"}, fake.gets, "guard should confirm existence via Get")
	require.Empty(t, fake.deletes, "guard must not forward the Delete to a non-honoring API")
	require.Contains(t, warn.String(), "whether the target exists", "delete warning should describe the existence check")
	require.Contains(t, warn.String(), "No delete was sent")
}

func TestDryRunGuard_BlocksNonHonoringDelete_NotFound(t *testing.T) {
	fake := &fakeDynamicClient{getNotFound: true}
	guard := newDryRunGuard(fake, testAllowlist(t), &bytes.Buffer{})

	err := guard.Delete(context.Background(), guardAlertRuleDesc(), "rule-1", metav1.DeleteOptions{DryRun: dryRun()})

	require.ErrorIs(t, err, errDryRunUnverified, "a missing resource is still reported as skipped, not an error")
	require.Empty(t, fake.deletes)
}

func TestDryRunGuard_PassesThroughAllowlisted(t *testing.T) {
	fake := &fakeDynamicClient{}
	warn := &bytes.Buffer{}
	guard := newDryRunGuard(fake, testAllowlist(t), warn)

	_, err := guard.Create(context.Background(), guardDashboardDesc(), namedObject("dash-1"), metav1.CreateOptions{DryRun: dryRun()})

	require.NoError(t, err)
	require.Equal(t, []string{"dash-1"}, fake.creates, "allowlisted dry-run must pass through so the server validates")
	require.Empty(t, warn.String())
}

func TestDryRunGuard_PassesThroughNonDryRun(t *testing.T) {
	fake := &fakeDynamicClient{}
	guard := newDryRunGuard(fake, testAllowlist(t), &bytes.Buffer{})

	_, err := guard.Create(context.Background(), guardAlertRuleDesc(), namedObject("rule-1"), metav1.CreateOptions{})

	require.NoError(t, err)
	require.Equal(t, []string{"rule-1"}, fake.creates, "a real (non-dry-run) create is never blocked")
}

func TestDryRunGuard_UserAssertedPassesThroughWithNote(t *testing.T) {
	fake := &fakeDynamicClient{}
	warn := &bytes.Buffer{}
	guard := newDryRunGuard(fake, testAllowlist(t, "alertrules.rules.alerting.grafana.app"), warn)

	_, err := guard.Create(context.Background(), guardAlertRuleDesc(), namedObject("rule-1"), metav1.CreateOptions{DryRun: dryRun()})

	require.NoError(t, err)
	require.Equal(t, []string{"rule-1"}, fake.creates)
	require.Contains(t, warn.String(), "user assertion")
}

func TestDryRunGuard_WarnsOncePerGroupResource(t *testing.T) {
	fake := &fakeDynamicClient{}
	warn := &bytes.Buffer{}
	guard := newDryRunGuard(fake, testAllowlist(t), warn)

	for _, name := range []string{"rule-1", "rule-2", "rule-3"} {
		_, err := guard.Create(context.Background(), guardAlertRuleDesc(), namedObject(name), metav1.CreateOptions{DryRun: dryRun()})
		require.ErrorIs(t, err, errDryRunUnverified)
	}

	require.Equal(t, 1, strings.Count(warn.String(), "alertrules.rules.alerting.grafana.app"),
		"a bulk operation should warn once per GroupResource, not once per resource")
}

func TestDryRunGuard_NonBlockingDeleteError(t *testing.T) {
	// A Get error other than NotFound should surface, not be masked as skipped.
	fake := &errGetClient{err: errors.New("boom")}
	guard := newDryRunGuard(fake, testAllowlist(t), &bytes.Buffer{})

	err := guard.Delete(context.Background(), guardAlertRuleDesc(), "rule-1", metav1.DeleteOptions{DryRun: dryRun()})

	require.Error(t, err)
	require.NotErrorIs(t, err, errDryRunUnverified)
}

type errGetClient struct {
	fakeDynamicClient

	err error
}

func (e *errGetClient) Get(_ context.Context, _ resources.Descriptor, _ string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	return nil, e.err
}
