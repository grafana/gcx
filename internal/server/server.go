package server

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/server/grafana"
	"github.com/grafana/gcx/internal/server/handlers"
	"github.com/grafana/gcx/internal/server/livereload"
	"github.com/grafana/grafana-app-sdk/logging"
)

type Config struct {
	ListenAddr string
	Port       int
	NoColor    bool
}

type Server struct {
	config           Config
	context          *config.Context
	resources        *resources.Resources
	resourceHandlers []handlers.ResourceHandler
	proxy            *httputil.ReverseProxy
	subpath          string
}

func New(config Config, context *config.Context, resources *resources.Resources) *Server {
	return &Server{
		config:    config,
		context:   context,
		resources: resources,
		resourceHandlers: []handlers.ResourceHandler{
			handlers.NewDashboardProxy(context, resources),
			handlers.NewFoldersProxy(resources),
		},
	}
}

func (s *Server) Start(ctx context.Context) error {
	assetsFS, err := fs.Sub(embedFS, "embed/assets")
	if err != nil {
		return fmt.Errorf("could not create a sub-tree from the embedded assets FS: %w", err)
	}

	u, err := url.Parse(s.context.Grafana.Server)
	if err != nil {
		return err
	}

	s.subpath = strings.TrimSuffix(u.Path, "/")
	s.proxy = &httputil.ReverseProxy{
		Transport: httputils.NewTransport(s.context),
		Rewrite: func(r *httputil.ProxyRequest) {
			u.Path = "" // to ensure possible sub-paths won't be added twice.
			r.SetURL(u)

			grafana.AuthenticateRequest(s.context.Grafana, r.Out)

			r.Out.Header.Del("Origin")
			r.Out.Header.Set("User-Agent", httputils.UserAgent)
		},
	}

	r := chi.NewRouter()

	// Log HTTP requests/responses at debug level
	r.Use(middleware.RequestLogger(&middleware.DefaultLogFormatter{
		Logger:  logs.DecorateAtLevel(logging.FromContext(ctx), slog.LevelInfo),
		NoColor: s.config.NoColor,
	}))
	// Inject the logger in HTTP requests context
	r.Use(func(h http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			//nolint:contextcheck
			r = r.WithContext(logging.Context(r.Context(), logging.FromContext(ctx)))
			h.ServeHTTP(w, r)
		}

		return http.HandlerFunc(fn)
	})

	s.applyStaticProxyConfig(r, s.staticProxyConfig())

	for _, handler := range s.resourceHandlers {
		logging.FromContext(ctx).Debug("registering proxy handler")

		for _, endpoint := range handler.Endpoints(s.proxy) {
			switch endpoint.Method {
			case http.MethodGet:
				r.Get(s.subpath+endpoint.URL, endpoint.Handler)
			case http.MethodPost:
				r.Post(s.subpath+endpoint.URL, endpoint.Handler)
			case http.MethodPut:
				r.Put(s.subpath+endpoint.URL, endpoint.Handler)
			default:
				return fmt.Errorf("unknown endpoint method %s", endpoint.Method)
			}
		}

		s.applyStaticProxyConfig(r, handler.StaticEndpoints())
	}

	// Livereload setup
	var upgrader = &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(_ *http.Request) bool { return true },
	}

	livereload.Initialize()
	r.Get("/livereload", livereload.Handler(upgrader))

	s.resources.OnChange(func(resource *resources.Resource) {
		logging.FromContext(ctx).Debug("Resource changed in memory", slog.String("component", "livereload"), slog.String("resource", string(resource.Ref())))
		livereload.ReloadResource(resource)
	})

	r.Get("/", s.rootHandler)
	r.Get("/gcx/{group}/{version}/{kind}/{name}", s.iframeHandler)
	r.Handle("/gcx/assets/*", http.StripPrefix("/gcx/assets/", http.FileServer(http.FS(assetsFS))))

	//nolint:gosec
	return http.ListenAndServe(fmt.Sprintf("%s:%d", s.config.ListenAddr, s.config.Port), r)
}

