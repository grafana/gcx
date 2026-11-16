package httputils

import (
	"bytes"
	"mime"
	"net/http"
	"strings"
)

// IsHTML reports whether a response is an HTML page, by its declared media
// type or, when that is missing or generic, by sniffing the body. Error
// rendering uses it to describe an HTML error page instead of printing it.
func IsHTML(contentType string, body []byte) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return true
	}
	return strings.HasPrefix(http.DetectContentType(bytes.TrimSpace(body)), "text/html")
}
