//nolint:testpackage // white-box testing: drives unexported run, opts, and fakes.
package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/providers/metrics"
	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type fakeContactPoints struct {
	list      []alert.ContactPoint
	listErr   error
	created   *alert.ContactPoint
	createErr error
	creates   int
}

func (f *fakeContactPoints) ListContactPoints(context.Context) ([]alert.ContactPoint, error) {
	return f.list, f.listErr
}

func (f *fakeContactPoints) CreateContactPoint(_ context.Context, cp alert.ContactPoint) (*alert.ContactPoint, error) {
	f.creates++
	if f.createErr != nil {
		return nil, f.createErr
	}
	created := cp
	if created.UID == "" {
		created.UID = "cp-uid"
	}
	f.created = &created
	return &created, nil
}

type fakeAlertRules struct {
	existing      bool
	getErr        error
	createErr     error
	folderErr     error
	created       *unstructured.Unstructured
	createdFolder *unstructured.Unstructured
	creates       int
	folderCreates int
	folders       map[string]*unstructured.Unstructured
}

func (f *fakeAlertRules) Get(_ context.Context, desc resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	if desc.Kind == folderDescriptor().Kind {
		if obj, ok := f.folders[name]; ok {
			return obj, nil
		}
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "folder.grafana.app", Resource: "folders"}, name)
	}
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.existing {
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": name},
		}}, nil
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: "rules.alerting.grafana.app", Resource: "alertrules"}, name)
}

func (f *fakeAlertRules) Create(_ context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	if desc.Kind == folderDescriptor().Kind {
		f.folderCreates++
		if f.folderErr != nil {
			return nil, f.folderErr
		}
		f.createdFolder = obj
		if f.folders == nil {
			f.folders = map[string]*unstructured.Unstructured{}
		}
		f.folders[obj.GetName()] = obj
		return obj, nil
	}
	f.creates++
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = obj
	return obj, nil
}

func TestEnsureContactPoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("creates when missing", func(t *testing.T) {
		t.Parallel()
		contacts := &fakeContactPoints{}
		r := &quickstartRunner{contacts: contacts}
		got := r.ensureContactPoint(ctx, "you@example.com")
		assert.Equal(t, actionCreated, got.Action)
		assert.Equal(t, "cp-uid", got.Reference)
		require.NotNil(t, contacts.created)
		assert.Equal(t, quickstartContactName, contacts.created.Name)
		assert.Equal(t, "email", contacts.created.Type)
		assert.Contains(t, string(contacts.created.Settings), "you@example.com")
	})

	t.Run("skips when name exists", func(t *testing.T) {
		t.Parallel()
		contacts := &fakeContactPoints{list: []alert.ContactPoint{{
			UID:  "existing",
			Name: quickstartContactName,
			Type: "email",
		}}}
		r := &quickstartRunner{contacts: contacts}
		got := r.ensureContactPoint(ctx, "you@example.com")
		assert.Equal(t, actionExists, got.Action)
		assert.Equal(t, "existing", got.Reference)
		assert.Zero(t, contacts.creates)
	})

	t.Run("records list failure", func(t *testing.T) {
		t.Parallel()
		r := &quickstartRunner{contacts: &fakeContactPoints{listErr: errors.New("boom")}}
		got := r.ensureContactPoint(ctx, "you@example.com")
		assert.Equal(t, actionFailed, got.Action)
		assert.Contains(t, got.Detail, "boom")
	})
}

func TestEnsureUsageAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("creates when missing", func(t *testing.T) {
		t.Parallel()
		rules := &fakeAlertRules{}
		r := &quickstartRunner{rules: rules}
		got := r.ensureUsageAlert(ctx)
		assert.Equal(t, actionCreated, got.Action)
		assert.Equal(t, quickstartAlertName, got.Reference)
		assert.Contains(t, got.Detail, quickstartFolderName)
		require.NotNil(t, rules.created)
		assert.Equal(t, quickstartAlertName, rules.created.GetName())
		assert.Equal(t, "rules.alerting.grafana.app/v0alpha1", rules.created.GetAPIVersion())
		assert.Equal(t, quickstartFolderName, rules.created.GetAnnotations()[folderAnnotationKey])
		require.NotNil(t, rules.createdFolder)
		assert.Equal(t, quickstartFolderName, rules.createdFolder.GetName())
	})

	t.Run("skips when exists", func(t *testing.T) {
		t.Parallel()
		rules := &fakeAlertRules{existing: true}
		r := &quickstartRunner{rules: rules}
		got := r.ensureUsageAlert(ctx)
		assert.Equal(t, actionExists, got.Action)
		assert.Zero(t, rules.creates)
	})

	t.Run("records create failure", func(t *testing.T) {
		t.Parallel()
		r := &quickstartRunner{rules: &fakeAlertRules{createErr: errors.New("quota")}}
		got := r.ensureUsageAlert(ctx)
		assert.Equal(t, actionFailed, got.Action)
		assert.Contains(t, got.Detail, "quota")
	})

	t.Run("records folder failure", func(t *testing.T) {
		t.Parallel()
		rules := &fakeAlertRules{folderErr: errors.New("folder denied")}
		r := &quickstartRunner{rules: rules}
		got := r.ensureUsageAlert(ctx)
		assert.Equal(t, actionFailed, got.Action)
		assert.Contains(t, got.Detail, "folder denied")
		assert.Zero(t, rules.creates)
	})

	t.Run("reuses existing folder", func(t *testing.T) {
		t.Parallel()
		rules := &fakeAlertRules{folders: map[string]*unstructured.Unstructured{
			quickstartFolderName: {Object: map[string]any{
				"metadata": map[string]any{"name": quickstartFolderName},
			}},
		}}
		r := &quickstartRunner{rules: rules}
		got := r.ensureUsageAlert(ctx)
		assert.Equal(t, actionCreated, got.Action)
		assert.Zero(t, rules.folderCreates)
		assert.Equal(t, quickstartFolderName, rules.created.GetAnnotations()[folderAnnotationKey])
	})
}

func TestBuildUsageAlertRule(t *testing.T) {
	t.Parallel()
	obj, err := buildUsageAlertRule(quickstartFolderName)
	require.NoError(t, err)

	assert.Equal(t, "AlertRule", obj.GetKind())
	assert.Equal(t, quickstartAlertName, obj.GetName())
	assert.Equal(t, quickstartFolderName, obj.GetAnnotations()[folderAnnotationKey])

	spec, ok := obj.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "5m", spec["for"])
	assert.Equal(t, "Ok", spec["noDataState"])
	assert.Equal(t, "Error", spec["execErrState"])

	ns, ok := spec["notificationSettings"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "SimplifiedRouting", ns["type"])
	assert.Equal(t, quickstartContactName, ns["receiver"])

	exprs, ok := spec["expressions"].(map[string]any)
	require.True(t, ok)
	a, ok := exprs["A"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, metrics.BillingDatasourceUID, a["datasourceUID"])
	model, ok := a["model"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sum(grafanacloud_instance_active_series)", model["expr"])
}

func TestEnsureUsageDashboard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("creates when missing", func(t *testing.T) {
		t.Parallel()
		rules := &fakeAlertRules{}
		r := &quickstartRunner{rules: rules}
		got := r.ensureUsageDashboard(ctx)
		assert.Equal(t, actionCreated, got.Action)
		assert.Equal(t, quickstartDashboardName, got.Reference)
		assert.Contains(t, got.Detail, quickstartFolderName)
		require.NotNil(t, rules.created)
		assert.Equal(t, "Dashboard", rules.created.GetKind())
		assert.Equal(t, quickstartDashboardName, rules.created.GetName())
		assert.Equal(t, quickstartFolderName, rules.created.GetAnnotations()[folderAnnotationKey])
		require.NotNil(t, rules.createdFolder)
		assert.Equal(t, 1, rules.folderCreates)
	})

	t.Run("skips when exists", func(t *testing.T) {
		t.Parallel()
		rules := &fakeAlertRules{existing: true}
		r := &quickstartRunner{rules: rules}
		got := r.ensureUsageDashboard(ctx)
		assert.Equal(t, actionExists, got.Action)
		assert.Equal(t, quickstartDashboardName, got.Reference)
		assert.Zero(t, rules.creates)
	})

	t.Run("records create failure", func(t *testing.T) {
		t.Parallel()
		r := &quickstartRunner{rules: &fakeAlertRules{createErr: errors.New("quota exceeded")}}
		got := r.ensureUsageDashboard(ctx)
		assert.Equal(t, actionFailed, got.Action)
		assert.Contains(t, got.Detail, "quota exceeded")
	})
}

func TestBuildUsageDashboard(t *testing.T) {
	t.Parallel()
	obj, err := buildUsageDashboard(quickstartFolderName)
	require.NoError(t, err)

	assert.Equal(t, "Dashboard", obj.GetKind())
	assert.Equal(t, "dashboard.grafana.app/v1beta1", obj.GetAPIVersion())
	assert.Equal(t, quickstartDashboardName, obj.GetName())
	assert.Equal(t, quickstartFolderName, obj.GetAnnotations()[folderAnnotationKey])

	spec, ok := obj.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, quickstartDashboardTitle, spec["title"])

	panels, ok := spec["panels"].([]any)
	require.True(t, ok)
	require.Len(t, panels, 1)
	panel, ok := panels[0].(map[string]any)
	require.True(t, ok)
	targets, ok := panel["targets"].([]any)
	require.True(t, ok)
	require.Len(t, targets, 1)
	target, ok := targets[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sum(grafanacloud_instance_active_series)", target["expr"])
}

