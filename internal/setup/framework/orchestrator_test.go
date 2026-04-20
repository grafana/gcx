package framework_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/grafana/gcx/internal/setup/framework/testhelpers"
)

func makeOpts(in string, providers []framework.Setupable) framework.Options {
	return framework.Options{
		In:            strings.NewReader(in),
		Out:           &bytes.Buffer{},
		Err:           &bytes.Buffer{},
		Providers:     providers,
		IsInteractive: func() bool { return true },
		SecretFn:      func(label string) (string, error) { return "secret-value", nil },
	}
}

var infraCat = framework.InfraCategory{
	ID:    "infra",
	Label: "Infrastructure",
	Params: []framework.SetupParam{
		{Name: "endpoint", Prompt: "Endpoint", Kind: framework.ParamKindText, Required: true},
	},
}

var secretInfraCat = framework.InfraCategory{
	ID:    "infra",
	Label: "Infrastructure",
	Params: []framework.SetupParam{
		{Name: "token", Prompt: "API Token", Kind: framework.ParamKindText, Secret: true},
	},
}

func notConfiguredStatus(name string) *framework.ProductStatus {
	return &framework.ProductStatus{Product: name, State: framework.StateNotConfigured}
}

func configuredStatus(name string) *framework.ProductStatus {
	return &framework.ProductStatus{Product: name, State: framework.StateConfigured}
}

// TestRun_ZeroCategories verifies early exit when no providers have categories.
func TestRun_ZeroCategories(t *testing.T) {
	p := &testhelpers.FakeSetupable{
		ProductName_: "alpha",
		Status_:      notConfiguredStatus("alpha"),
		// No categories
	}
	opts := makeOpts("", []framework.Setupable{p})
	summary, err := framework.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Completed) != 0 || len(summary.Failed) != 0 {
		t.Errorf("expected empty summary, got %+v", summary)
	}
}

// TestRun_SkipIfConfigured verifies that providers already configured are skipped.
func TestRun_SkipIfConfigured(t *testing.T) {
	p := &testhelpers.FakeSetupable{
		ProductName_: "alpha",
		Status_:      configuredStatus("alpha"),
		Categories_:  []framework.InfraCategory{infraCat},
	}
	// Select "Infrastructure" by pressing Enter (default = all selected)
	// then hit Enter to accept defaults
	opts := makeOpts("\n\n", []framework.Setupable{p})
	summary, err := framework.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Skipped) != 1 || summary.Skipped[0] != "alpha" {
		t.Errorf("expected alpha skipped, got %+v", summary)
	}
	if p.SetupCalled {
		t.Error("Setup should not have been called for already-configured provider")
	}
}

// TestRun_ValidationRetry verifies that validation errors cause re-prompting.
func TestRun_ValidationRetry(t *testing.T) {
	validationErr := errors.New("invalid endpoint")
	p := &testhelpers.FakeSetupable{
		ProductName_: "beta",
		Status_:      notConfiguredStatus("beta"),
		Categories_:  []framework.InfraCategory{infraCat},
		ValidateSetupErrs: []error{
			validationErr, // first attempt fails
			nil,           // second attempt succeeds
		},
	}
	// Input: Enter (default categories) + "bad\n" + "good\n" (two param prompts) + Enter (confirm)
	opts := makeOpts("\nbad\ngood\n\n", []framework.Setupable{p})
	opts.Out = &bytes.Buffer{}
	opts.Err = &bytes.Buffer{}
	summary, err := framework.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Completed) != 1 || summary.Completed[0] != "beta" {
		t.Errorf("expected beta completed, got %+v", summary)
	}
	if p.LastParams["endpoint"] != "good" {
		t.Errorf("expected last params to have endpoint=good, got %v", p.LastParams)
	}
}

// TestRun_SecretMasking verifies that secret params use SecretFn and preview masks values.
func TestRun_SecretMasking(t *testing.T) {
	p := &testhelpers.FakeSetupable{
		ProductName_: "gamma",
		Status_:      notConfiguredStatus("gamma"),
		Categories_:  []framework.InfraCategory{secretInfraCat},
	}
	var outBuf bytes.Buffer
	secretCalled := false
	opts := framework.Options{
		In:            strings.NewReader("\n\n"), // Enter for category, Enter for confirm
		Out:           &outBuf,
		Err:           &bytes.Buffer{},
		Providers:     []framework.Setupable{p},
		IsInteractive: func() bool { return true },
		SecretFn: func(label string) (string, error) {
			secretCalled = true
			return "super-secret", nil
		},
	}
	summary, err := framework.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !secretCalled {
		t.Error("SecretFn was not called")
	}
	// Preview must show *** not the actual value.
	outStr := outBuf.String()
	if strings.Contains(outStr, "super-secret") {
		t.Errorf("preview should not contain the secret value, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "***") {
		t.Errorf("preview should contain *** for secret, got:\n%s", outStr)
	}
	if len(summary.Completed) != 1 || summary.Completed[0] != "gamma" {
		t.Errorf("expected gamma completed, got %+v", summary)
	}
	if p.LastParams["token"] != "super-secret" {
		t.Errorf("Setup should receive actual secret, got %v", p.LastParams)
	}
}

// TestRun_AlphabeticalOrder verifies providers are set up in alphabetical order.
func TestRun_AlphabeticalOrder(t *testing.T) {
	var order []string
	mkProvider := func(name string) *testhelpers.FakeSetupable {
		return &testhelpers.FakeSetupable{
			ProductName_:       name,
			Status_:            notConfiguredStatus(name),
			Categories_:        []framework.InfraCategory{infraCat},
			SetupOrderRecorder: &order,
		}
	}
	// Add in reverse alphabetical order to test sorting.
	providers := []framework.Setupable{
		mkProvider("zeta"),
		mkProvider("alpha"),
		mkProvider("mu"),
	}
	// Input: Enter (select all categories) + 3x "val\n" for 3 param prompts + Enter (confirm)
	opts := makeOpts("\nval\nval\nval\n\n", providers)
	summary, err := framework.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Completed) != 3 {
		t.Fatalf("expected 3 completed, got %+v", summary)
	}
	if order[0] != "alpha" || order[1] != "mu" || order[2] != "zeta" {
		t.Errorf("expected alphabetical order [alpha mu zeta], got %v", order)
	}
}

