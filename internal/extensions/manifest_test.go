package extensions_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/extensions"
)

func TestParseManifest(t *testing.T) {
	t.Parallel()

	base := `apiVersion: extensions.gcx.grafana.com/v1alpha1
kind: Extension
metadata:
  name: azure-datasources
spec:
  version: 0.1.0
`

	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "valid platform table",
			yaml: base + `  platforms:
    - os: linux
      arch: amd64
      url: https://example.com/x.tar.gz
      sha256: abc
`,
		},
		{
			name: "valid script extension",
			yaml: base + "  script: ./run.sh\n",
		},
		{
			name: "wrong apiVersion",
			yaml: strings.Replace(base, "extensions.gcx.grafana.com/v1alpha1", "v1", 1) +
				"  script: ./run.sh\n",
			wantErr: "unsupported apiVersion",
		},
		{
			name:    "name with uppercase",
			yaml:    strings.Replace(base, "azure-datasources", "AzureDatasources", 1) + "  script: ./run.sh\n",
			wantErr: "invalid metadata.name",
		},
		{
			name:    "neither script nor platforms",
			yaml:    base,
			wantErr: "either script or platforms",
		},
		{
			name:    "script and platforms together",
			yaml:    base + "  script: ./run.sh\n  platforms:\n    - os: linux\n      arch: amd64\n      path: ./bin\n",
			wantErr: "mutually exclusive",
		},
		{
			name:    "url without checksum fails closed",
			yaml:    base + "  platforms:\n    - os: linux\n      arch: amd64\n      url: https://example.com/x\n",
			wantErr: "sha256 is required",
		},
		{
			name:    "wildcard platform needs a local path",
			yaml:    base + "  platforms:\n    - os: \"*\"\n      arch: \"*\"\n      url: https://example.com/x\n      sha256: abc\n",
			wantErr: "only valid on a path row",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := extensions.ParseManifest([]byte(tt.yaml))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSelectPlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest extensions.Manifest
		wantErr  bool
	}{
		{
			name: "exact match wins over wildcard",
			manifest: extensions.Manifest{Spec: extensions.Spec{Platforms: []extensions.Platform{
				{OS: "*", Arch: "*", Path: "./local"},
				{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: "https://example.com/exact", SHA256: "abc"},
			}}},
		},
		{
			name: "wildcard matches any host",
			manifest: extensions.Manifest{Spec: extensions.Spec{Platforms: []extensions.Platform{
				{OS: "*", Arch: "*", Path: "./local"},
			}}},
		},
		{
			name:     "script extension has no platform table to select from",
			manifest: extensions.Manifest{Spec: extensions.Spec{Script: "./run.sh"}},
			wantErr:  true,
		},
		{
			name: "no row for this platform",
			manifest: extensions.Manifest{
				Metadata: extensions.Metadata{Name: "x"},
				Spec:     extensions.Spec{Platforms: []extensions.Platform{{OS: "plan9", Arch: "mips", URL: "u", SHA256: "s"}}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := tt.manifest.SelectPlatform()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p == nil {
				t.Fatal("expected a platform row")
			}
		})
	}
}

func TestReportUsageDefaultsOn(t *testing.T) {
	t.Parallel()

	if !(extensions.Spec{}).ReportUsage() {
		t.Fatal("expected reporting to default on")
	}
	off := false
	if (extensions.Spec{Telemetry: &extensions.Telemetry{ReportUsage: &off}}).ReportUsage() {
		t.Fatal("expected reportUsage: false to opt out")
	}
}
