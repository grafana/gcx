package main

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/config"
)

func main() {
	outputDir := "./docs/reference/environment-variables"
	if len(os.Args) > 1 {
		outputDir = os.Args[1]
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatal(err)
	}

	typesCommentsMap, err := buildCommentsMap("internal/config/")
	if err != nil {
		log.Fatal(err)
	}

	envVarMap := make(map[string]string)
	discoverEnvVars(reflect.TypeFor[config.Config](), typesCommentsMap, envVarMap)
	discoverEnvVars(reflect.TypeFor[config.CLIOptions](), typesCommentsMap, envVarMap)

	err = os.WriteFile(filepath.Join(outputDir, "index.md"), toMarkdown(envVarMap), 0600)
	if err != nil {
		log.Fatal(err)
	}
}

func toMarkdown(envVarMap map[string]string) []byte {
	buffer := strings.Builder{}

	vars := make([]string, 0, len(envVarMap))
	for varName := range envVarMap {
		vars = append(vars, varName)
	}
	sort.Strings(vars)

	for i, varName := range vars {
		buffer.WriteString(fmt.Sprintf("## `%s`\n\n", varName))
		buffer.WriteString(envVarMap[varName])

		if i != len(vars)-1 {
			buffer.WriteString("\n\n")
		}
	}

	return fmt.Appendf(nil, "# Environment variables reference\n\n%s\n", buffer.String())
}

type typeComments struct {
	TypeLevel string
	Fields    map[string]string
}

func buildCommentsMap(typesInputDir string) (map[string]typeComments, error) {
	typesComments := make(map[string]typeComments)

	packages, err := parser.ParseDir(token.NewFileSet(), typesInputDir, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	for _, packageAst := range packages {
		packageDocs := doc.New(packageAst, "./", doc.AllDecls)

		for _, typeDecl := range packageDocs.Types {
			typesComments[typeDecl.Name] = typeComments{
				TypeLevel: strings.TrimSpace(typeDecl.Doc),
				Fields:    make(map[string]string),
			}

			if len(typeDecl.Decl.Specs) != 1 {
				continue
			}

			typeSpec, isTypeSpec := typeDecl.Decl.Specs[0].(*ast.TypeSpec)
			if !isTypeSpec {
				continue
			}

			structType, isStructType := typeSpec.Type.(*ast.StructType)
			if !isStructType {
				continue
			}

			for _, field := range structType.Fields.List {
				if len(field.Names) != 1 {
					continue
				}

				typesComments[typeDecl.Name].Fields[field.Names[0].Name] = strings.TrimSpace(field.Doc.Text())
			}
		}
	}

	return typesComments, nil
}

func discoverEnvVars(typeDef reflect.Type, typesCommentsMap map[string]typeComments, envVarMap map[string]string) {
	switch typeDef.Kind() {
	case reflect.Ptr:
		discoverEnvVars(typeDef.Elem(), typesCommentsMap, envVarMap)
	case reflect.Slice:
		discoverEnvVars(typeDef.Elem(), typesCommentsMap, envVarMap)
	case reflect.Map:
		discoverEnvVars(typeDef.Elem(), typesCommentsMap, envVarMap)
	case reflect.Struct:
		comments := typesCommentsMap[typeDef.Name()]

		for field := range typeDef.Fields() {
			fieldKind := field.Type.Kind()
			envName := strings.Split(field.Tag.Get("env"), ",")[0]

			if envName != "" {
				envVarMap[envName] = comments.Fields[field.Name]
			}

			if fieldKind == reflect.Ptr {
				fieldKind = field.Type.Elem().Kind()
			}

			if fieldKind == reflect.Struct || fieldKind == reflect.Map || fieldKind == reflect.Slice {
				discoverEnvVars(field.Type, typesCommentsMap, envVarMap)
			}
		}
	}
}
