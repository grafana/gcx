// Package preferences provides Grafana organization preferences management.
package preferences

// ErrorResponse is the error body returned by the Grafana preferences API.
type ErrorResponse struct {
	Message string `json:"message"`
}

// NavbarItem represents a saved navigation bar item.
type NavbarItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	URL  string `json:"url"`
}

// NavbarPrefs holds navigation bar preferences.
type NavbarPrefs struct {
	SavedItems []NavbarItem `json:"savedItems,omitempty"`
}

// OrgPreferences represents Grafana organization preferences.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type OrgPreferences struct {
	Theme           string      `json:"theme,omitempty"`
	HomeDashboardID int         `json:"homeDashboardId,omitempty"`
	Timezone        string      `json:"timezone,omitempty"`
	WeekStart       string      `json:"weekStart,omitempty"`
	Locale          string      `json:"locale,omitempty"`
	Navbar          NavbarPrefs `json:"navbar"`
}

// GetResourceName returns the fixed singleton name.
func (p OrgPreferences) GetResourceName() string { return "default" }

// SetResourceName is a no-op because this is a singleton resource.
func (p *OrgPreferences) SetResourceName(_ string) {}
