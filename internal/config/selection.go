package config

import (
	"errors"
	"fmt"
	"os"
)

// contextSelection records how a load decided which context it targets. Strict
// context mode (GCX_REQUIRE_CONTEXT) refuses a load whose target came from the
// shared current-context field of the config file.
type contextSelection int

const (
	// selectionUndeclared means no override stated how the context was chosen.
	selectionUndeclared contextSelection = iota
	// selectionExplicit means the invocation named a context.
	selectionExplicit
	// selectionInherited means the invocation named none and the config file
	// supplied a non-empty current-context.
	selectionInherited
	// selectionNone means the invocation named none and the config file has no
	// current-context, so nothing was inherited.
	selectionNone
)

// ErrContextRequired is the sentinel behind ContextRequiredError.
var ErrContextRequired = errors.New("context selection required")

// ErrContextSelectionUndeclared is returned in strict context mode when a load
// declared neither a context selection nor FileScope. It signals a missing
// call-site declaration, not a user mistake, so it fails closed.
var ErrContextSelectionUndeclared = errors.New("internal error: config loaded without declaring a context selection; strict context mode refuses it")

// ContextRequiredError reports that strict context mode refused an invocation
// that did not name a context. Inherited is the current-context the config file
// would have supplied; Available lists the configured contexts.
type ContextRequiredError struct {
	Inherited string
	Available []string
}

func (e *ContextRequiredError) Error() string {
	return fmt.Sprintf("strict context mode: invocation does not name a context (current-context is %q)", e.Inherited)
}

func (e *ContextRequiredError) Is(target error) bool {
	return target == ErrContextRequired
}

// StrictContextEnabled reports whether GCX_REQUIRE_CONTEXT turns strict context
// mode on, using the same parsing as CLIOptions.
func StrictContextEnabled() bool {
	return CLIOptions{RequireContext: os.Getenv("GCX_REQUIRE_CONTEXT")}.RequireContextEnabled()
}

// SelectContext returns an Override that declares the invocation's context
// selection. A non-empty name selects that context (failing with
// ContextNotFound when it does not exist) and counts as explicit. An empty name
// leaves the current context alone and records it as inherited when the config
// supplies one.
func SelectContext(name string) Override {
	return func(cfg *Config) error {
		if name == "" {
			// A name given earlier in the override chain stays explicit.
			if cfg.contextSelection == selectionUndeclared {
				if cfg.CurrentContext != "" {
					cfg.contextSelection = selectionInherited
				} else {
					cfg.contextSelection = selectionNone
				}
			}
			return nil
		}
		if !cfg.HasContext(name) {
			return ContextNotFound(name, cfg.ContextNames())
		}
		cfg.CurrentContext = name
		cfg.contextSelection = selectionExplicit
		return nil
	}
}

// FileScope is an Override for loads that inspect the config file itself rather
// than a context's target, so strict context mode does not apply to them.
//
//nolint:gochecknoglobals // Stateless Override value used like a constant at call sites.
var FileScope Override = func(cfg *Config) error {
	cfg.fileScoped = true
	return nil
}

// checkContextSelection applies strict context mode to a loaded config.
func checkContextSelection(cfg *Config) error {
	if cfg.fileScoped {
		return nil
	}
	switch cfg.contextSelection {
	case selectionUndeclared:
		return ErrContextSelectionUndeclared
	case selectionInherited:
		return &ContextRequiredError{Inherited: cfg.CurrentContext, Available: cfg.ContextNames()}
	case selectionExplicit, selectionNone:
		return nil
	}
	return nil
}

// inheritedRefusal returns the strict-mode refusal for a load that failed after
// recording an inherited selection, or nil. It never reports an undeclared
// selection: a load that failed early may simply not have recorded one yet.
func inheritedRefusal(cfg *Config) error {
	if cfg.fileScoped || cfg.contextSelection != selectionInherited {
		return nil
	}
	return &ContextRequiredError{Inherited: cfg.CurrentContext, Available: cfg.ContextNames()}
}

// ResolveContextName picks the context to operate on: the explicit override when
// set, otherwise the config's current context, falling back to the default. In
// strict context mode, falling back to a non-empty current context is refused.
func ResolveContextName(override string, cfg Config) (string, error) {
	if override != "" {
		return override, nil
	}
	if cfg.CurrentContext != "" {
		if StrictContextEnabled() {
			return "", &ContextRequiredError{Inherited: cfg.CurrentContext, Available: cfg.ContextNames()}
		}
		return cfg.CurrentContext, nil
	}
	return DefaultContextName, nil
}