func TestRunQuickstart_UseDefaults_EndToEnd(t *testing.T) {
	contacts := &fakeContactPoints{}
	rules := &fakeAlertRules{}
	var stdout bytes.Buffer

	opts := &quickstartOpts{}
	flags := pflag.NewFlagSet("quickstart", pflag.ContinueOnError)
	opts.setup(flags)
	require.NoError(t, flags.Parse([]string{
		"--use-defaults", "--email", "you@example.com", "-o", "json",
	}))
	opts.emailSet = true
	require.NoError(t, opts.Validate())

	r := &quickstartRunner{
		contacts: contacts,
		rules:    rules,
		stdout:   &stdout,
		stderr:   io.Discard,
		isTTY:    false,
		promptYesNo: func(string, bool) (bool, error) {
			t.Fatal("prompt must not fire under --use-defaults")
			return false, nil
		},
		promptText: func(string, string) (string, error) {
			t.Fatal("prompt must not fire under --use-defaults")
			return "", nil
		},
	}

	err := runQuickstart(context.Background(), opts, r)
	require.NoError(t, err)
	assert.Equal(t, 1, contacts.creates)
	assert.Equal(t, 2, rules.creates)
	assert.Equal(t, 1, rules.folderCreates)
	assert.Equal(t, "Dashboard", rules.created.GetKind())
	assert.Equal(t, quickstartDashboardName, rules.created.GetName())
	assert.Equal(t, quickstartFolderName, rules.created.GetAnnotations()[folderAnnotationKey])

	doc := decodeQuickstartJSON(t, stdout.String())
	assert.Equal(t, quickstartResultType, doc["type"])
	assert.Equal(t, "1", doc["schema_version"])
	steps, ok := doc["steps"].([]any)
	require.True(t, ok)
	require.Len(t, steps, 3)
	for i, want := range []string{stepContactPoint, stepUsageAlert, stepUsageDashboard} {
		step, ok := steps[i].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, want, step["step"])
		assert.Equal(t, actionCreated, step["action"])
	}
}

func TestRunQuickstart_UseDefaults_Idempotent(t *testing.T) {
	contacts := &fakeContactPoints{list: []alert.ContactPoint{{UID: "cp", Name: quickstartContactName}}}
	rules := &fakeAlertRules{existing: true}
	var stdout bytes.Buffer

	opts := &quickstartOpts{}
	flags := pflag.NewFlagSet("quickstart", pflag.ContinueOnError)
	opts.setup(flags)
	require.NoError(t, flags.Parse([]string{
		"--use-defaults", "--email", "you@example.com", "-o", "json",
	}))
	opts.emailSet = true
	require.NoError(t, opts.Validate())

	r := &quickstartRunner{
		contacts: contacts,
		rules:    rules,
		stdout:   &stdout,
		stderr:   io.Discard,
		isTTY:    false,
	}
	require.NoError(t, runQuickstart(context.Background(), opts, r))
	assert.Zero(t, contacts.creates)
	assert.Zero(t, rules.creates)

	doc := decodeQuickstartJSON(t, stdout.String())
	steps, ok := doc["steps"].([]any)
	require.True(t, ok)
	for _, raw := range steps {
		step := raw.(map[string]any)
		assert.Equal(t, actionExists, step["action"])
	}
}

func TestRunQuickstart_PartialFailure_ContinuesAndEmits(t *testing.T) {
	opts := &quickstartOpts{}
	flags := pflag.NewFlagSet("quickstart", pflag.ContinueOnError)
	opts.setup(flags)
	require.NoError(t, flags.Parse([]string{
		"--use-defaults", "--email", "you@example.com", "-o", "json",
	}))
	opts.emailSet = true
	require.NoError(t, opts.Validate())

	var stdout bytes.Buffer
	r := &quickstartRunner{
		contacts: &fakeContactPoints{},
		rules:    &fakeAlertRules{createErr: errors.New("denied")},
		stdout:   &stdout,
		stderr:   io.Discard,
	}
	err := runQuickstart(context.Background(), opts, r)
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

	doc := decodeQuickstartJSON(t, stdout.String())
	steps, ok := doc["steps"].([]any)
	require.True(t, ok)
	require.Len(t, steps, 3)
	assert.Equal(t, actionCreated, steps[0].(map[string]any)["action"])
	assert.Equal(t, actionFailed, steps[1].(map[string]any)["action"])
	assert.Equal(t, actionFailed, steps[2].(map[string]any)["action"])
}

