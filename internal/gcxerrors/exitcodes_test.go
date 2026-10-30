package gcxerrors_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
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
	constants := make(map[string]int)
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fset, file.Name(), nil, 0)
		require.NoError(t, parseErr)
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				require.True(t, ok)
				for i, name := range valueSpec.Names {
					if !strings.HasPrefix(name.Name, "Exit") {
						continue
					}
					message := name.Name + " must use a plain integer literal; use a literal or update this test"
					require.Less(t, i, len(valueSpec.Values), message)
					literal, ok := valueSpec.Values[i].(*ast.BasicLit)
					require.True(t, ok, message)
					require.Equal(t, token.INT, literal.Kind, message)
					code, err := strconv.Atoi(literal.Value)
					require.NoError(t, err, message)
					constants[name.Name] = code
				}
			}
		}
	}

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

	for name, code := range constants {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, 1, counts[code], "%s must appear exactly once in ExitCodes", name)
		})
	}
	assert.NotEmpty(t, constants, "must find exit code constants")
	assert.Len(t, entries, len(constants), "table must contain only declared exit codes")
}
