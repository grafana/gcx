package builtins

import (
	"encoding/json"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/tester"
	"github.com/open-policy-agent/opa/v1/topdown/print"
	"github.com/open-policy-agent/opa/v1/types"
)

func All() []func(*rego.Rego) {
	return nil
}

func Tester() []*tester.Builtin {
	return []*tester.Builtin{
		{
			Decl: &ast.Builtin{
				Name: assertReportsMatchMeta.Name,
				Decl: assertReportsMatchMeta.Decl,
			},
			Func: assertReportsMatch(),
		},
	}
}

//nolint:gochecknoglobals
var assertReportsMatchMeta = &rego.Function{
	Name: "assert_reports_match",
	Decl: types.NewFunction(
		types.Args(
			types.SetOfAny,
			types.SetOfAny,
		),
		types.Boolean{},
	),
}

func assertReportsMatch() func(*rego.Rego) {
	return rego.Function2(
		assertReportsMatchMeta,
		func(ctx rego.BuiltinContext, actual *ast.Term, expected *ast.Term) (*ast.Term, error) {
			var actualList []map[string]any
			var expectedList []map[string]any

			//nolint:forcetypeassert
			err := actual.Value.(ast.Set).Iter(func(term *ast.Term) error {
				item := map[string]any{}
				if err := json.Unmarshal([]byte(term.String()), &item); err != nil {
					return err
				}

				actualList = append(actualList, item)

				return nil
			})
			if err != nil {
				return nil, err
			}

			//nolint:forcetypeassert
			err = expected.Value.(ast.Set).Iter(func(term *ast.Term) error {
				item := map[string]any{}
				if err := json.Unmarshal([]byte(term.String()), &item); err != nil {
					return err
				}

				expectedList = append(expectedList, item)

				return nil
			})
			if err != nil {
				return nil, err
			}

			diff := cmp.Diff(actualList, expectedList)
			if diff == "" {
				return ast.BooleanTerm(true), nil
			}

			printCtx := print.Context{
				Context:  ctx.Context,
				Location: actual.Location,
			}
			if err := ctx.PrintHook.Print(printCtx, diff); err != nil {
				return nil, err
			}

			return ast.BooleanTerm(false), nil
		},
	)
}
