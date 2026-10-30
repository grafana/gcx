package fail_test

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	gcxerrorsImportPath = "github.com/grafana/gcx/internal/gcxerrors"
	summariesSource     = "../../../internal/gcxerrors/summaries.go"
	errorsDesignDoc     = "../../../docs/design/errors.md"
)

// toDetailedError converts err and checks that a converter-built summary is in
// the vocabulary. A chain that already carries a DetailedError passes through
// ErrorToDetailedError with the summary its producer chose, which is outside
// the cmd/gcx/fail policy, so it is not checked.
func toDetailedError(t *testing.T, err error) *gcxerrors.DetailedError {
	t.Helper()

	got := fail.ErrorToDetailedError(err)
	if got != nil && !carriesDetailedError(err) {
		assert.Contains(t, gcxerrors.Summaries(), got.Summary, "summary %q is not in the vocabulary", got.Summary)
	}
	return got
}

func carriesDetailedError(err error) bool {
	var val gcxerrors.DetailedError
	var ptr *gcxerrors.DetailedError
	return errors.As(err, &val) || errors.As(err, &ptr)
}

// TestConverterSummariesUseVocabulary parses the non-test sources of
// cmd/gcx/fail and fails on any summary that is not a gcxerrors Summary
// constant, so a literal or formatted summary cannot reach users.
func TestConverterSummariesUseVocabulary(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)
		files = append(files, file)
	}
	require.NotEmpty(t, files)

	if violations := summaryViolations(fset, files, vocabularyConstantNames(t)); len(violations) > 0 {
		t.Errorf("summaries must be gcxerrors Summary constants (see %s):\n%s", errorsDesignDoc, strings.Join(violations, "\n"))
	}
}

