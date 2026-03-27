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
	"strings"

	"github.com/grafana/gcx/internal/config"
)

func main() {
	outputDir := "./docs/reference/configuration"
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

	value := reflect.TypeFor[config.Config]()

	markdown := toMarkdown(docs(value, typesCommentsMap))
	err = os.WriteFile(filepath.Join(outputDir, "index.md"), []byte(markdown), 0600)
	if err != nil {
		log.Fatal(err)
	}
}

func toMarkdown(reference string) string {
	return fmt.Sprintf("# Configuration reference\n\n```yaml\n%s\n```\n", reference)
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

func docs(typeDef reflect.Type, typesCommentsMap map[string]typeComments) string {
	switch typeDef.Kind() {
	case reflect.Ptr:
		return docs(typeDef.Elem(), typesCommentsMap)
	case reflect.Bool:
		return "bool"
	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "string"
	case reflect.String:
		return "string"
	case reflect.Slice:
		buffer := strings.Builder{}

		valueType := typeDef.Elem()
		valueKind := valueType.Kind()
		if valueType.Kind() == reflect.Ptr {
			valueKind = valueType.Elem().Kind()
		}

		if valueKind == reflect.Struct || valueKind == reflect.Map {
			buffer.WriteString(
				"-\n" + indent(docs(valueType, typesCommentsMap), 2),
			)
		} else {
			buffer.WriteString("- " + docs(valueType, typesCommentsMap))
		}

		buffer.WriteString("\n- ...\n")

		return buffer.String()
	case reflect.Map:
		buffer := strings.Builder{}

		keyType := typeDef.Key()
		valueType := typeDef.Elem()

		fmt.Fprintf(&buffer, "${%s}:\n", keyType)
		buffer.WriteString(
			indent(docs(valueType, typesCommentsMap), 2),
		)

		return buffer.String()
	case reflect.Struct:
		buffer := strings.Builder{}

		comments := typesCommentsMap[typeDef.Name()]
		if comments.TypeLevel != "" {
			buffer.WriteString(prefixLines(comments.TypeLevel, "# ") + "\n")
		}

		for fieldIndex := range typeDef.NumField() {
			field := typeDef.Field(fieldIndex)
			fieldKind := field.Type.Kind()
			yamlTag := field.Tag.Get("yaml")
			yamlName := strings.Split(yamlTag, ",")[0]
			if yamlName == "" {
				yamlName = field.Name
			}

			if yamlName == "-" {
				continue
			}

			if comments.Fields[field.Name] != "" {
				buffer.WriteString(prefixLines(comments.Fields[field.Name], "# ") + "\n")
			}
			buffer.WriteString(yamlName + ": ")

			if field.Type.Kind() == reflect.Ptr {
				fieldKind = field.Type.Elem().Kind()
			}

			if fieldKind == reflect.Struct || fieldKind == reflect.Map || fieldKind == reflect.Slice {
				buffer.WriteString(
					"\n" + indent(docs(field.Type, typesCommentsMap), 2),
				)
			} else {
				buffer.WriteString(docs(field.Type, typesCommentsMap))
			}

			if fieldIndex != typeDef.NumField()-1 {
				buffer.WriteString("\n")
			}
		}

		return buffer.String()
	}
	return "unknown"
}

func indent(input string, spaces int) string {
	return prefixLines(input, strings.Repeat(" ", spaces))
}

func prefixLines(input string, prefix string) string {
	if input == "" {
		return ""
	}

	return prefix + strings.ReplaceAll(input, "\n", "\n"+prefix)
}
