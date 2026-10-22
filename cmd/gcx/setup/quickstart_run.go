package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/providers/metrics"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/promql-builder/go/promql"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	quickstartResultType     = "gcx.setup.quickstart.result"
	quickstartSchemaVersion  = "1"
	quickstartContactName    = "quickstart-email"
	quickstartAlertName      = "quickstart-usage"
	quickstartFolderName     = "gcx-quickstart"
	quickstartFolderTitle    = "gcx quickstart"
	quickstartDashboardName  = "quickstart-usage-dashboard"
	quickstartDashboardTitle = "Grafana Cloud active series"
	folderAnnotationKey      = "grafana.app/folder"
	usageSeriesThreshold     = 100000
)

func alertRuleDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "rules.alerting.grafana.app", Version: "v0alpha1"},
		Kind:         "AlertRule",
		Singular:     "alertrule",
		Plural:       "alertrules",
	}
}

func folderDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1beta1"},
		Kind:         "Folder",
		Singular:     "folder",
		Plural:       "folders",
	}
}

func dashboardDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"},
		Kind:         "Dashboard",
		Singular:     "dashboard",
		Plural:       "dashboards",
	}
}

// Discriminated result document for `gcx setup quickstart`.
type quickstartResult struct {
	Type          string           `json:"type" yaml:"type"`
	SchemaVersion string           `json:"schema_version" yaml:"schema_version"`
	Steps         []quickstartStep `json:"steps" yaml:"steps"`
}

type quickstartStep struct {
	Step      string `json:"step" yaml:"step"`
	Action    string `json:"action" yaml:"action"`
	Detail    string `json:"detail,omitempty" yaml:"detail,omitempty"`
	Reference string `json:"reference,omitempty" yaml:"reference,omitempty"`
}

const (
	stepContactPoint   = "contact_point"
	stepUsageAlert     = "usage_alert"
	stepUsageDashboard = "usage_dashboard"
	actionCreated      = "created"
	actionExists       = "exists"
	actionSkipped      = "skipped"
	actionFailed       = "failed"
)

func newQuickstartResult(steps []quickstartStep) quickstartResult {
	if steps == nil {
		steps = []quickstartStep{}
	}
	return quickstartResult{
		Type:          quickstartResultType,
		SchemaVersion: quickstartSchemaVersion,
		Steps:         steps,
	}
}

type contactPointsAPI interface {
	ListContactPoints(ctx context.Context) ([]alert.ContactPoint, error)
	CreateContactPoint(ctx context.Context, cp alert.ContactPoint) (*alert.ContactPoint, error)
}

type alertRuleAPI interface {
	Get(ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions) (*unstructured.Unstructured, error)
	Create(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions) (*unstructured.Unstructured, error)
}

type quickstartRunner struct {
	contacts    contactPointsAPI
	rules       alertRuleAPI
	stdout      io.Writer
	stderr      io.Writer
	isTTY       bool
	promptYesNo func(name string, current bool) (bool, error)
	promptText  func(name, defaultVal string) (string, error)
}

type quickstartPlan struct {
	contactPoint    bool
	usageAlert      bool
	usageDashboard  bool
	email           string
	dashboardDetail string
	emailDetail     string
}

func runQuickstart(ctx context.Context, o *quickstartOpts, r *quickstartRunner) error {
	plan, err := resolveQuickstartPlan(o, r)
	if err != nil {
		return err
	}
	return executeQuickstart(ctx, o, plan, r)
}

func executeQuickstart(ctx context.Context, o *quickstartOpts, plan quickstartPlan, r *quickstartRunner) error {
	steps := make([]quickstartStep, 0, 3)
	steps = append(steps, runContactPoint(ctx, r, plan))
	steps = append(steps, runUsageAlert(ctx, r, plan))
	steps = append(steps, runUsageDashboard(ctx, r, plan))

	result := newQuickstartResult(steps)
	if err := o.IO.Encode(r.stdout, result); err != nil {
		return err
	}

	failed := 0
	for _, s := range steps {
		if s.Action == actionFailed {
			failed++
		}
	}
	if failed > 0 {
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure,
			gcxerrors.NewPartialFailureError("quickstart", len(steps), failed))
	}
	return nil
}

