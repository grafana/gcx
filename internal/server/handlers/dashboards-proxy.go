package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/server/grafana"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/grafana/pkg/apimachinery/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ ResourceHandler = &DashboardProxy{}

// DashboardProxy describes how to proxy Dashboard resources.
type DashboardProxy struct {
	context   *config.Context
	resources *resources.Resources
}

func NewDashboardProxy(context *config.Context, resources *resources.Resources) *DashboardProxy {
	return &DashboardProxy{
		context:   context,
		resources: resources,
	}
}

func (c *DashboardProxy) ResourceType() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group: "dashboard.grafana.app",
			// Serves any version
		},
		Kind:     "Dashboard",
		Singular: "dashboard",
		Plural:   "dashboards",
	}
}

func (c *DashboardProxy) ProxyURL(uid string) string {
	return fmt.Sprintf("/d/%s/slug", uid)
}

func (c *DashboardProxy) Endpoints(_ *httputil.ReverseProxy) []HTTPEndpoint {
	return []HTTPEndpoint{
		{
			Method:  http.MethodGet,
			URL:     "/d/{uid}/{slug}",
			Handler: grafana.AuthenticateAndProxyHandler(c.context),
		},
		{
			Method:  http.MethodGet,
			URL:     "/apis/dashboard.grafana.app/",
			Handler: c.dashboardAPIGroupDiscoveryHandler(),
		},
		{
			Method:  http.MethodGet,
			URL:     "/apis/dashboard.grafana.app",
			Handler: c.dashboardAPIGroupDiscoveryHandler(),
		},
		{
			Method:  http.MethodGet,
			URL:     "/apis/dashboard.grafana.app/{version}/namespaces/{namespace}/dashboards/{name}/dto",
			Handler: c.dashboardJSONGetHandler(),
		},
		{
			Method:  http.MethodPut,
			URL:     "/apis/dashboard.grafana.app/{version}/namespaces/{namespace}/dashboards/{name}",
			Handler: c.dashboardJSONPostHandler(),
		},
	}
}

// dashboardAPIGroupDiscoveryHandler serves the k8s-style API group discovery
// document for dashboard.grafana.app (GET /apis/dashboard.grafana.app/).
// Grafana's frontend calls this once per session to negotiate which dashboard
// API version to use, caching the result; if the call fails (as it did here
// -- this route didn't exist at all), it silently falls back to its own
// hardcoded default (v2beta1) regardless of what's actually on disk. Reporting
// the version that's actually loaded as "preferred" lets the frontend
// negotiate the right version itself, so dashboardJSONGetHandler sees a
// version match and never needs to convert anything.
func (c *DashboardProxy) dashboardAPIGroupDiscoveryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		preferred := c.preferredDashboardVersion()

		httputils.WriteJSON(r, w, map[string]any{
			"kind":             "APIGroup",
			"apiVersion":       "v1",
			"name":             "dashboard.grafana.app",
			"versions":         dashboardAPIGroupVersions(preferred),
			"preferredVersion": dashboardAPIGroupVersion(preferred),
		})
	}
}

// dashboardAPIGroupVersions lists the known dashboard.grafana.app versions,
// most-preferred-first, with `preferred` unioned in (and moved to the front)
// even if it isn't one of the known six -- e.g. a hand-written or future
// apiVersion gcx hasn't heard of. Without this, a client that validates
// preferredVersion against the advertised list would reject the discovery
// document and fall straight back to the hardcoded-fallback behaviour this
// handler exists to avoid.
func dashboardAPIGroupVersions(preferred string) []map[string]any {
	known := []string{"v2", "v2beta1", "v2alpha1", "v1", "v1beta1", "v0alpha1"}

	versions := make([]string, 0, len(known)+1)
	versions = append(versions, preferred)
	for _, v := range known {
		if v != preferred {
			versions = append(versions, v)
		}
	}

	out := make([]map[string]any, len(versions))
	for i, v := range versions {
		out[i] = dashboardAPIGroupVersion(v)
	}
	return out
}

func dashboardAPIGroupVersion(version string) map[string]any {
	return map[string]any{
		"groupVersion": "dashboard.grafana.app/" + version,
		"version":      version,
	}
}

// preferredDashboardVersion returns whichever apiVersion is most common
// among the currently loaded Dashboard resources (ties broken by first-seen
// order), defaulting to "v2" if none are loaded. Iterates resources.AsList(),
// which is sorted by group/version/kind/name -- Resources' underlying
// collection is a map, so iterating it directly (e.g. via ForEach) would make
// "first-seen order" meaningless, since Go randomises map iteration order.
// Version negotiation on the frontend is resolved once per browser session
// and cached, so a directory mixing multiple dashboard versions only gets
// this benefit for whichever version "wins" here -- the rest fall back to
// dashboardJSONGetHandler's existing mismatch handling.
func (c *DashboardProxy) preferredDashboardVersion() string {
	counts := map[string]int{}
	order := make([]string, 0, 4)

	for _, resource := range c.resources.AsList() {
		if resource.Kind() != "Dashboard" {
			continue
		}

		v := resource.Version()
		if counts[v] == 0 {
			order = append(order, v)
		}
		counts[v]++
	}

	best, bestCount := "v2", 0
	for _, v := range order {
		if counts[v] > bestCount {
			best, bestCount = v, counts[v]
		}
	}

	return best
}

