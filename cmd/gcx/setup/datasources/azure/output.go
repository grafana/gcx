package azure

import (
	"github.com/grafana/gcx/internal/config"
	azonboard "github.com/grafana/gcx/internal/onboard/azure"
)

// subLabel renders a subscription for progress output, preferring its name.
func subLabel(a azonboard.Account) string {
	if a.Name != "" {
		return a.Name
	}
	return a.SubID
}

// stackLabel derives the Grafana stack slug (used to label and attribute
// created artifacts) from the resolved REST config.
func stackLabel(restCfg config.NamespacedRESTConfig) string {
	url := restCfg.GrafanaURL
	if url == "" {
		url = restCfg.Host
	}
	if slug, ok := config.StackSlugFromServerURL(url); ok && slug != "" {
		return slug
	}
	return ""
}
