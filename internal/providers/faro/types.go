// Package faro provides a client and resource adapter for Grafana Frontend Observability (Faro).
package faro

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/resources/adapter"
)

// FaroApp represents a Frontend Observability application.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type FaroApp struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	// AppType is set at creation; the API ignores changes on update.
	AppType string `json:"appType,omitempty"`
	// A nil Runtime preserves the stored runtime on update.
	// A pointer preserves explicit empty input so the API can reject it.
	Runtime            *string `json:"runtime,omitempty"`
	AppKey             string  `json:"appKey,omitempty"`
	CollectEndpointURL string  `json:"collectEndpointURL,omitempty"`
	// OTLPIngestEndpointURL is the base endpoint the native mobile SDKs (Android
	// and iOS OpenTelemetry) send to. The web SDK uses CollectEndpointURL.
	// The API returns the base URL only — append AppKey to make it usable.
	OTLPIngestEndpointURL string            `json:"otlpIngestEndpointURL,omitempty"`
	CORSOrigins           []CORSOrigin      `json:"corsOrigins,omitempty"`
	ExtraLogLabels        map[string]string `json:"extraLogLabels,omitempty"`
	Settings              *FaroAppSettings  `json:"settings,omitempty"`
}

// GetResourceName returns the composite slug name for this Faro app (e.g. "my-web-app-42").
func (app FaroApp) GetResourceName() string {
	slug := adapter.SlugifyName(app.Name)
	return adapter.ComposeName(slug, app.ID)
}

// SetResourceName restores the numeric ID from a composite slug name.
func (app *FaroApp) SetResourceName(name string) {
	if id, ok := adapter.ExtractIDFromSlug(name); ok {
		app.ID = id
	}
}

// faroAppAPI is the API wire representation with array-based extraLogLabels.
type faroAppAPI struct {
	ID                    int64        `json:"id,omitempty"`
	Name                  string       `json:"name"`
	AppType               string       `json:"appType,omitempty"`
	Runtime               *string      `json:"runtime,omitempty"`
	AppKey                string       `json:"appKey,omitempty"`
	CollectEndpointURL    string       `json:"collectEndpointURL,omitempty"`
	OTLPIngestEndpointURL string       `json:"otlpIngestEndpointURL,omitempty"`
	CORSOrigins           []CORSOrigin `json:"corsOrigins,omitempty"`
	ExtraLogLabels        []LogLabel   `json:"extraLogLabels,omitempty"`
	// The API stores settings as string pairs, e.g. "geolocation.enabled": "1".
	Settings map[string]string `json:"settings,omitempty"`
}

// LogLabel represents a key-value log label for the API.
// The API field is "label", not "key" — a mismatch here silently stores an
// empty label name, which makes Loki reject every write for the app.
type LogLabel struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CORSOrigin represents an allowed CORS origin.
type CORSOrigin struct {
	URL string `json:"url"`
}

// FaroAppSettings represents Faro app settings.
// Omitted fields keep their stored values on update: the API upserts the
// settings it receives and never deletes the others.
type FaroAppSettings struct {
	// A pointer keeps an explicit false, which disables geolocation.
	GeolocationEnabled *bool  `json:"geolocationEnabled,omitempty"`
	GeolocationLevel   string `json:"geolocationLevel,omitempty"`
	// ISO 3166-1 alpha-2 codes whose sessions are not enriched. Nil keeps the
	// stored list on update; an empty list clears it.
	GeolocationCountryDenylist []string `json:"geolocationCountryDenylist,omitempty"`
}

const (
	settingGeolocationEnabled = "geolocation.enabled"
	settingGeolocationLevel   = "geolocation.level"
	// The receiver binary-searches this comma-separated list, so it must be sorted.
	settingGeolocationCountryDenylist = "geolocation.country_denylist"
)

// geolocationLevels lists the manifest names in the API's index order:
// "geolocation.level" "0" is continent, "4" is network.
func geolocationLevels() []string {
	return []string{"continent", "country", "subdivision", "city", "network"}
}

