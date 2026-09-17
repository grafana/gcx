// Package api defines API metadata shared by SLO resource types.
package api

const (
	// Group identifies Grafana SLO resources.
	Group = "slo.ext.grafana.app"
	// Version is the API version used by SLO resource manifests.
	Version = "v1alpha1"
	// GroupVersion is the manifest apiVersion.
	GroupVersion = Group + "/" + Version
)
