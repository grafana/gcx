package server

import (
	"bytes"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"strings"
)

//nolint:gochecknoglobals
var templates *template.Template

//go:embed embed/templates/*/*.tmpl embed/assets/*
var embedFS embed.FS

//nolint:gochecknoinits
func init() {
	base := template.New("gcx").
		Option("missingkey=error").
		Funcs(template.FuncMap{
			"kindHasProxy": func(_ string) bool {
				panic("kindHasProxy is not implemented")
			},
		})

	templates = template.Must(findAndParseTemplates(embedFS, base, "embed/templates"))
}

func findAndParseTemplates(vfs fs.FS, rootTmpl *template.Template, rootDir string) (*template.Template, error) {
	err := fs.WalkDir(vfs, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		fileHandle, err := vfs.Open(path)
		if err != nil {
			return err
		}

		contents, err := io.ReadAll(fileHandle)
		if err != nil {
			return err
		}

		templateName := strings.TrimPrefix(strings.TrimPrefix(path, rootDir), "/")
		t := rootTmpl.New(templateName)
		_, err = t.Parse(string(contents))

		return err
	})

	return rootTmpl, err
}

func renderTemplate(out io.Writer, name string, data any) error {
	var buffer bytes.Buffer

	// Render to a buffer first, to avoid writing partial templates in `out` in
	// case of error.
	if err := templates.ExecuteTemplate(&buffer, name, data); err != nil {
		return err
	}

	_, err := out.Write(buffer.Bytes())
	return err
}
