package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"sigs.k8s.io/yaml"
)

// ReadFileOrStdin decodes a JSON/YAML object from a file path or stdin (when
// file == "-") into the provided target. Used by provider commands that post
// bare objects to a product REST API.
//
// The target is the bare object, but the manifests that gcx itself writes —
// `resources list-examples`, `pull`, and the K8s envelope that provider list
// commands emit — wrap that object in an apiVersion/kind/metadata/spec
// envelope. Decoding such an envelope into the bare target matches no field
// and silently produces an empty object, so unwrap the envelope first. See
// issue #1185.
func ReadFileOrStdin(file string, stdin io.Reader, out any) error {
	if file == "" {
		return errors.New("--filename is required (use - to read from stdin)")
	}

	var reader io.Reader
	if file == "-" {
		reader = stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", file, err)
		}
		defer f.Close()
		reader = f
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return errors.New("input is empty")
	}

	spec, err := envelopeSpec(data, targetKind(out))
	if err != nil {
		return fmt.Errorf("%s: %w", inputName(file), err)
	}
	if spec != nil {
		data = spec
	}

	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to parse input: %w", err)
	}
	return nil
}

// targetKind names the resource kind that the target type accepts. Each
// envelope that gcx prints carries the name of the Go type behind the target
// under "kind", so the name of that type is the expected kind. targetKind
// returns an empty string for a target that has no type name, and envelopeSpec
// then skips the kind check.
func targetKind(out any) string {
	t := reflect.TypeOf(out)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}
	return t.Name()
}

// inputName names the source of the document for an error message.
func inputName(file string) string {
	if file == "-" {
		return "stdin"
	}
	return file
}

// envelopeSpec returns the spec of a K8s-style resource envelope. It returns a
// nil slice when the document is not an envelope, so a bare object passes
// through unchanged. A document counts as an envelope when it carries
// "apiVersion" or "kind"; none of the bare objects that the provider commands
// post declares either name.
//
// envelopeSpec reports an error for three documents that define no resource
// for the target: a Grafana alert provisioning file, an envelope of another
// kind, and an envelope without an object-valued spec.
func envelopeSpec(data []byte, wantKind string) ([]byte, error) {
	// A document that does not decode as an object leaves doc nil, so the key
	// probes below classify it as a non-envelope. The caller then decodes it
	// against the target type, and reports the syntax error with that context.
	var doc map[string]json.RawMessage
	_ = yaml.Unmarshal(data, &doc)

	rawAPIVersion, hasAPIVersion := doc["apiVersion"]
	rawKind, hasKind := doc["kind"]
	if !hasAPIVersion && !hasKind {
		return nil, nil
	}

	// Grafana writes an alert provisioning file with a number under
	// "apiVersion", it declares no kind, and it groups the resources under a
	// plural key such as "contactPoints". A K8s envelope always holds a
	// "group/version" string under "apiVersion".
	if !hasKind && isJSONNumber(rawAPIVersion) {
		return nil, fmt.Errorf(
			"the document is a Grafana alert provisioning file, because apiVersion holds the number %s and the document declares no kind; gcx reads one resource object, and it does not read that file format",
			strings.TrimSpace(string(rawAPIVersion)))
	}

	var kind string
	_ = json.Unmarshal(rawKind, &kind)
	if kind != "" && wantKind != "" && !strings.EqualFold(kind, wantKind) {
		return nil, fmt.Errorf("the document declares kind %q, but this command reads a %q", kind, wantKind)
	}

	// A missing "spec" decodes as an empty slice, which fails the same way a
	// null, scalar, or list spec does.
	spec := doc["spec"]
	var specObject map[string]json.RawMessage
	if err := json.Unmarshal(spec, &specObject); err != nil || len(specObject) == 0 {
		return nil, errors.New("the document sets apiVersion or kind, but it carries no object-valued spec field, so it defines no resource")
	}
	return spec, nil
}

// isJSONNumber reports whether the raw value is a JSON number.
func isJSONNumber(raw json.RawMessage) bool {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	_, ok := value.(float64)
	return ok
}