func runContactPoint(ctx context.Context, r *quickstartRunner, plan quickstartPlan) quickstartStep {
	if !plan.contactPoint {
		detail := plan.emailDetail
		if detail == "" {
			detail = "--contact-point=false"
		}
		return quickstartStep{Step: stepContactPoint, Action: actionSkipped, Detail: detail}
	}
	return r.ensureContactPoint(ctx, plan.email)
}

func runUsageAlert(ctx context.Context, r *quickstartRunner, plan quickstartPlan) quickstartStep {
	if !plan.usageAlert {
		detail := plan.emailDetail
		if detail == "" {
			detail = "--usage-alert=false"
		}
		return quickstartStep{Step: stepUsageAlert, Action: actionSkipped, Detail: detail}
	}
	return r.ensureUsageAlert(ctx)
}

func runUsageDashboard(ctx context.Context, r *quickstartRunner, plan quickstartPlan) quickstartStep {
	if !plan.usageDashboard {
		detail := plan.dashboardDetail
		if detail == "" {
			detail = "--dashboard=false"
		}
		return quickstartStep{Step: stepUsageDashboard, Action: actionSkipped, Detail: detail}
	}
	return r.ensureUsageDashboard(ctx)
}

func resolveQuickstartPlan(o *quickstartOpts, r *quickstartRunner) (quickstartPlan, error) {
	switch {
	case o.useDefaults:
		return resolveQuickstartDefaults(o)
	case r.isTTY:
		return resolveQuickstartInteractive(o, r)
	default:
		return quickstartPlan{}, errors.New(
			"setup quickstart: interactive prompts are unavailable (stdin is not a TTY, or agent mode is active); " +
				"use --use-defaults for non-interactive mode")
	}
}

func resolveQuickstartDefaults(o *quickstartOpts) (quickstartPlan, error) {
	plan := quickstartPlan{
		contactPoint:   true,
		usageAlert:     true,
		usageDashboard: true,
		email:          strings.TrimSpace(o.email),
	}
	if o.contactPointSet {
		plan.contactPoint = o.contactPoint
	}
	if o.usageAlertSet {
		plan.usageAlert = o.usageAlert
	}
	if o.dashboardSet {
		plan.usageDashboard = o.dashboard
	}

	if (plan.contactPoint || plan.usageAlert) && plan.email == "" {
		return quickstartPlan{}, errors.New(
			"setup quickstart: --email is required in non-interactive mode when creating a contact point or usage alert; " +
				"pass --email you@example.com")
	}
	if plan.email != "" {
		plan.email = parsedEmail(plan.email)
	}
	return plan, nil
}

func resolveQuickstartInteractive(o *quickstartOpts, r *quickstartRunner) (quickstartPlan, error) {
	plan := quickstartPlan{
		email: strings.TrimSpace(o.email),
	}

	var err error
	plan.contactPoint, err = resolveStepFlag(o.contactPointSet, o.contactPoint, true, "email contact point", r.promptYesNo)
	if err != nil {
		return quickstartPlan{}, fmt.Errorf("setup quickstart: prompt contact-point: %w", err)
	}
	plan.usageAlert, err = resolveStepFlag(o.usageAlertSet, o.usageAlert, true, "usage alert rule", r.promptYesNo)
	if err != nil {
		return quickstartPlan{}, fmt.Errorf("setup quickstart: prompt usage-alert: %w", err)
	}
	plan.usageDashboard, err = resolveStepFlag(o.dashboardSet, o.dashboard, true, "usage dashboard", r.promptYesNo)
	if err != nil {
		return quickstartPlan{}, fmt.Errorf("setup quickstart: prompt dashboard: %w", err)
	}

	if (plan.contactPoint || plan.usageAlert) && plan.email == "" {
		plan.email, err = r.promptText("email for contact point", "")
		if err != nil {
			return quickstartPlan{}, fmt.Errorf("setup quickstart: prompt email: %w", err)
		}
		plan.email = strings.TrimSpace(plan.email)
		if plan.email == "" {
			plan.contactPoint = false
			plan.usageAlert = false
			plan.emailDetail = "no email"
		} else if err := validateEmail(plan.email); err != nil {
			return quickstartPlan{}, fmt.Errorf("invalid email %q: %w", plan.email, err)
		} else {
			plan.email = parsedEmail(plan.email)
		}
	} else if plan.email != "" {
		plan.email = parsedEmail(plan.email)
	}
	return plan, nil
}

