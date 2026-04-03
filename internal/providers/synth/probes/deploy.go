package probes

import (
	"encoding/base64"
	"io"
	"text/template"
)

// DefaultAgentImage is the default container image for the SM agent.
const DefaultAgentImage = "grafana/synthetic-monitoring-agent:latest"

// DeployConfig holds all parameters needed to generate SM agent manifests.
type DeployConfig struct {
	ProbeName    string // Name for k8s resources (e.g. "my-private-probe")
	ProbeToken   string // Probe auth token from create response
	APIServerURL string // SM API gRPC endpoint (e.g. "synthetic-monitoring-grpc.grafana.net:443")
	Namespace    string // K8s namespace (default "synthetic-monitoring")
	Image        string // SM agent container image
}

// manifestTemplate is the Go template for generating SM agent k8s manifests.
//
//nolint:gochecknoglobals // Static template parsed once at init time.
var manifestTemplate = template.Must(template.New("manifests").Parse(`apiVersion: v1
kind: Secret
metadata:
  name: {{ .ProbeName }}-sm-agent
  namespace: {{ .Namespace }}
type: Opaque
data:
  API_ACCESS_TOKEN: {{ .EncodedToken }}
  API_SERVER_URL: {{ .EncodedAPIServerURL }}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .ProbeName }}-sm-agent
  namespace: {{ .Namespace }}
  labels:
    app: {{ .ProbeName }}-sm-agent
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
      serviceAccountName: {{ .ProbeName }}-sm-agent
      containers:
        - name: sm-agent
          image: {{ .Image }}
          env:
            - name: API_SERVER_URL
              valueFrom:
                secretKeyRef:
                  name: {{ .ProbeName }}-sm-agent
                  key: API_SERVER_URL
            - name: API_ACCESS_TOKEN
              valueFrom:
                secretKeyRef:
                  name: {{ .ProbeName }}-sm-agent
                  key: API_ACCESS_TOKEN
`))

// templateData is the data passed to the manifest template.
type templateData struct {
	ProbeName           string
	Namespace           string
	Image               string
	EncodedToken        string
	EncodedAPIServerURL string
}

// RenderManifests writes k8s YAML manifests for deploying an SM agent to w.
// Generates: Secret, ServiceAccount, Deployment (separated by "---").
func RenderManifests(w io.Writer, cfg DeployConfig) error {
	data := templateData{
		ProbeName:           cfg.ProbeName,
		Namespace:           cfg.Namespace,
		Image:               cfg.Image,
		EncodedToken:        base64.StdEncoding.EncodeToString([]byte(cfg.ProbeToken)),
		EncodedAPIServerURL: base64.StdEncoding.EncodeToString([]byte(cfg.APIServerURL)),
	}

	return manifestTemplate.Execute(w, data)
}
