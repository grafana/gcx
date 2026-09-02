package probes_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/probes"
)

func TestRenderManifests(t *testing.T) {
	cfg := probes.DeployConfig{
		ProbeName:    "my-private-probe",
		ProbeToken:   "secret-token-123",
		APIServerURL: "synthetic-monitoring-grpc.grafana.net:443",
		Namespace:    "synthetic-monitoring",
		Image:        probes.DefaultAgentImage,
	}

	var buf bytes.Buffer
	if err := probes.RenderManifests(&buf, cfg); err != nil {
		t.Fatalf("RenderManifests() error = %v", err)
	}

	output := buf.String()

	// Should contain three documents separated by "---".
	docs := strings.Split(output, "---\n")
	// First split element may be empty if output starts with "---", or we get 3+ parts.
	var nonEmpty []string
	for _, d := range docs {
		if strings.TrimSpace(d) != "" {
			nonEmpty = append(nonEmpty, d)
		}
	}
	if len(nonEmpty) != 3 {
		t.Fatalf("expected 3 YAML documents, got %d\n%s", len(nonEmpty), output)
	}

	// Verify Namespace document.
	namespaceDoc := nonEmpty[0]
	if !strings.Contains(namespaceDoc, "kind: Namespace") {
		t.Error("first document should be a Namespace")
	}
	if !strings.Contains(namespaceDoc, "name: synthetic-monitoring") {
		t.Error("Namespace should have the configured name")
	}

	// Verify Secret document.
	secretDoc := nonEmpty[1]
	if !strings.Contains(secretDoc, "kind: Secret") {
		t.Error("second document should be a Secret")
	}
	if !strings.Contains(secretDoc, "namespace: synthetic-monitoring") {
		t.Error("Secret should have correct namespace")
	}
	encodedToken := base64.StdEncoding.EncodeToString([]byte("secret-token-123"))
	if !strings.Contains(secretDoc, encodedToken) {
		t.Errorf("Secret should contain base64-encoded token %q", encodedToken)
	}
	encodedURL := base64.StdEncoding.EncodeToString([]byte("synthetic-monitoring-grpc.grafana.net:443"))
	if !strings.Contains(secretDoc, encodedURL) {
		t.Errorf("Secret should contain base64-encoded API server URL %q", encodedURL)
	}
	if !strings.Contains(secretDoc, "api-token:") {
		t.Error("Secret should contain the api-token key")
	}
	if !strings.Contains(secretDoc, "api-server-address:") {
		t.Error("Secret should contain the api-server-address key")
	}

	// Verify Deployment document.
	deployDoc := nonEmpty[2]
	if !strings.Contains(deployDoc, "kind: Deployment") {
		t.Error("third document should be a Deployment")
	}
	if !strings.Contains(deployDoc, "namespace: synthetic-monitoring") {
		t.Error("Deployment should have correct namespace")
	}
	if !strings.Contains(deployDoc, `image: "`+probes.DefaultAgentImage+`"`) {
		t.Error("Deployment should contain the default agent image")
	}
	if !strings.Contains(deployDoc, "--api-server-address=$(API_SERVER_ADDRESS)") {
		t.Error("Deployment should pass the API server address to the agent")
	}
	if !strings.Contains(deployDoc, "SM_AGENT_API_TOKEN") {
		t.Error("Deployment should pass the token through the supported environment variable")
	}
	if strings.Contains(deployDoc, "--api-token") {
		t.Error("Deployment should not expose the token in the container arguments")
	}
	if !strings.Contains(deployDoc, "automountServiceAccountToken: false") {
		t.Error("Deployment should disable the Kubernetes service account token mount")
	}
	if !strings.Contains(deployDoc, "my-private-probe") {
		t.Error("Deployment should reference the probe name in labels")
	}
	if !strings.Contains(deployDoc, "replicas: 1") {
		t.Error("Deployment should have a single replica")
	}
}

func TestDefaultAgentImageUsesLatestTag(t *testing.T) {
	if probes.DefaultAgentImage != "grafana/synthetic-monitoring-agent:latest" {
		t.Errorf("DefaultAgentImage = %q, want the floating latest tag", probes.DefaultAgentImage)
	}
}