func resolveStepFlag(set bool, value, defaultVal bool, prompt string, promptFn func(string, bool) (bool, error)) (bool, error) {
	if set {
		return value, nil
	}
	return promptFn(prompt, defaultVal)
}

func (r *quickstartRunner) ensureContactPoint(ctx context.Context, email string) quickstartStep {
	existing, err := r.contacts.ListContactPoints(ctx)
	if err != nil {
		return failedStep(stepContactPoint, err)
	}
	for _, cp := range existing {
		if cp.Name == quickstartContactName {
			return quickstartStep{
				Step:      stepContactPoint,
				Action:    actionExists,
				Detail:    "contact point already exists",
				Reference: cp.UID,
			}
		}
	}

	settings, err := json.Marshal(map[string]any{"addresses": email})
	if err != nil {
		return failedStep(stepContactPoint, err)
	}
	created, err := r.contacts.CreateContactPoint(ctx, alert.ContactPoint{
		Name:     quickstartContactName,
		Type:     "email",
		Settings: alert.JSONModel(settings),
	})
	if err != nil {
		return failedStep(stepContactPoint, err)
	}
	ref := created.UID
	if ref == "" {
		ref = created.Name
	}
	return quickstartStep{
		Step:      stepContactPoint,
		Action:    actionCreated,
		Detail:    "email contact point for " + email,
		Reference: ref,
	}
}

func (r *quickstartRunner) ensureUsageAlert(ctx context.Context) quickstartStep {
	_, err := r.rules.Get(ctx, alertRuleDescriptor(), quickstartAlertName, metav1.GetOptions{})
	if err == nil {
		return quickstartStep{
			Step:      stepUsageAlert,
			Action:    actionExists,
			Detail:    "alert rule already exists",
			Reference: quickstartAlertName,
		}
	}
	if !apierrors.IsNotFound(err) {
		return failedStep(stepUsageAlert, err)
	}

	folderUID, err := r.ensureQuickstartFolder(ctx)
	if err != nil {
		return failedStep(stepUsageAlert, err)
	}

	obj, buildErr := buildUsageAlertRule(folderUID)
	if buildErr != nil {
		return failedStep(stepUsageAlert, buildErr)
	}
	created, err := r.rules.Create(ctx, alertRuleDescriptor(), obj, metav1.CreateOptions{})
	if err != nil {
		return failedStep(stepUsageAlert, err)
	}
	ref := created.GetName()
	if ref == "" {
		ref = quickstartAlertName
	}
	return quickstartStep{
		Step:      stepUsageAlert,
		Action:    actionCreated,
		Detail:    fmt.Sprintf("fires when active series > %d for 5m; folder %s", usageSeriesThreshold, folderUID),
		Reference: ref,
	}
}

