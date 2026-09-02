package probes

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// DefaultAgentImage is the default container image for the SM agent.
const DefaultAgentImage = "grafana/synthetic-monitoring-agent:v0.64.2"

// DeployConfig holds all parameters needed to generate SM agent manifests.
type DeployConfig struct {
	ProbeName    string // Name for Kubernetes resources (e.g. "my-private-probe")
	ProbeToken   string // Probe auth token from create response
	APIServerURL string // SM API gRPC endpoint (e.g. "synthetic-monitoring-grpc.grafana.net:443")
	Namespace    string // Kubernetes namespace (default "synthetic-monitoring")
	Image        string // SM agent container image
}

// k8sNameRe matches valid Kubernetes resource names (DNS label: lowercase
// alphanumeric and hyphens, 1-63 chars, must start and end with alphanumeric).
var k8sNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Validate checks that all required fields are safe to use in a Kubernetes
// manifest.
func (c DeployConfig) Validate() error {
	if c.ProbeToken == "" {
		return errors.New("probe token is required")
	}
	if c.ProbeName == "" {
		return errors.New("probe name is required")
	}
	if !k8sNameRe.MatchString(c.ProbeName) {
		return errors.New("probe name must be a valid Kubernetes name (lowercase alphanumeric and hyphens, 1-63 chars)")
	}
	if c.APIServerURL == "" {
		return errors.New("API server address is required")
	}
	if strings.Contains(c.APIServerURL, "://") {
		return errors.New("API server address must use host:port format without a URL scheme")
	}
	host, port, err := net.SplitHostPort(c.APIServerURL)
	if err != nil || host == "" || port == "" {
		return errors.New("API server address must use host:port format")
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return errors.New("API server address must contain a valid port")
	}
	if c.Namespace == "" {
		return errors.New("namespace is required")
	}
	if !k8sNameRe.MatchString(c.Namespace) {
		return errors.New("namespace must be a valid Kubernetes name (lowercase alphanumeric and hyphens, 1-63 chars)")
	}
	if strings.TrimSpace(c.Image) == "" {
		return errors.New("agent image is required")
	}
	if strings.TrimSpace(c.Image) != c.Image {
		return fmt.Errorf("agent image %q must not start or end with whitespace", c.Image)
	}
	return nil
}

// manifestTemplate is the Go template for generating SM agent Kubernetes manifests.
//
//nolint:gochecknoglobals // Static template parsed once at init time.
var manifestTemplate = template.Must(template.New("manifests").Parse(`apiVersion: v1
kind: Namespace
metadata:
  name: {{ .Namespace }}
---
apiVersion: v1
kind: Secret
metadata:
  name: {{ .ProbeName }}-sm-agent
  namespace: {{ .Namespace }}
type: Opaque
data:
  api-token: {{ .EncodedToken }}
  api-server-address: {{ .EncodedAPIServerURL }}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .ProbeName }}-sm-agent
  namespace: {{ .Namespace }}
  labels:
    app: {{ .ProbeName }}-sm-agent
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ .ProbeName }}-sm-agent
  template:
    metadata:
      labels:
        app: {{ .ProbeName }}-sm-agent
    spec:
      automountServiceAccountToken: false
      containers:
        - name: sm-agent
          image: {{ printf "%q" .Image }}
          args:
            - --api-server-address=$(API_SERVER_ADDRESS)
          env:
            - name: API_SERVER_ADDRESS
              valueFrom:
                secretKeyRef:
                  name: {{ .ProbeName }}-sm-agent
                  key: api-server-address
            - name: SM_AGENT_API_TOKEN
              valueFrom:
                secretKeyRef:
                  name: {{ .ProbeName }}-sm-agent
                  key: api-token
`))

// templateData is the data passed to the manifest template.
type templateData struct {
	ProbeName           string
	Namespace           string
	Image               string
	EncodedToken        string
	EncodedAPIServerURL string
}

// RenderManifests writes Kubernetes YAML manifests for an SM agent to w.
// It generates a Namespace, Secret, and Deployment separated by "---".
func RenderManifests(w io.Writer, cfg DeployConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	data := templateData{
		ProbeName:           cfg.ProbeName,
		Namespace:           cfg.Namespace,
		Image:               cfg.Image,
		EncodedToken:        base64.StdEncoding.EncodeToString([]byte(cfg.ProbeToken)),
		EncodedAPIServerURL: base64.StdEncoding.EncodeToString([]byte(cfg.APIServerURL)),
	}

	return manifestTemplate.Execute(w, data)
}