// toAPI returns an empty map when nothing is set; omitempty then drops it.
func (s *FaroAppSettings) toAPI() (map[string]string, error) {
	out := map[string]string{}
	if s == nil {
		return out, nil
	}
	if s.GeolocationEnabled != nil {
		out[settingGeolocationEnabled] = "0"
		if *s.GeolocationEnabled {
			out[settingGeolocationEnabled] = "1"
		}
	}
	if s.GeolocationLevel != "" {
		i := slices.Index(geolocationLevels(), s.GeolocationLevel)
		if i < 0 {
			return nil, fmt.Errorf("faro: settings.geolocationLevel %q is invalid; use one of %v", s.GeolocationLevel, geolocationLevels())
		}
		out[settingGeolocationLevel] = strconv.Itoa(i)
	}
	if s.GeolocationCountryDenylist != nil {
		countries, err := countryDenylistToAPI(s.GeolocationCountryDenylist)
		if err != nil {
			return nil, err
		}
		out[settingGeolocationCountryDenylist] = countries
	}
	return out, nil
}

// countryDenylistToAPI uppercases, deduplicates and sorts the codes.
func countryDenylistToAPI(codes []string) (string, error) {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		c = strings.ToUpper(strings.TrimSpace(c))
		if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
			return "", fmt.Errorf("faro: settings.geolocationCountryDenylist entry %q is invalid; use ISO 3166-1 alpha-2 codes such as DE", c)
		}
		out = append(out, c)
	}
	slices.Sort(out)
	return strings.Join(slices.Compact(out), ","), nil
}

// settingsFromAPI keeps the geolocation settings only. The others stay on the
// server because updates do not delete settings they omit, but a new app
// created from an export does not get them.
func settingsFromAPI(m map[string]string) *FaroAppSettings {
	var s FaroAppSettings
	if v, ok := m[settingGeolocationEnabled]; ok {
		enabled := v == "1"
		s.GeolocationEnabled = &enabled
	}
	levels := geolocationLevels()
	if i, err := strconv.Atoi(m[settingGeolocationLevel]); err == nil && i >= 0 && i < len(levels) {
		s.GeolocationLevel = levels[i]
	}
	if v, ok := m[settingGeolocationCountryDenylist]; ok {
		s.GeolocationCountryDenylist = []string{}
		for c := range strings.SplitSeq(v, ",") {
			if c = strings.TrimSpace(c); c != "" {
				s.GeolocationCountryDenylist = append(s.GeolocationCountryDenylist, c)
			}
		}
	}
	if s.GeolocationEnabled == nil && s.GeolocationLevel == "" && len(s.GeolocationCountryDenylist) == 0 {
		return nil
	}
	return &s
}

// toAPI converts FaroApp to API wire format.
func (app *FaroApp) toAPI() (faroAppAPI, error) {
	labels := make([]LogLabel, 0, len(app.ExtraLogLabels))
	for k, v := range app.ExtraLogLabels {
		labels = append(labels, LogLabel{Label: k, Value: v})
	}
	var id int64
	if app.ID != "" {
		id, _ = strconv.ParseInt(app.ID, 10, 64)
	}
	settings, err := app.Settings.toAPI()
	if err != nil {
		return faroAppAPI{}, err
	}
	// The API assigns AppKey, CollectEndpointURL and OTLPIngestEndpointURL and
	// treats all three as read-only, so sending them back is harmless. This is
	// why StripFields keeps them in pulled manifests: they cannot leak one
	// stack's collector host into another on push.
	return faroAppAPI{
		AppType:               app.AppType,
		Runtime:               app.Runtime,
		ID:                    id,
		Name:                  app.Name,
		AppKey:                app.AppKey,
		CollectEndpointURL:    app.CollectEndpointURL,
		OTLPIngestEndpointURL: app.OTLPIngestEndpointURL,
		CORSOrigins:           app.CORSOrigins,
		ExtraLogLabels:        labels,
		Settings:              settings,
	}, nil
}

// fromAPI converts API wire format to FaroApp.
func fromAPI(api faroAppAPI) FaroApp {
	labels := make(map[string]string, len(api.ExtraLogLabels))
	for _, l := range api.ExtraLogLabels {
		labels[l.Label] = l.Value
	}
	id := ""
	if api.ID != 0 {
		id = strconv.FormatInt(api.ID, 10)
	}
	return FaroApp{
		AppType:               api.AppType,
		Runtime:               api.Runtime,
		ID:                    id,
		Name:                  api.Name,
		AppKey:                api.AppKey,
		CollectEndpointURL:    api.CollectEndpointURL,
		OTLPIngestEndpointURL: api.OTLPIngestEndpointURL,
		CORSOrigins:           api.CORSOrigins,
		ExtraLogLabels:        labels,
		Settings:              settingsFromAPI(api.Settings),
	}
}