// TestRun_SetupErrorIsolation verifies that a Setup failure doesn't abort the run.
func TestRun_SetupErrorIsolation(t *testing.T) {
	setupErr := errors.New("setup failed")
	alpha := &testhelpers.FakeSetupable{
		ProductName_: "alpha",
		Status_:      notConfiguredStatus("alpha"),
		Categories_:  []framework.InfraCategory{infraCat},
		SetupErr:     setupErr,
	}
	beta := &testhelpers.FakeSetupable{
		ProductName_: "beta",
		Status_:      notConfiguredStatus("beta"),
		Categories_:  []framework.InfraCategory{infraCat},
	}
	// Input: Enter (categories) + "v1\n" + "v2\n" (two params) + Enter (confirm)
	opts := makeOpts("\nv1\nv2\n\n", []framework.Setupable{alpha, beta})
	summary, err := framework.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Failed) != 1 || summary.Failed[0] != "alpha" {
		t.Errorf("expected alpha in Failed, got %+v", summary)
	}
	if len(summary.Completed) != 1 || summary.Completed[0] != "beta" {
		t.Errorf("expected beta in Completed, got %+v", summary)
	}
}

// TestRun_CtrlCSimulation verifies that context cancellation during Setup is handled gracefully.
func TestRun_CtrlCSimulation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var order []string
	alpha := &testhelpers.FakeSetupable{
		ProductName_:       "alpha",
		Status_:            notConfiguredStatus("alpha"),
		Categories_:        []framework.InfraCategory{infraCat},
		SetupOrderRecorder: &order,
		OnSetup:            cancel, // cancels context on first Setup call
	}
	beta := &testhelpers.FakeSetupable{
		ProductName_:       "beta",
		Status_:            notConfiguredStatus("beta"),
		Categories_:        []framework.InfraCategory{infraCat},
		SetupOrderRecorder: &order,
	}

	// Input: Enter (categories) + "v1\n" + "v2\n" + Enter (confirm)
	opts := makeOpts("\nv1\nv2\n\n", []framework.Setupable{alpha, beta})
	opts.Out = &bytes.Buffer{}
	opts.Err = &bytes.Buffer{}
	summary, err := framework.Run(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// alpha should have been called (it cancelled the context), beta should be in Cancelled.
	if len(order) != 1 || order[0] != "alpha" {
		t.Errorf("expected only alpha Setup called, got %v", order)
	}
	if len(summary.Cancelled) == 0 {
		t.Errorf("expected beta in Cancelled, got %+v", summary)
	}
}

// TestRun_NonInteractiveRefusal verifies that a non-TTY stdin is rejected.
func TestRun_NonInteractiveRefusal(t *testing.T) {
	opts := framework.Options{
		In:            strings.NewReader(""),
		Out:           &bytes.Buffer{},
		Err:           &bytes.Buffer{},
		Providers:     nil,
		IsInteractive: func() bool { return false },
	}
	_, err := framework.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error for non-interactive terminal")
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestRun_NotImplementedSentinel verifies that ErrSetupNotSupported routes to NotImplemented, not Failed.
func TestRun_NotImplementedSentinel(t *testing.T) {
	p := &testhelpers.FakeSetupable{
		ProductName_: "stub",
		Status_:      notConfiguredStatus("stub"),
		Categories_:  []framework.InfraCategory{infraCat},
		SetupErr:     framework.ErrSetupNotSupported,
	}
	opts := makeOpts("\nval\n\n", []framework.Setupable{p})
	summary, err := framework.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Failed) != 0 {
		t.Errorf("ErrSetupNotSupported should not be in Failed, got %v", summary.Failed)
	}
	if len(summary.NotImplemented) != 1 || summary.NotImplemented[0] != "stub" {
		t.Errorf("expected stub in NotImplemented, got %+v", summary)
	}
}

// TestRun_UserRefusal verifies that declining the preview causes all providers to be Cancelled.
func TestRun_UserRefusal(t *testing.T) {
	p := &testhelpers.FakeSetupable{
		ProductName_: "alpha",
		Status_:      notConfiguredStatus("alpha"),
		Categories_:  []framework.InfraCategory{infraCat},
	}
	// Input: Enter (categories) + "val\n" (param) + "n\n" (decline)
	opts := makeOpts("\nval\nn\n", []framework.Setupable{p})
	summary, err := framework.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.SetupCalled {
		t.Error("Setup should not be called when user declines")
	}
	if len(summary.Cancelled) == 0 {
		t.Errorf("expected alpha in Cancelled, got %+v", summary)
	}
}
