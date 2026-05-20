package format_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/format"
)

// orderedStruct has fields in a deliberately non-alphabetical order to verify
// that NewOrderedYAMLCodec preserves declaration order instead of
// alphabetizing keys (which sigs.k8s.io/yaml.JSONToYAML does).
type orderedStruct struct {
	Zulu  string `json:"zulu"`
	Alpha string `json:"alpha"`
	Mango string `json:"mango"`
	Bravo int    `json:"bravo"`
	Tango bool   `json:"tango"`
}

func TestNewOrderedYAMLCodec_PreservesFieldOrder(t *testing.T) {
	codec := format.NewOrderedYAMLCodec()
	if got := codec.Format(); got != format.YAML {
		t.Fatalf("Format() = %q, want %q", got, format.YAML)
	}

	v := orderedStruct{
		Zulu:  "last-alphabetically",
		Alpha: "first-alphabetically",
		Mango: "middle",
		Bravo: 42,
		Tango: true,
	}

	var buf bytes.Buffer
	if err := codec.Encode(&buf, v); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	out := buf.String()

	// Verify each key appears in the output.
	for _, key := range []string{"zulu", "alpha", "mango", "bravo", "tango"} {
		if !strings.Contains(out, key+":") {
			t.Errorf("output missing key %q:\n%s", key, out)
		}
	}

	// Verify struct-declaration order is preserved: zulu must appear before
	// alpha (i.e., NOT alphabetized).
	zuluIdx := strings.Index(out, "zulu:")
	alphaIdx := strings.Index(out, "alpha:")
	if zuluIdx < 0 || alphaIdx < 0 {
		t.Fatalf("could not find zulu/alpha keys in output:\n%s", out)
	}
	if zuluIdx > alphaIdx {
		t.Errorf("key order was alphabetized: alpha (pos %d) appears before zulu (pos %d); want declaration order\noutput:\n%s",
			alphaIdx, zuluIdx, out)
	}
}

func TestNewOrderedYAMLCodec_RoundTrip(t *testing.T) {
	codec := format.NewOrderedYAMLCodec()

	original := orderedStruct{
		Zulu:  "z-value",
		Alpha: "a-value",
		Mango: "m-value",
		Bravo: 99,
		Tango: true,
	}

	var buf bytes.Buffer
	if err := codec.Encode(&buf, original); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var decoded orderedStruct
	if err := codec.Decode(&buf, &decoded); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", decoded, original)
	}
}
