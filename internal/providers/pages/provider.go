package pages

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &PagesProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&PagesProvider{})
}

// PagesProvider builds and publishes Grafana Pages. It is commands-only.
// Grafana serves Page objects (pages.v0alpha1.assistant.ext.grafana.app),
// but they hold only a page's catalog details, not the page itself, so it
// registers no adapter for them and needs no configuration of its own.
type PagesProvider struct{}

// Name returns the unique identifier for this provider.
func (p *PagesProvider) Name() string { return "pages" }

// ShortDesc returns a one-line description of the provider.
func (p *PagesProvider) ShortDesc() string { return "Write, build and publish Grafana Pages" }

// Commands returns the experimental gcx pages tree.
func (p *PagesProvider) Commands() []*cobra.Command { return []*cobra.Command{Commands()} }

// Validate checks that the given provider configuration is valid.
func (p *PagesProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns the configuration keys used by this provider.
func (p *PagesProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns no adapters: Page objects hold only catalog
// details.
func (p *PagesProvider) TypedRegistrations() []adapter.Registration { return nil }
