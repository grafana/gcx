package output_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
)

func TestStatusSymbols_localeSelectsUnicodeOrASCII(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want [4]string // success, warning, error, info
	}{
		{name: "nothing set assumes UTF-8", env: map[string]string{}, want: [4]string{"✔", "⚠", "✘", "ℹ"}},
		{name: "LANG UTF-8", env: map[string]string{"LANG": "en_US.UTF-8"}, want: [4]string{"✔", "⚠", "✘", "ℹ"}},
		{name: "utf8 spelling", env: map[string]string{"LANG": "C.utf8"}, want: [4]string{"✔", "⚠", "✘", "ℹ"}},
		{name: "LANG C", env: map[string]string{"LANG": "C"}, want: [4]string{"[ok]", "[!]", "[x]", "[i]"}},
		{name: "LANG POSIX", env: map[string]string{"LANG": "POSIX"}, want: [4]string{"[ok]", "[!]", "[x]", "[i]"}},
		{name: "legacy charset", env: map[string]string{"LANG": "en_US.ISO-8859-1"}, want: [4]string{"[ok]", "[!]", "[x]", "[i]"}},
		{name: "LC_ALL overrides LANG", env: map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, want: [4]string{"[ok]", "[!]", "[x]", "[i]"}},
		{name: "LC_CTYPE overrides LANG", env: map[string]string{"LC_CTYPE": "en_US.UTF-8", "LANG": "C"}, want: [4]string{"✔", "⚠", "✘", "ℹ"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
				t.Setenv(name, tc.env[name])
			}

			assert.Equal(t, tc.want[0], output.SuccessSymbol())
			assert.Equal(t, tc.want[1], output.WarningSymbol())
			assert.Equal(t, tc.want[2], output.ErrorSymbol())
			assert.Equal(t, tc.want[3], output.InfoSymbol())
		})
	}
}

func TestMessages_useASCIIFallback(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_CTYPE", "")
	t.Setenv("LANG", "")

	var out bytes.Buffer
	output.Success(&out, "done %d", 1)
	output.Warning(&out, "careful")
	output.Error(&out, "failed")
	output.Info(&out, "note")

	assert.Equal(t, "[ok] done 1\n[!] careful\n[x] failed\n[i] note\n", out.String())
}
