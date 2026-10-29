package gcxerrors_test

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitCodesMatchConstants(t *testing.T) {
	fset := token.NewFileSet()
	files, err := os.ReadDir(".")
	require.NoError(t, err)
	// Type-check all constants without importing unrelated error helpers.
	constantsFile := &ast.File{Name: ast.NewIdent("gcxerrors")}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fset, file.Name(), nil, 0)
		require.NoError(t, parseErr)
		for _, decl := range parsed.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
				constantsFile.Decls = append(constantsFile.Decls, gen)
			}
		}
	}
	var config types.Config
	pkg, err := config.Check("gcxerrors", fset, []*ast.File{constantsFile}, nil)
	require.NoError(t, err)

	entries := gcxerrors.ExitCodes()
	counts := make(map[int]int, len(entries))
	for i, info := range entries {
		counts[info.Code]++
		t.Run(info.Name, func(t *testing.T) {
			assert.NotEmpty(t, info.Name)
			assert.NotEmpty(t, info.Description)
			assert.NotEmpty(t, info.Example)
			if i > 0 {
				assert.Greater(t, info.Code, entries[i-1].Code, "exit codes must be ordered")
			}
		})
	}

	constants := 0
	for _, name := range pkg.Scope().Names() {
		value, ok := pkg.Scope().Lookup(name).(*types.Const)
		if !ok || !strings.HasPrefix(name, "Exit") {
			continue
		}
		constants++
		t.Run(name, func(t *testing.T) {
			code, valid := constant.Int64Val(value.Val())
			require.True(t, valid, "exit code must be an integer")
			assert.Equal(t, 1, counts[int(code)], "%s must appear exactly once in ExitCodes", name)
		})
	}
	assert.Positive(t, constants, "must find exit code constants")
	assert.Len(t, entries, constants, "table must contain only declared exit codes")
}