func TestRunQuickstart_UseDefaults_MissingEmail(t *testing.T) {
	opts := &quickstartOpts{useDefaults: true}
	r := &quickstartRunner{stdout: io.Discard, stderr: io.Discard}
	err := runQuickstart(context.Background(), opts, r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--email")
}

func TestRunQuickstart_UseDefaults_SkipDashboard(t *testing.T) {
	opts := &quickstartOpts{}
	flags := pflag.NewFlagSet("quickstart", pflag.ContinueOnError)
	opts.setup(flags)
	require.NoError(t, flags.Parse([]string{
		"--use-defaults", "--email", "you@example.com", "--dashboard=false", "-o", "json",
	}))
	opts.emailSet = true
	opts.dashboardSet = flags.Changed("dashboard")
	require.NoError(t, opts.Validate())

	rules := &fakeAlertRules{}
	var stdout bytes.Buffer
	r := &quickstartRunner{
		contacts: &fakeContactPoints{},
		rules:    rules,
		stdout:   &stdout,
		stderr:   io.Discard,
	}
	require.NoError(t, runQuickstart(context.Background(), opts, r))
	assert.Equal(t, 1, rules.creates)
	assert.Equal(t, "AlertRule", rules.created.GetKind())

	doc := decodeQuickstartJSON(t, stdout.String())
	steps := doc["steps"].([]any)
	dash := steps[2].(map[string]any)
	assert.Equal(t, stepUsageDashboard, dash["step"])
	assert.Equal(t, actionSkipped, dash["action"])
	assert.Equal(t, "--dashboard=false", dash["detail"])
}

func TestQuickstartOpts_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "empty email rejected", args: []string{"--email", ""}, wantErr: "--email must not be empty"},
		{name: "bad email rejected", args: []string{"--email", "not-an-email"}, wantErr: "invalid --email"},
		{name: "valid email accepted", args: []string{"--email", "you@example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flags := pflag.NewFlagSet("quickstart", pflag.ContinueOnError)
			opts := &quickstartOpts{}
			opts.setup(flags)
			require.NoError(t, flags.Parse(tc.args))
			opts.emailSet = flags.Changed("email")
			err := opts.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestQuickstartTextCodec_Table(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := (&quickstartTextCodec{}).Encode(&buf, newQuickstartResult([]quickstartStep{
		{Step: stepContactPoint, Action: actionCreated, Detail: "email", Reference: "uid-1"},
	}))
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "STEP")
	assert.Contains(t, out, "contact_point")
	assert.Contains(t, out, "created")
	assert.Contains(t, out, "uid-1")
}

func TestQuickstartJSON_EmptyStepsAreArray(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(newQuickstartResult(nil))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"steps":[]`)
	assert.NotContains(t, string(data), `"steps":null`)
}

func decodeQuickstartJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout is not valid JSON: %s", raw)
	require.ErrorIs(t, dec.Decode(new(any)), io.EOF, "stdout must contain exactly one JSON value")
	doc, ok := first.(map[string]any)
	require.True(t, ok, "document is %T, want object", first)
	return doc
}

func TestResolveQuickstartInteractive_HonorsExplicitFalse(t *testing.T) {
	promptCalls := 0
	opts := &quickstartOpts{
		contactPointSet: true,
		contactPoint:    false,
		usageAlertSet:   true,
		usageAlert:      false,
		dashboardSet:    true,
		dashboard:       false,
	}
	plan, err := resolveQuickstartInteractive(opts, &quickstartRunner{
		promptYesNo: func(string, bool) (bool, error) {
			promptCalls++
			return true, nil
		},
		promptText: func(string, string) (string, error) {
			t.Fatal("text prompt should not fire when all steps are off")
			return "", nil
		},
	})
	require.NoError(t, err)
	assert.Zero(t, promptCalls)
	assert.False(t, plan.contactPoint)
	assert.False(t, plan.usageAlert)
	assert.False(t, plan.usageDashboard)
}

func TestResolveQuickstartInteractive_PromptsEmailAndKeepsDashboard(t *testing.T) {
	plan, err := resolveQuickstartInteractive(&quickstartOpts{}, &quickstartRunner{
		stderr: io.Discard,
		promptYesNo: func(_ string, current bool) (bool, error) {
			return current, nil
		},
		promptText: func(name, _ string) (string, error) {
			if strings.Contains(name, "email") {
				return "you@example.com", nil
			}
			return "", nil
		},
	})
	require.NoError(t, err)
	assert.True(t, plan.contactPoint)
	assert.True(t, plan.usageAlert)
	assert.True(t, plan.usageDashboard)
	assert.Equal(t, "you@example.com", plan.email)
	assert.Empty(t, plan.dashboardDetail)
}
