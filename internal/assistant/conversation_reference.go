package assistant

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode"
)

const sharedConversationPath = "/a/grafana-assistant-app/chats/shared/"

// ConversationReference identifies an ordinary conversation or a shared snapshot.
type ConversationReference struct {
	ID     string
	Shared bool

	parsedURL *url.URL
}

// ParseConversationReference accepts an opaque conversation ID or a shared Assistant URL.
func ParseConversationReference(input string) (ConversationReference, error) {
	if input == "" {
		return ConversationReference{}, errors.New("conversation reference must not be empty; use a conversation ID or shared Grafana Assistant URL")
	}

	if looksLikeConversationURL(input) {
		return parseSharedConversationURL(input)
	}

	if err := validateConversationID(input); err != nil {
		return ConversationReference{}, err
	}
	return ConversationReference{ID: input}, nil
}

func parseSharedConversationURL(input string) (ConversationReference, error) {
	parsed, err := url.Parse(input)
	if err != nil {
		return ConversationReference{}, fmt.Errorf("invalid conversation URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" || parsed.Host == "" || parsed.Opaque != "" {
		return ConversationReference{}, errors.New("invalid conversation URL: expected an absolute http or https URL")
	}
	if parsed.User != nil {
		return ConversationReference{}, errors.New("conversation URL must not contain credentials")
	}

	escapedPath := parsed.EscapedPath()
	marker := strings.LastIndex(escapedPath, sharedConversationPath)
	if marker < 0 {
		return ConversationReference{}, fmt.Errorf("unsupported conversation URL: expected %s<conversation-id>", sharedConversationPath)
	}
	escapedID := escapedPath[marker+len(sharedConversationPath):]
	if escapedID == "" || strings.Contains(escapedID, "/") {
		return ConversationReference{}, fmt.Errorf("unsupported conversation URL: expected exactly one ID segment after %s", sharedConversationPath)
	}
	id, err := url.PathUnescape(escapedID)
	if err != nil {
		return ConversationReference{}, fmt.Errorf("invalid conversation URL ID: %w", err)
	}
	if err := validateConversationID(id); err != nil {
		return ConversationReference{}, err
	}
	return ConversationReference{ID: id, Shared: true, parsedURL: parsed}, nil
}

// ValidateGrafanaURL ensures a shared URL belongs to the selected Grafana context.
func (r ConversationReference) ValidateGrafanaURL(grafanaURL string) error {
	if r.parsedURL == nil {
		return nil
	}
	configured, err := url.Parse(grafanaURL)
	if err != nil || configured.Scheme == "" || configured.Host == "" {
		return fmt.Errorf("invalid Grafana URL %q", grafanaURL)
	}

	expectedPath := path.Join("/", configured.Path, sharedConversationPath, r.ID)
	if normalizedOrigin(r.parsedURL) != normalizedOrigin(configured) || r.parsedURL.Path != expectedPath {
		return errors.New("conversation URL does not match the selected Grafana context; select the matching context and retry")
	}
	return nil
}

func looksLikeConversationURL(input string) bool {
	lower := strings.ToLower(input)
	return strings.Contains(input, "://") || strings.HasPrefix(lower, "http:") || strings.HasPrefix(lower, "https:")
}

func validateConversationID(id string) error {
	if id == "" || id == "." || id == ".." {
		return fmt.Errorf("invalid conversation ID %q", id)
	}
	if strings.ContainsAny(id, `/\\`) {
		return errors.New("conversation ID must be a single path segment")
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return errors.New("conversation ID must not contain control characters")
		}
	}
	return nil
}

func normalizedOrigin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host
}
