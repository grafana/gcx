// Package linterr holds the linter's sentinel errors, so error reporting can
// recognise them without importing the linter and its policy engine.
package linterr

import "errors"

// ErrTestsFailed is returned when linter rule tests fail.
var ErrTestsFailed = errors.New("tests failed")
