//go:build wasip1

package clipboard

import "errors"

var errUnsupported = errors.New("clipboard: unsupported on wasip1")

func readAll() (string, error) { return "", errUnsupported }

func writeAll(string) error { return errUnsupported }