// ensureQuickstartFolder returns the UID of the folder the usage alert is stored in.
// Grafana rejects AlertRule creates that omit metadata.annotations["grafana.app/folder"].
func (r *quickstartRunner) ensureQuickstartFolder(ctx context.Context) (string, error) {
	existing, err := r.rules.Get(ctx, folderDescriptor(), quickstartFolderName, metav1.GetOptions{})
	if err == nil {
		return resourceName(existing, quickstartFolderName), nil
	}
	if !apierrors.IsNotFound(err) {
		return "", err
	}

	created, err := r.rules.Create(ctx, folderDescriptor(), buildQuickstartFolder(), metav1.CreateOptions{})
	if err == nil {
		return resourceName(created, quickstartFolderName), nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return "", err
	}
	existing, err = r.rules.Get(ctx, folderDescriptor(), quickstartFolderName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return resourceName(existing, quickstartFolderName), nil
}

func resourceName(obj *unstructured.Unstructured, fallback string) string {
	if obj == nil || obj.GetName() == "" {
		return fallback
	}
	return obj.GetName()
}

func buildQuickstartFolder() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": folderDescriptor().GroupVersion.String(),
		"kind":       folderDescriptor().Kind,
		"metadata": map[string]any{
			"name": quickstartFolderName,
		},
		"spec": map[string]any{
			"title": quickstartFolderTitle,
		},
	}}
}

func (r *quickstartRunner) ensureUsageDashboard(ctx context.Context) quickstartStep {
	_, err := r.rules.Get(ctx, dashboardDescriptor(), quickstartDashboardName, metav1.GetOptions{})
	if err == nil {
		return quickstartStep{
			Step:      stepUsageDashboard,
			Action:    actionExists,
			Detail:    "dashboard already exists",
			Reference: quickstartDashboardName,
		}
	}
	if !apierrors.IsNotFound(err) {
		return failedStep(stepUsageDashboard, err)
	}

	folderUID, err := r.ensureQuickstartFolder(ctx)
	if err != nil {
		return failedStep(stepUsageDashboard, err)
	}

	obj, buildErr := buildUsageDashboard(folderUID)
	if buildErr != nil {
		return failedStep(stepUsageDashboard, buildErr)
	}
	created, err := r.rules.Create(ctx, dashboardDescriptor(), obj, metav1.CreateOptions{})
	if err != nil {
		return failedStep(stepUsageDashboard, err)
	}
	ref := created.GetName()
	if ref == "" {
		ref = quickstartDashboardName
	}
	return quickstartStep{
		Step:      stepUsageDashboard,
		Action:    actionCreated,
		Detail:    fmt.Sprintf("active series in folder %s", folderUID),
		Reference: ref,
	}
}

func failedStep(step string, err error) quickstartStep {
	return quickstartStep{
		Step:   step,
		Action: actionFailed,
		Detail: err.Error(),
	}
}

func buildUsageAlertRule(folderUID string) (*unstructured.Unstructured, error) {
	expr, err := usageSeriesQuery()
	if err != nil {
		return nil, err
	}

	spec := map[string]any{
		"title":        "Grafana Cloud active series is high",
		"paused":       false,
		"for":          "5m",
		"noDataState":  "Ok",
		"execErrState": "Error",
		"trigger": map[string]any{
			"interval": "1m",
		},
		"annotations": map[string]any{
			"summary": "Grafana Cloud active series exceeded the quickstart threshold.",
		},
		"labels": map[string]any{
			"source": "gcx-quickstart",
		},
		"notificationSettings": map[string]any{
			"type":     "SimplifiedRouting",
			"receiver": quickstartContactName,
		},
		"expressions": map[string]any{
			"A": map[string]any{
				"datasourceUID": metrics.BillingDatasourceUID,
				"queryType":     "",
				"relativeTimeRange": map[string]any{
					"from": "10m",
					"to":   "0s",
				},
				"model": map[string]any{
					"datasource": map[string]any{
						"type": "prometheus",
						"uid":  metrics.BillingDatasourceUID,
					},
					"expr":    expr,
					"instant": true,
					"refId":   "A",
				},
			},
			"B": map[string]any{
				"datasourceUID": "__expr__",
				"source":        true,
				"model": map[string]any{
					"type":       "threshold",
					"expression": "A",
					"refId":      "B",
					"datasource": map[string]any{
						"type": "__expr__",
						"uid":  "__expr__",
					},
					"conditions": []any{
						map[string]any{
							"evaluator": map[string]any{
								"type":   "gt",
								"params": []any{usageSeriesThreshold},
							},
							"operator": map[string]any{"type": "and"},
							"query":    map[string]any{"params": []any{"A"}},
							"reducer":  map[string]any{"type": "last"},
							"type":     "query",
						},
					},
				},
			},
		},
	}

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": alertRuleDescriptor().GroupVersion.String(),
		"kind":       alertRuleDescriptor().Kind,
		"metadata": map[string]any{
			"name": quickstartAlertName,
			"annotations": map[string]any{
				folderAnnotationKey: folderUID,
			},
		},
		"spec": spec,
	}}
	return obj, nil
}

