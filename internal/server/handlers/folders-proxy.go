package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"

	"github.com/go-chi/chi/v5"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/grafana-app-sdk/logging"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ ResourceHandler = &FoldersProxy{}

// FoldersProxy describes how to proxy Folder resources.
type FoldersProxy struct {
	resources *resources.Resources
}

func NewFoldersProxy(resources *resources.Resources) *FoldersProxy {
	return &FoldersProxy{
		resources: resources,
	}
}

// ResourceType returns the resource descriptor for folders.
// FIXME: resources stuff.
func (c *FoldersProxy) ResourceType() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group: "folder.grafana.app",
			// Serves any version
		},
		Kind:     "Folder",
		Singular: "folder",
		Plural:   "folders",
	}
}

func (c *FoldersProxy) ProxyURL(_ string) string {
	return ""
}

func (c *FoldersProxy) Endpoints(_ *httputil.ReverseProxy) []HTTPEndpoint {
	return []HTTPEndpoint{
		{
			Method:  http.MethodGet,
			URL:     "/api/folders/{name}",
			Handler: c.folderJSONGetHandler(),
		},
	}
}

func (c *FoldersProxy) StaticEndpoints() StaticProxyConfig {
	return StaticProxyConfig{}
}

func (c *FoldersProxy) folderJSONGetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if name == "" {
			httputils.Error(r, w, "No name specified", errors.New("no name specified within the URL"), http.StatusBadRequest)
			return
		}

		// TODO: use at least group + kind to identify a resource
		resource, found := c.resources.Find("Folder", name)

		title := name
		if found {
			title = resource.Raw.FindTitle(name)
		} else {
			logging.FromContext(r.Context()).Info(fmt.Sprintf("Folder with name %s not found locally: returning mock folder", name))
		}

		// TODO: this is far from complete, but it's enough to serve dashboards defined in a folder
		folder := map[string]any{
			"uid":   name,
			"title": title,
		}

		httputils.WriteJSON(r, w, folder)
	}
}