func (c *DashboardProxy) StaticEndpoints() StaticProxyConfig {
	return StaticProxyConfig{
		ProxyGet: []string{
			"/api/datasources/proxy/*",
			"/api/datasources/*",
			"/api/plugins/*",
			"/bootdata",
		},
		ProxyPost: []string{
			"/api/datasources/proxy/*",
			"/apis/query.grafana.app/*",
			"/api/ds/query",
		},
		MockGet: map[string]string{
			"/api/annotations":                 "[]",
			"/api/access-control/user/actions": `{"dashboards:write": true}`,
		},
	}
}

func (c *DashboardProxy) dashboardJSONGetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resource := c.dashboardFromRequest(w, r)
		if resource == nil {
			return
		}

		accessConfig := map[string]any{
			"slug":      "slug",
			"url":       "/d/" + resource.Name() + "/slug",
			"canSave":   true,
			"canEdit":   true,
			"canAdmin":  false,
			"canStar":   false,
			"canDelete": false,
			"annotationsPermissions": map[string]any{
				"dashboard": map[string]any{
					"canAdd":    false,
					"canEdit":   true,
					"canDelete": false,
				},
				"organization": map[string]any{
					"canAdd":    false,
					"canEdit":   true,
					"canDelete": false,
				},
			},
		}

		version := chi.URLParam(r, "version")

		if version != resource.Version() {
			logging.FromContext(r.Context()).Debug("resource version mismatch", slog.String("requested", version), slog.String("available", resource.Version()))
			httputils.WriteJSON(r, w, map[string]any{
				"kind":       "DashboardWithAccessInfo",
				"apiVersion": resource.APIVersion(),
				"metadata": map[string]any{
					"name":      resource.Name(),
					"namespace": resource.Namespace(),
				},
				"spec": nil,
				"status": map[string]any{
					"conversion": map[string]any{
						"failed":        true,
						"storedVersion": resource.Version(),
						"error":         "conversion in proxy implemented",
					},
				},
				"access": accessConfig,
			})
			return
		}

		spec, err := resource.Raw.GetSpec()
		if err != nil {
			err := errors.New("could not get resource spec")
			httputils.Error(r, w, err.Error(), err, http.StatusInternalServerError)
			return
		}

		generation := max(resource.Raw.GetGeneration(), 1)

		object := map[string]any{
			"kind":       "DashboardWithAccessInfo",
			"apiVersion": resource.APIVersion(),
			"access":     accessConfig,
			"metadata": map[string]any{
				"name":        resource.Name(),
				"namespace":   resource.Namespace(),
				"generation":  generation,
				"labels":      resource.Raw.GetLabels(),
				"annotations": resource.Raw.GetAnnotations(),
			},
			"spec": spec,
		}

		httputils.WriteJSON(r, w, object)
	}
}

func (c *DashboardProxy) dashboardJSONPostHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resource := c.dashboardFromRequest(w, r)
		if resource == nil {
			return
		}

		if resource.SourcePath() == "" {
			err := errors.New("resources generated from a script can not be persisted through gcx serve")
			httputils.Error(r, w, err.Error(), err, http.StatusBadRequest)
			return
		}

		input := map[string]any{}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&input); err != nil {
			httputils.Error(r, w, err.Error(), err, http.StatusBadRequest)
			return
		}

		object := &unstructured.Unstructured{}
		object.SetUnstructuredContent(input)
		object.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "dashboard.grafana.app",
			Version: chi.URLParam(r, "version"),
			Kind:    "Dashboard",
		})

		// Delete the annotation that the UI always sets.
		ans := object.GetAnnotations()
		delete(ans, resources.AnnotationSavedFromUI)
		object.SetAnnotations(ans)

		// Reset the generation to 0.
		object.SetGeneration(0)

		file, err := os.OpenFile(resource.SourcePath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			httputils.Error(r, w, err.Error(), err, http.StatusInternalServerError)
			return
		}
		defer file.Close()

		var codec format.Encoder = format.NewJSONCodec()
		if resource.SourceFormat() == format.YAML {
			codec = format.NewYAMLCodec()
		}

		if err := codec.Encode(file, object); err != nil {
			httputils.Error(r, w, err.Error(), err, http.StatusInternalServerError)
			return
		}

		metaAccessor, _ := utils.MetaAccessor(object)
		resource.Raw = metaAccessor

		httputils.WriteJSON(r, w, object)
	}
}

func (c *DashboardProxy) dashboardFromRequest(w http.ResponseWriter, r *http.Request) *resources.Resource {
	name := chi.URLParam(r, "name")
	if name == "" {
		httputils.Error(r, w, "No name specified", errors.New("no name specified within the URL"), http.StatusBadRequest)
		return nil
	}

	// TODO: kind + name isn't enough to unambiguously identify a resource
	resource, found := c.resources.Find("Dashboard", name)
	if !found {
		httputils.Error(r, w, fmt.Sprintf("Dashboard with name %s not found", name), fmt.Errorf("dashboard with UID %s not found", name), http.StatusNotFound)
		return nil
	}

	return resource
}
