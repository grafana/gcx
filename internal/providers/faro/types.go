// Package faro provides a client and resource adapter for Grafana Frontend Observability (Faro).
package faro

import (
	"strconv"

	"github.com/grafana/gcx/internal/resources/adapter"
)

// FaroApp represents a Frontend Observability application.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type FaroApp struct {
	ID                 string            `json:"id,omitempty"`
	Name               string            `json:"name"`
	AppKey             string            `json:"appKey,omitempty"`
	CollectEndpointURL string            `json:"collectEndpointURL,omitempty"`
	CORSOrigins        []CORSOrigin      `json:"corsOrigins,omitempty"`
	ExtraLogLabels     map[string]string `json:"extraLogLabels,omitempty"`
	Settings           *FaroAppSettings  `json:"settings,omitempty"`
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
	ID                 int64            `json:"id,omitempty"`
	Name               string           `json:"name"`
	AppKey             string           `json:"appKey,omitempty"`
	CollectEndpointURL string           `json:"collectEndpointURL,omitempty"`
	CORSOrigins        []CORSOrigin     `json:"corsOrigins,omitempty"`
	ExtraLogLabels     []LogLabel       `json:"extraLogLabels,omitempty"`
	Settings           *FaroAppSettings `json:"settings,omitempty"`
}

// LogLabel represents a key-value log label for the API.
type LogLabel struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CORSOrigin represents an allowed CORS origin.
type CORSOrigin struct {
	URL string `json:"url"`
}

// FaroAppSettings represents Faro app settings.
type FaroAppSettings struct {
	GeolocationEnabled bool   `json:"geolocationEnabled,omitempty"`
	GeolocationLevel   string `json:"geolocationLevel,omitempty"` // "country", "region", "city"
}

// toAPI converts FaroApp to API wire format.
func (app *FaroApp) toAPI() faroAppAPI {
	labels := make([]LogLabel, 0, len(app.ExtraLogLabels))
	for k, v := range app.ExtraLogLabels {
		labels = append(labels, LogLabel{Key: k, Value: v})
	}
	var id int64
	if app.ID != "" {
		id, _ = strconv.ParseInt(app.ID, 10, 64)
	}
	return faroAppAPI{
		ID:                 id,
		Name:               app.Name,
		AppKey:             app.AppKey,
		CollectEndpointURL: app.CollectEndpointURL,
		CORSOrigins:        app.CORSOrigins,
		ExtraLogLabels:     labels,
		Settings:           app.Settings,
	}
}

// fromAPI converts API wire format to FaroApp.
func fromAPI(api faroAppAPI) FaroApp {
	labels := make(map[string]string, len(api.ExtraLogLabels))
	for _, l := range api.ExtraLogLabels {
		labels[l.Key] = l.Value
	}
	id := ""
	if api.ID != 0 {
		id = strconv.FormatInt(api.ID, 10)
	}
	return FaroApp{
		ID:                 id,
		Name:               api.Name,
		AppKey:             api.AppKey,
		CollectEndpointURL: api.CollectEndpointURL,
		CORSOrigins:        api.CORSOrigins,
		ExtraLogLabels:     labels,
		Settings:           api.Settings,
	}
}