func buildUsageDashboard(folderUID string) (*unstructured.Unstructured, error) {
	expr, err := usageSeriesQuery()
	if err != nil {
		return nil, err
	}

	panel := map[string]any{
		"id":    1,
		"type":  "timeseries",
		"title": "Active series",
		"description": fmt.Sprintf(
			"Same series the quickstart usage alert watches. The alert fires above %d.",
			usageSeriesThreshold,
		),
		"gridPos": map[string]any{"h": 8, "w": 24, "x": 0, "y": 0},
		"datasource": map[string]any{
			"type": "prometheus",
			"uid":  metrics.BillingDatasourceUID,
		},
		"targets": []any{
			map[string]any{
				"datasource": map[string]any{
					"type": "prometheus",
					"uid":  metrics.BillingDatasourceUID,
				},
				"expr":  expr,
				"refId": "A",
			},
		},
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": dashboardDescriptor().GroupVersion.String(),
		"kind":       dashboardDescriptor().Kind,
		"metadata": map[string]any{
			"name": quickstartDashboardName,
			"annotations": map[string]any{
				folderAnnotationKey: folderUID,
			},
		},
		"spec": map[string]any{
			"title":         quickstartDashboardTitle,
			"tags":          []any{"gcx-quickstart"},
			"timezone":      "browser",
			"schemaVersion": 39,
			"refresh":       "1m",
			"time":          map[string]any{"from": "now-6h", "to": "now"},
			"panels":        []any{panel},
		},
	}}, nil
}

func usageSeriesQuery() (string, error) {
	expr, err := promql.Sum(promql.Vector("grafanacloud_instance_active_series")).Build()
	if err != nil {
		return "", fmt.Errorf("build usage query: %w", err)
	}
	return expr.String(), nil
}

func defaultPromptYesNo(in io.Reader, errOut io.Writer) func(name string, current bool) (bool, error) {
	scanner := bufio.NewScanner(in)
	return func(name string, current bool) (bool, error) {
		def := "n"
		if current {
			def = "y"
		}
		fmt.Fprintf(errOut, "%s [y/n, default %s]: ", name, def)
		if !scanner.Scan() {
			return current, scanner.Err()
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			return current, nil
		}
	}
}

func defaultPromptText(in io.Reader, errOut io.Writer) func(name, defaultVal string) (string, error) {
	scanner := bufio.NewScanner(in)
	return func(name, defaultVal string) (string, error) {
		if defaultVal != "" {
			fmt.Fprintf(errOut, "%s [%s]: ", name, defaultVal)
		} else {
			fmt.Fprintf(errOut, "%s: ", name)
		}
		if !scanner.Scan() {
			return defaultVal, scanner.Err()
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			return defaultVal, nil
		}
		return text, nil
	}
}

type quickstartTextCodec struct{}

func (c *quickstartTextCodec) Format() format.Format { return "text" }

func (c *quickstartTextCodec) Decode(io.Reader, any) error {
	return errors.New("setup quickstart text codec does not support decoding")
}

func (c *quickstartTextCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(quickstartResult)
	if !ok {
		return errors.New("invalid data type for setup quickstart text codec: expected quickstartResult")
	}

	t := style.NewTable("STEP", "ACTION", "DETAIL", "REFERENCE")
	for _, s := range result.Steps {
		t.Row(s.Step, s.Action, s.Detail, s.Reference)
	}
	return t.Render(w)
}
