package prompt_test

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/setup/framework/prompt"
)

func TestText(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		def      string
		required bool
		want     string
		wantErr  bool
	}{
		{name: "empty returns default", input: "\n", def: "foo", want: "foo"},
		{name: "typed input returned", input: "hello\n", want: "hello"},
		{name: "required re-prompts once then succeeds", input: "\nhello\n", required: true, want: "hello"},
		{name: "newline only equals empty uses default", input: "\n", def: "bar", want: "bar"},
		{name: "not required empty no default returns empty", input: "\n", want: ""},
		{name: "eof with default returns default", input: "", def: "fallback", want: "fallback"},
		{name: "required eof returns error", input: "", required: true, wantErr: true},
		{name: "typed input over default", input: "custom\n", def: "def", want: "custom"},
		{name: "crlf trimmed", input: "hello\r\n", want: "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.NewReader(tc.input)
			var out bytes.Buffer
			got, err := prompt.Text(in, &out, "Enter value", tc.def, tc.required)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBool(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		def     bool
		want    bool
		wantErr bool
	}{
		{name: "Y returns true", input: "Y\n", def: false, want: true},
		{name: "y returns true", input: "y\n", def: false, want: true},
		{name: "yes returns true", input: "yes\n", def: false, want: true},
		{name: "YES returns true", input: "YES\n", def: false, want: true},
		{name: "N returns false", input: "N\n", def: true, want: false},
		{name: "n returns false", input: "n\n", def: true, want: false},
		{name: "no returns false", input: "no\n", def: true, want: false},
		{name: "NO returns false", input: "NO\n", def: true, want: false},
		{name: "empty def true returns true", input: "\n", def: true, want: true},
		{name: "empty def false returns false", input: "\n", def: false, want: false},
		{name: "unrecognized re-prompts then succeeds", input: "maybe\ny\n", def: false, want: true},
		{name: "eof returns default", input: "", def: true, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.NewReader(tc.input)
			var out bytes.Buffer
			got, err := prompt.Bool(in, &out, "Continue?", tc.def)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChoice(t *testing.T) {
	options := []string{"a", "b", "c"}
	cases := []struct {
		name    string
		input   string
		def     string
		want    string
		wantErr bool
	}{
		{name: "valid index returns option", input: "2\n", def: "a", want: "b"},
		{name: "index 1 returns first", input: "1\n", want: "a"},
		{name: "index 3 returns last", input: "3\n", want: "c"},
		{name: "enter returns def", input: "\n", def: "b", want: "b"},
		{name: "out-of-range re-prompts then succeeds", input: "5\n1\n", want: "a"},
		{name: "non-numeric re-prompts then succeeds", input: "x\n3\n", want: "c"},
		{name: "eof no def returns error", input: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.NewReader(tc.input)
			var out bytes.Buffer
			got, err := prompt.Choice(in, &out, "Pick one:", options, tc.def)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMultiChoice(t *testing.T) {
	options := []string{"a", "b", "c", "d"}
	cases := []struct {
		name    string
		input   string
		defs    []string
		want    []string
		wantErr bool
	}{
		{name: "1,3 selects a and c", input: "1,3\n", defs: nil, want: []string{"a", "c"}},
		{name: "empty input returns defs", input: "\n", defs: []string{"b"}, want: []string{"b"}},
		{name: "invalid re-prompts then succeeds", input: "x\n1\n", defs: nil, want: []string{"a"}},
		{name: "out-of-range re-prompts then succeeds", input: "5\n2\n", defs: nil, want: []string{"b"}},
		{name: "empty defs empty input returns nil", input: "\n", defs: nil, want: nil},
		{name: "spaces around numbers", input: " 1 , 3 \n", defs: nil, want: []string{"a", "c"}},
		{name: "single selection", input: "4\n", defs: nil, want: []string{"d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.NewReader(tc.input)
			var out bytes.Buffer
			got, err := prompt.MultiChoice(in, &out, "Pick many:", options, tc.defs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSecret(t *testing.T) {
	t.Run("non-TTY returns error", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		defer r.Close()
		defer w.Close()

		var out bytes.Buffer
		_, err = prompt.Secret(r, &out, "Password")
		if err == nil {
			t.Fatal("expected error for non-TTY input, got nil")
		}
	})
}
