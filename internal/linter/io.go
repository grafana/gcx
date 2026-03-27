package linter

import (
	"fmt"
	"io/fs"
	"log"
	"strings"

	gbundle "github.com/grafana/gcx/internal/linter/bundle"
	"github.com/open-policy-agent/opa/v1/ast"
	opabundle "github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/loader/filter"
)

// BuiltinBundle contains a bundle with built-in rules and utilities as well as
// the linter's main logic.
//
//nolint:gochecknoglobals
var BuiltinBundle = mustLoadBundleFS(gbundle.BundleFS)

// mustLoadBundleFS implements the same functionality as LoadBundleFS, but logs
// an error on failure and exits.
func mustLoadBundleFS(fs fs.FS) opabundle.Bundle {
	bundle, err := loadBundleFS(fs)
	if err != nil {
		log.Fatal(err)
	}

	return bundle
}

// loadBundleFS loads a bundle from the given filesystem.
// Note: tests are excluded.
func loadBundleFS(fs fs.FS) (opabundle.Bundle, error) {
	embedLoader, err := opabundle.NewFSLoader(fs)
	if err != nil {
		return opabundle.Bundle{}, fmt.Errorf("failed to load bundle from filesystem: %w", err)
	}

	return opabundle.NewCustomReader(embedLoader.WithFilter(excludeTestFilter())).
		WithCapabilities(ast.CapabilitiesForThisVersion()).
		WithSkipBundleVerification(true).
		WithProcessAnnotations(true).
		Read()
}

// excludeTestFilter implements a filter.LoaderFilter that excludes test files.
// ie: files with a `_test.rego` suffix
func excludeTestFilter() filter.LoaderFilter {
	return func(_ string, info fs.FileInfo, _ int) bool {
		return strings.HasSuffix(info.Name(), "_test.rego")
	}
}