func (s *Server) applyStaticProxyConfig(r chi.Router, config handlers.StaticProxyConfig) {
	for _, pattern := range config.ProxyGet {
		r.Get(s.subpath+pattern, s.proxy.ServeHTTP)
	}
	for _, pattern := range config.ProxyPost {
		r.Post(s.subpath+pattern, s.proxy.ServeHTTP)
	}
	for pattern, response := range config.MockGet {
		r.Get(s.subpath+pattern, s.mockHandler(response))
	}
	for pattern, response := range config.MockPost {
		r.Post(s.subpath+pattern, s.mockHandler(response))
	}
}

func (s *Server) staticProxyConfig() handlers.StaticProxyConfig {
	return handlers.StaticProxyConfig{
		ProxyGet: []string{
			"/public/*",
			"/avatar/*",
		},
		MockGet: map[string]string{
			"/api/ma/events":       "[]",
			"/api/live/publish":    "[]",
			"/api/live/list":       "[]",
			"/api/user/orgs":       "[]",
			"/api/search":          "[]",
			"/api/usage/*":         "[]",
			"/api/frontend/assets": "{}",
			"/api/org/preferences": "{}",
			"/api/org/users":       "[]",

			"/api/prometheus/grafana/api/v1/rules": `{
      "status": "success",
      "data": { "groups": [] }
    }`,
			"/api/folders": "[]",
			"/api/recording-rules/writer": `{
      "id": "cojWep7Vz",
      "data_source_uid": "grafanacloud-prom",
      "remote_write_path": "/api/prom/push"
    }`,

			"/apis/banners.grafana.app/v0alpha1/namespaces/{stack}/announcement-banners": `{
      "kind": "AnnouncementBannerList",
      "apiVersion": "banners.grafana.app/v0alpha1",
      "metadata": {"resourceVersion": "29"}
    }`,
		},
		MockPost: map[string]string{
			"/api/frontend-metrics": "[]",
			"/api/search-v2":        "[]",
			"/api/live/publish":     "{}",
			"/api/ma/events":        "null",
		},
	}
}

func (s *Server) rootHandler(w http.ResponseWriter, r *http.Request) {
	templateVars := map[string]any{
		"CurrentContext": s.context,
		"Resources":      s.resources,
	}

	templates = templates.Funcs(template.FuncMap{
		"kindHasProxy": func(kind string) bool {
			for _, handler := range s.resourceHandlers {
				if handler.ResourceType().Kind == kind && handler.ProxyURL("foo") != "" {
					return true
				}
			}
			return false
		},
	})

	err := renderTemplate(w, "proxy/index.html.tmpl", templateVars)
	if err != nil {
		httputils.Error(r, w, "Error while executing template", err, http.StatusInternalServerError)
		return
	}
}

func (s *Server) mockHandler(response string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		httputils.Write(r, w, []byte(response))
	}
}

func (s *Server) iframeHandler(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	version := chi.URLParam(r, "version")
	kind := chi.URLParam(r, "kind")
	name := chi.URLParam(r, "name")

	var handler handlers.ResourceHandler
	for _, candidate := range s.resourceHandlers {
		candidateResourceType := candidate.ResourceType()
		groupVersion := candidateResourceType.GroupVersion

		if candidateResourceType.Kind == kind &&
			groupVersion.Group == group &&
			(groupVersion.Version == version || groupVersion.Version == "") {
			handler = candidate
			break
		}
	}

	msg := fmt.Sprintf("Could not find handler for group=%s, version=%s, kind=%s", group, version, kind)
	if handler == nil {
		httputils.Error(r, w, msg, errors.New(msg), http.StatusInternalServerError)
		return
	}

	templateVars := map[string]any{
		"IframeURL":      s.subpath + handler.ProxyURL(name),
		"CurrentContext": s.context,
		"Port":           s.config.Port,
	}

	if err := renderTemplate(w, "proxy/iframe.html.tmpl", templateVars); err != nil {
		httputils.Error(r, w, "Error while executing template", err, http.StatusInternalServerError)
		return
	}
}
