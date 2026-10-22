package config

import (
	"errors"
	"fmt"
	"os"
)

// contextSelection records how a load chose its target context. Strict context
// mode (GCX_REQUIRE_CONTEXT) refuses a load whose target came from the shared
// current-context field of the config file.
type contextSelection int

const (
	// selectionUndeclared means no override stated how the load chose the context.
	selectionUndeclared contextSelection = iota
	// selectionExplicit means the invocation named a context.
	selectionExplicit
	// selectionInherited means the invocation named none and the config file
	// has a non-empty current-context.
	selectionInherited
	// selectionNone means the invocation named none and the config file has no
	// current-context, so the load inherited nothing.
	selectionNone
)

// ErrContextRequired is the sentinel behind ContextRequiredError.
var ErrContextRequired = errors.New("context selection required")

// ErrContextSelectionUndeclared is the error in strict context mode when a load
// declared neither a context selection nor FileScope. It shows a missing
// call-site declaration, not a user mistake, so it fails closed.
var ErrContextSelectionUndeclared = errors.New("internal error: config loaded without a declared context selection, so strict context mode refuses it")

// ContextRequiredError reports that strict context mode refused an invocation
// that did not name a context. Inherited is the current-context that the config
// file would have supplied. Available lists the configured contexts.
type ContextRequiredError struct {
	Inherited string
	Available []string
	// Command is "login" or "signup" when that command raised the refusal. Both
	// take their context from the positional argument or --context and ignore
	// GCX_CONTEXT. It is empty for every other command.
	Command string
}

func (e *ContextRequiredError) Error() string {
	return fmt.Sprintf("strict context mode: the command does not name a context (current-context is %q)", e.Inherited)
}

func (e *ContextRequiredError) Is(target error) bool {
	return target == ErrContextRequired
}

// StrictContextEnabled reports whether GCX_REQUIRE_CONTEXT turns on strict
// context mode. It parses the variable the same way as CLIOptions.
func StrictContextEnabled() bool {
	return CLIOptions{RequireContext: os.Getenv(RequireContextEnvVar)}.RequireContextEnabled()
}

// SelectContext returns an Override that declares the context selection of the
// invocation. A non-empty name selects that context and counts as explicit. It
// fails with ContextNotFound when the context does not exist. An empty name
// leaves the current context as it is. The Override records it as inherited
// when the config has a current-context.
func SelectContext(name string) Override {
	return func(cfg *Config) error {
		if name == "" {
			// A name from an earlier override in the chain stays explicit.
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

// FileScope is an Override for loads that inspect the config file itself, not
// the target of a context. Strict context mode does not apply to them.
//
// Apply it before any override that can fail (SelectContext with a name, env
// parsing). The loader stops at the first failing override. A FileScope after
// such an override never runs, so the load counts as unscoped and inherited.
func FileScope(cfg *Config) error {
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

// inheritedRefusal returns the strict context mode refusal for a load that
// failed after it recorded an inherited selection, or nil. It never reports an
// undeclared selection, because a load that failed early may not have recorded
// one yet.
func inheritedRefusal(cfg *Config) error {
	if cfg.fileScoped || cfg.contextSelection != selectionInherited {
		return nil
	}
	return &ContextRequiredError{Inherited: cfg.CurrentContext, Available: cfg.ContextNames()}
}

// ResolveContextName picks the context to use. It returns the explicit override
// when set. Otherwise it follows the selection that the load recorded.
//
// An explicit or empty selection returns the current context, or the default
// when that is empty. Strict context mode does not refuse these, because the
// file did not supply the name. An inherited selection is refused in strict
// mode. That load already failed, so this is a second check. A file-scoped
// config is never refused.
//
// A config without a recorded selection comes from a raw Load call. For it,
// strict context mode refuses a non-empty current context. Outside strict mode,
// all cases return the current context, or the default when that is empty.
func ResolveContextName(override string, cfg Config) (string, error) {
	if override != "" {
		return override, nil
	}
	if StrictContextEnabled() && !cfg.fileScoped {
		switch cfg.contextSelection {
		case selectionExplicit, selectionNone:
		case selectionInherited:
			return "", &ContextRequiredError{Inherited: cfg.CurrentContext, Available: cfg.ContextNames()}
		case selectionUndeclared:
			if cfg.CurrentContext != "" {
				return "", &ContextRequiredError{Inherited: cfg.CurrentContext, Available: cfg.ContextNames()}
			}
		}
	}
	if cfg.CurrentContext != "" {
		return cfg.CurrentContext, nil
	}
	return DefaultContextName, nil
}
