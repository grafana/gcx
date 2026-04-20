// Package testhelpers provides reusable test doubles for the setup framework.
// It is a regular (non-test) package so that downstream packages (T2–T6) can
// import it in their own test files.
package testhelpers

import (
	"context"
	"time"

	"github.com/grafana/gcx/internal/setup/framework"
)

// FakeStatusDetectable is a test double that implements framework.StatusDetectable.
type FakeStatusDetectable struct {
	ProductName_ string
	Status_      *framework.ProductStatus
	Err          error
	Latency      time.Duration
	ShouldPanic  bool
}

var _ framework.StatusDetectable = (*FakeStatusDetectable)(nil)

func (f *FakeStatusDetectable) ProductName() string { return f.ProductName_ }

func (f *FakeStatusDetectable) Status(_ context.Context) (*framework.ProductStatus, error) {
	if f.ShouldPanic {
		panic("FakeStatusDetectable: ShouldPanic is true")
	}
	if f.Latency > 0 {
		time.Sleep(f.Latency)
	}
	return f.Status_, f.Err
}

// FakeSetupable is a test double that implements framework.Setupable.
type FakeSetupable struct {
	ProductName_     string
	Status_          *framework.ProductStatus
	StatusErr        error
	Latency          time.Duration
	ShouldPanic      bool
	Categories_      []framework.InfraCategory
	ValidateSetupErr error
	// ValidateSetupErrs is consumed one per call; if exhausted, ValidateSetupErr is used.
	ValidateSetupErrs []error
	validateCallCount int
	SetupErr          error
	SetupCalled       bool
	LastParams        map[string]string
	// OnSetup is called at the start of Setup, before recording or returning errors.
	OnSetup func()
	// SetupOrderRecorder, when non-nil, gets ProductName_ appended on each Setup call.
	SetupOrderRecorder *[]string
}

var _ framework.Setupable = (*FakeSetupable)(nil)

func (f *FakeSetupable) ProductName() string { return f.ProductName_ }

func (f *FakeSetupable) Status(_ context.Context) (*framework.ProductStatus, error) {
	if f.ShouldPanic {
		panic("FakeSetupable: ShouldPanic is true")
	}
	if f.Latency > 0 {
		time.Sleep(f.Latency)
	}
	return f.Status_, f.StatusErr
}

func (f *FakeSetupable) InfraCategories() []framework.InfraCategory {
	return f.Categories_
}

func (f *FakeSetupable) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (f *FakeSetupable) ValidateSetup(_ context.Context, params map[string]string) error {
	f.LastParams = params
	idx := f.validateCallCount
	f.validateCallCount++
	if idx < len(f.ValidateSetupErrs) {
		return f.ValidateSetupErrs[idx]
	}
	return f.ValidateSetupErr
}

func (f *FakeSetupable) Setup(_ context.Context, params map[string]string) error {
	if f.OnSetup != nil {
		f.OnSetup()
	}
	f.SetupCalled = true
	f.LastParams = params
	if f.SetupOrderRecorder != nil {
		*f.SetupOrderRecorder = append(*f.SetupOrderRecorder, f.ProductName_)
	}
	return f.SetupErr
}
