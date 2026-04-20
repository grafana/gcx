package framework

import (
	"context"
	"errors"
)

// ProductState represents the configuration/activation state of a product.
type ProductState string

const (
	StateNotConfigured ProductState = "not_configured"
	StateConfigured    ProductState = "configured"
	StateActive        ProductState = "active"
	StateError         ProductState = "error"
)

// ProductStatus is the status snapshot returned by StatusDetectable.Status().
type ProductStatus struct {
	Product   string       `json:"product"    yaml:"product"`
	State     ProductState `json:"state"      yaml:"state"`
	Details   string       `json:"details"    yaml:"details"`
	SetupHint string       `json:"setup_hint" yaml:"setup_hint"`
}

// InfraCategoryID is the identifier for an infrastructure category.
type InfraCategoryID string

// InfraCategory groups related setup parameters under a named category.
type InfraCategory struct {
	ID     InfraCategoryID
	Label  string
	Params []SetupParam
}

// ParamKind describes the type of input a SetupParam expects.
type ParamKind string

const (
	ParamKindText        ParamKind = "text"
	ParamKindBool        ParamKind = "bool"
	ParamKindChoice      ParamKind = "choice"
	ParamKindMultiChoice ParamKind = "multi_choice"
)

// SetupParam describes a single configurable parameter for a setup flow.
type SetupParam struct {
	Name     string
	Prompt   string
	Default  string
	Kind     ParamKind
	Required bool
	Secret   bool
	Choices  []string
}

// StatusDetectable is implemented by providers that can report their
// configuration/activation state without performing a full setup.
type StatusDetectable interface {
	ProductName() string
	Status(ctx context.Context) (*ProductStatus, error)
}

// Setupable is implemented by providers that support guided setup flows.
// It extends StatusDetectable with setup orchestration methods.
type Setupable interface {
	StatusDetectable

	// InfraCategories returns the categories of infrastructure parameters
	// required to set up this product.
	InfraCategories() []InfraCategory

	// ResolveChoices returns the available options for a choice-type parameter.
	ResolveChoices(ctx context.Context, paramName string) ([]string, error)

	// ValidateSetup validates setup parameters without applying them.
	ValidateSetup(ctx context.Context, params map[string]string) error

	// Setup applies the provided parameters to configure the product.
	Setup(ctx context.Context, params map[string]string) error
}

// ErrSetupNotSupported is returned by Setupable implementations that have
// not yet implemented their setup flow.
var ErrSetupNotSupported = errors.New("setup not yet implemented for this provider")