func TestK8sNameValidation(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"my-probe", false},
		{"a", false},
		{"probe-123", false},
		{"a-b", false},
		{"", true},                      // empty
		{"Uppercase", true},             // uppercase
		{"has space", true},             // space
		{"has/slash", true},             // slash
		{"has.dot", true},               // dot
		{"-leading", true},              // leading hyphen
		{"trailing-", true},             // trailing hyphen
		{strings.Repeat("a", 64), true}, // too long
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := probes.DeployConfig{
				ProbeName:    tt.name,
				ProbeToken:   "token",
				APIServerURL: "grpc.example.com:443",
				Namespace:    "default",
				Image:        "img:latest",
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() for name %q: error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestDeployConfigValidation(t *testing.T) {
	valid := probes.DeployConfig{
		ProbeName:    "my-probe",
		ProbeToken:   "token",
		APIServerURL: "grpc.example.com:443",
		Namespace:    "synthetic-monitoring",
		Image:        probes.DefaultAgentImage,
	}

	tests := []struct {
		name   string
		change func(*probes.DeployConfig)
	}{
		{name: "missing namespace", change: func(c *probes.DeployConfig) { c.Namespace = "" }},
		{name: "invalid namespace", change: func(c *probes.DeployConfig) { c.Namespace = "Invalid" }},
		{name: "missing image", change: func(c *probes.DeployConfig) { c.Image = "" }},
		{name: "image with surrounding whitespace", change: func(c *probes.DeployConfig) { c.Image = " image:v1" }},
		{name: "API server URL with scheme", change: func(c *probes.DeployConfig) { c.APIServerURL = "https://grpc.example.com:443" }},
		{name: "API server without port", change: func(c *probes.DeployConfig) { c.APIServerURL = "grpc.example.com" }},
		{name: "API server with invalid port", change: func(c *probes.DeployConfig) { c.APIServerURL = "grpc.example.com:invalid" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.change(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Error("Validate() error = nil, want an error")
			}
		})
	}
}

func TestRenderManifestsValidatesConfig(t *testing.T) {
	var buf bytes.Buffer
	err := probes.RenderManifests(&buf, probes.DeployConfig{
		ProbeName:    "my-probe",
		ProbeToken:   "token",
		APIServerURL: "grpc.example.com:443",
		Namespace:    "",
		Image:        probes.DefaultAgentImage,
	})
	if err == nil {
		t.Fatal("RenderManifests() error = nil, want an error")
	}
	if buf.Len() != 0 {
		t.Errorf("RenderManifests() wrote %d bytes for invalid input", buf.Len())
	}
}

func TestRenderManifests_CustomConfig(t *testing.T) {
	cfg := probes.DeployConfig{
		ProbeName:    "my-custom-probe",
		ProbeToken:   "tok3n+with/special=chars",
		APIServerURL: "grpc.example.com:443",
		Namespace:    "custom-ns",
		Image:        "grafana/synthetic-monitoring-agent:v0.1.0",
	}

	var buf bytes.Buffer
	if err := probes.RenderManifests(&buf, cfg); err != nil {
		t.Fatalf("RenderManifests() error = %v", err)
	}

	output := buf.String()

	// Probe name used in resource names.
	if !strings.Contains(output, "my-custom-probe") {
		t.Error("probe name should appear in output")
	}

	// Token should be base64-encoded correctly (including special chars).
	encodedToken := base64.StdEncoding.EncodeToString([]byte("tok3n+with/special=chars"))
	if !strings.Contains(output, encodedToken) {
		t.Errorf("Secret should contain base64-encoded token %q", encodedToken)
	}

	// Custom namespace should be used.
	if !strings.Contains(output, "namespace: custom-ns") {
		t.Error("manifests should use the custom namespace")
	}

	// Custom image should be used.
	if !strings.Contains(output, "grafana/synthetic-monitoring-agent:v0.1.0") {
		t.Error("Deployment should use the custom image")
	}
}
