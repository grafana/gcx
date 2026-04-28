// Package linterr contains sentinel errors for the linter package.
// It exists so that packages like cmd/gcx/fail can check for linter
// errors without importing the full linter (which pulls in heavy
// dependencies like Loki via builtins).
package linterr

import "errors"

// ErrTestsFailed is returned when one or more linter tests fail.
var ErrTestsFailed = errors.New("tests failed")