// TestSummaryViolations proves the static check rejects the summary forms it
// exists to stop, and accepts the ones converters use.
func TestSummaryViolations(t *testing.T) {
	const src = `package fail

import (
	"fmt"

	gx "github.com/grafana/gcx/internal/gcxerrors"
)

func good(inner *gx.DetailedError) *gx.DetailedError {
	d := &gx.DetailedError{Summary: gx.SummaryAPIError}
	d.Summary = gx.SummaryResourceNotFound
	d.Summary = pick(1)
	copied := *inner
	return &copied
}

func pick(code int) string {
	_ = func() string { return "a nested function's returns are its own" }
	if code == 1 {
		return gx.SummaryAuthenticationFailed
	}
	return gx.SummaryAuthorizationFailed
}

func mixed(code int) string {
	if code == 1 {
		return gx.SummaryAPIError
	}
	return "Something else"
}

func bad(name string, d *gx.DetailedError) {
	_ = gx.DetailedError{Summary: "Literal summary"}
	_ = gx.DetailedError{Summary: fmt.Sprintf("%s failed", name)}
	_ = gx.DetailedError{Summary: gx.SummaryAPIError + ": " + name}
	_ = gx.DetailedError{Summary: name}
	_ = gx.DetailedError{Summary: gx.SummaryNotInVocabulary}
	_ = gx.DetailedError{Summary: mixed(1)}
	_ = gx.DetailedError{Details: "no summary"}
	d.Summary = "Assigned literal"
	d.Summary += "suffix"
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, 0)
	require.NoError(t, err)

	got := summaryViolations(fset, []*ast.File{file}, vocabularyConstantNames(t))

	want := []string{
		"sample.go:33:", // literal
		"sample.go:34:", // fmt.Sprintf
		"sample.go:35:", // concatenation
		"sample.go:36:", // variable
		"sample.go:37:", // unknown constant
		"sample.go:38:", // helper with a non-constant return
		"sample.go:39:", // DetailedError without a summary
		"sample.go:40:", // assigned literal
		"sample.go:41:", // compound assignment
	}
	require.Len(t, got, len(want), strings.Join(got, "\n"))
	for i, prefix := range want {
		assert.True(t, strings.HasPrefix(got[i], prefix), "violation %d = %q, want prefix %q", i, got[i], prefix)
	}
}

// TestSummaryVocabularyMatchesDesignDoc keeps the gcxerrors constants, the
// Summaries list and the "Summary vocabulary" table in docs/design/errors.md
// identical.
func TestSummaryVocabularyMatchesDesignDoc(t *testing.T) {
	summaries := gcxerrors.Summaries()
	assert.Empty(t, duplicates(summaries), "gcxerrors.Summaries() lists a summary twice")

	constants := slices.Collect(maps.Values(vocabularyConstants(t)))
	missing, extra := setDiff(constants, summaries)
	assert.Empty(t, missing, "Summary constants missing from gcxerrors.Summaries()")
	assert.Empty(t, extra, "gcxerrors.Summaries() entries with no Summary constant")

	table := designDocSummaries(t)
	assert.Empty(t, duplicates(table), "%s lists a summary twice", errorsDesignDoc)
	missing, extra = setDiff(summaries, table)
	assert.Empty(t, missing, "summaries in gcxerrors but not in the %s table", errorsDesignDoc)
	assert.Empty(t, extra, "summaries in the %s table but not in gcxerrors", errorsDesignDoc)
}

// summaryViolations reports, as "file:line:col: message", every Summary field
// or assignment in files that is not a vocabulary constant or a call to a
// package-level helper whose every return is one. A DetailedError literal
// that sets fields but no Summary is reported too.
func summaryViolations(fset *token.FileSet, files []*ast.File, allowed map[string]bool) []string {
	c := &summaryChecker{fset: fset, allowed: allowed, helpers: map[string]summaryHelper{}}
	for _, file := range files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
				c.helpers[fn.Name.Name] = summaryHelper{decl: fn, gcxRef: gcxerrorsName(file)}
			}
		}
	}

	for _, file := range files {
		gcxRef := gcxerrorsName(file)
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				c.checkCompositeLit(n, gcxRef)
			case *ast.AssignStmt:
				c.checkAssign(n, gcxRef)
			}
			return true
		})
	}

	sort.SliceStable(c.violations, func(i, j int) bool {
		return lessPosition(c.violations[i], c.violations[j])
	})
	return c.violations
}

type summaryHelper struct {
	decl   *ast.FuncDecl
	gcxRef string
}

type summaryChecker struct {
	fset       *token.FileSet
	allowed    map[string]bool
	helpers    map[string]summaryHelper
	violations []string
}

func (c *summaryChecker) reportf(node ast.Node, format string, args ...any) {
	c.violations = append(c.violations, fmt.Sprintf("%s: %s", c.fset.Position(node.Pos()), fmt.Sprintf(format, args...)))
}

func (c *summaryChecker) checkCompositeLit(lit *ast.CompositeLit, gcxRef string) {
	hasSummary := false
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Summary" {
			hasSummary = true
			if !c.validSummary(kv.Value, gcxRef) {
				c.reportf(kv.Value, "Summary %s is not a gcxerrors Summary constant", exprString(kv.Value))
			}
		}
	}
	if !hasSummary && len(lit.Elts) > 0 && isDetailedErrorType(lit.Type, gcxRef) {
		c.reportf(lit, "DetailedError literal sets no Summary")
	}
}

func (c *summaryChecker) checkAssign(assign *ast.AssignStmt, gcxRef string) {
	for i, lhs := range assign.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Summary" {
			continue
		}
		if assign.Tok != token.ASSIGN || len(assign.Rhs) != len(assign.Lhs) {
			c.reportf(assign, "Summary must be assigned a gcxerrors Summary constant with =")
			continue
		}
		if !c.validSummary(assign.Rhs[i], gcxRef) {
			c.reportf(assign.Rhs[i], "Summary %s is not a gcxerrors Summary constant", exprString(assign.Rhs[i]))
		}
	}
}

func (c *summaryChecker) validSummary(expr ast.Expr, gcxRef string) bool {
	if c.isConstant(expr, gcxRef) {
		return true
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	return ok && c.helperReturnsConstants(fn.Name)
}

func (c *summaryChecker) isConstant(expr ast.Expr, gcxRef string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && gcxRef != "" && pkg.Name == gcxRef && c.allowed[sel.Sel.Name]
}

func (c *summaryChecker) helperReturnsConstants(name string) bool {
	h, ok := c.helpers[name]
	if !ok {
		return false
	}
	returns := 0
	allConstant := true
	ast.Inspect(h.decl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // a nested function's returns are its own
		case *ast.ReturnStmt:
			returns++
			if len(n.Results) != 1 || !c.isConstant(n.Results[0], h.gcxRef) {
				allConstant = false
			}
		}
		return true
	})
	return returns > 0 && allConstant
}

func isDetailedErrorType(expr ast.Expr, gcxRef string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == gcxRef && sel.Sel.Name == "DetailedError"
}

// gcxerrorsName returns the name a file uses for the gcxerrors package, or ""
// when the file does not import it.
func gcxerrorsName(file *ast.File) string {
	for _, imp := range file.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err != nil || path != gcxerrorsImportPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "gcxerrors"
	}
	return ""
}

// exprString renders expr briefly for violation messages.
func exprString(expr ast.Expr) string {
	var b strings.Builder
	printExpr(&b, expr)
	return b.String()
}

func printExpr(b *strings.Builder, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		b.WriteString(e.Value)
	case *ast.Ident:
		b.WriteString(e.Name)
	case *ast.SelectorExpr:
		printExpr(b, e.X)
		b.WriteString("." + e.Sel.Name)
	case *ast.CallExpr:
		printExpr(b, e.Fun)
		b.WriteString("(...)")
	case *ast.BinaryExpr:
		printExpr(b, e.X)
		b.WriteString(" " + e.Op.String() + " ")
		printExpr(b, e.Y)
	default:
		fmt.Fprintf(b, "%T", expr)
	}
}

// lessPosition orders "file:line:col: message" strings by file, then line.
func lessPosition(a, b string) bool {
	fileA, lineA := splitPosition(a)
	fileB, lineB := splitPosition(b)
	if fileA != fileB {
		return fileA < fileB
	}
	return lineA < lineB
}

func splitPosition(violation string) (string, int) {
	parts := strings.SplitN(violation, ":", 3)
	if len(parts) < 2 {
		return violation, 0
	}
	line, _ := strconv.Atoi(parts[1])
	return parts[0], line
}

// vocabularyConstants parses internal/gcxerrors/summaries.go and returns the
// value of each exported Summary constant by name.
func vocabularyConstants(t *testing.T) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), summariesSource, nil, 0)
	require.NoError(t, err)

	constants := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "Summary") || i >= len(value.Values) {
					continue
				}
				lit, ok := value.Values[i].(*ast.BasicLit)
				require.True(t, ok, "%s must be a string literal", name.Name)
				text, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				constants[name.Name] = text
			}
		}
	}
	require.NotEmpty(t, constants, "no Summary constants found in %s", summariesSource)
	return constants
}

func vocabularyConstantNames(t *testing.T) map[string]bool {
	t.Helper()

	names := map[string]bool{}
	for name := range vocabularyConstants(t) {
		names[name] = true
	}
	return names
}

// designDocSummaries returns the first-column values of the table under the
// "## Summary vocabulary" heading in docs/design/errors.md, in order.
func designDocSummaries(t *testing.T) []string {
	t.Helper()

	f, err := os.Open(errorsDesignDoc)
	require.NoError(t, err)
	defer f.Close()

	var summaries []string
	inSection, inTable := false, false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			inSection = line == "## Summary vocabulary"
			continue
		}
		if !inSection {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			if inTable {
				break
			}
			continue
		}
		inTable = true
		cells := strings.Split(strings.Trim(line, "|"), "|")
		first := strings.TrimSpace(cells[0])
		if first == "Summary" || strings.HasPrefix(first, "---") {
			continue
		}
		require.True(t, strings.HasPrefix(first, "`") && strings.HasSuffix(first, "`") && len(first) > 2,
			"vocabulary row %q must start with a backticked summary", line)
		summaries = append(summaries, strings.Trim(first, "`"))
	}
	require.NoError(t, scanner.Err())
	require.NotEmpty(t, summaries, "no summary vocabulary table found in %s", errorsDesignDoc)
	return summaries
}

// setDiff returns the values in want missing from got, and the values in got
// that are not in want, both sorted.
func setDiff(want, got []string) ([]string, []string) {
	var missing, extra []string
	for _, v := range want {
		if !slices.Contains(got, v) {
			missing = append(missing, v)
		}
	}
	for _, v := range got {
		if !slices.Contains(want, v) {
			extra = append(extra, v)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func duplicates(values []string) []string {
	seen := map[string]bool{}
	var dups []string
	for _, v := range values {
		if seen[v] {
			dups = append(dups, v)
		}
		seen[v] = true
	}
	return dups
}
