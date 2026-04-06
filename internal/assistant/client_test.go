package assistant_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant"
)

func TestFormatTimeContext(t *testing.T) {
	result := assistant.FormatTimeContext()

	for _, tag := range []string{"<context>", "</context>", "<time_iso_utc>", "<time_iso_local>", "<timezone>"} {
		if !strings.Contains(result, tag) {
			t.Errorf("FormatTimeContext() missing %s tag", tag)
		}
	}
}

func TestFormatTimeContext_ContainsValidTimestamps(t *testing.T) {
	result := assistant.FormatTimeContext()

	utcPattern := regexp.MustCompile(`<time_iso_utc>([^<]+)</time_iso_utc>`)
	utcMatch := utcPattern.FindStringSubmatch(result)
	if len(utcMatch) != 2 {
		t.Fatal("Could not extract UTC timestamp from FormatTimeContext()")
	}

	if _, err := time.Parse(time.RFC3339, utcMatch[1]); err != nil {
		t.Errorf("UTC timestamp %q is not valid RFC3339: %v", utcMatch[1], err)
	}

	if !strings.HasSuffix(utcMatch[1], "Z") {
		t.Errorf("UTC timestamp %q should end with Z", utcMatch[1])
	}

	localPattern := regexp.MustCompile(`<time_iso_local>([^<]+)</time_iso_local>`)
	localMatch := localPattern.FindStringSubmatch(result)
	if len(localMatch) != 2 {
		t.Fatal("Could not extract local timestamp from FormatTimeContext()")
	}

	if _, err := time.Parse(time.RFC3339, localMatch[1]); err != nil {
		t.Errorf("Local timestamp %q is not valid RFC3339: %v", localMatch[1], err)
	}
}

func TestFormatTimeContext_ContainsTimezone(t *testing.T) {
	result := assistant.FormatTimeContext()

	tzPattern := regexp.MustCompile(`<timezone>([^<]+)</timezone>`)
	tzMatch := tzPattern.FindStringSubmatch(result)
	if len(tzMatch) != 2 {
		t.Fatal("Could not extract timezone from FormatTimeContext()")
	}

	if tzMatch[1] == "" {
		t.Error("Timezone should not be empty")
	}

	if expected := time.Now().Location().String(); tzMatch[1] != expected {
		t.Errorf("Timezone = %q, want %q", tzMatch[1], expected)
	}
}

func TestFormatTimeContext_TimestampsAreRecent(t *testing.T) {
	before := time.Now().Add(-1 * time.Second)
	result := assistant.FormatTimeContext()
	after := time.Now().Add(1 * time.Second)

	utcPattern := regexp.MustCompile(`<time_iso_utc>([^<]+)</time_iso_utc>`)
	utcMatch := utcPattern.FindStringSubmatch(result)
	if len(utcMatch) != 2 {
		t.Fatal("Could not extract UTC timestamp")
	}

	parsedUTC, err := time.Parse(time.RFC3339, utcMatch[1])
	if err != nil {
		t.Fatalf("Failed to parse UTC timestamp: %v", err)
	}

	if parsedUTC.Before(before.UTC()) || parsedUTC.After(after.UTC()) {
		t.Errorf("UTC timestamp %v is not within expected range [%v, %v]",
			parsedUTC, before.UTC(), after.UTC())
	}
}

func TestNew_URLTrailingSlashRemoved(t *testing.T) {
	c := assistant.New(assistant.ClientOptions{
		GrafanaURL: "https://test.grafana.net/",
		Token:      "test-token",
	})

	if c.GetGrafanaURL() != "https://test.grafana.net" {
		t.Errorf("GetGrafanaURL() = %q, want trailing slash removed", c.GetGrafanaURL())
	}
}

func TestClient_GetToken(t *testing.T) {
	c := assistant.New(assistant.ClientOptions{
		GrafanaURL: "https://test.grafana.net",
		Token:      "secret-token",
	})

	if c.GetToken() != "secret-token" {
		t.Errorf("GetToken() = %q, want %q", c.GetToken(), "secret-token")
	}
}

func TestClient_SetLogger(t *testing.T) {
	c := assistant.New(assistant.ClientOptions{
		GrafanaURL: "https://test.grafana.net",
		Token:      "test-token",
	})

	c.SetLogger(assistant.NopLogger{})
}

func TestNopLogger(t *testing.T) {
	logger := assistant.NopLogger{}
	logger.Info("test")
	logger.Warning("test")
	logger.Debug("test")
}

func TestA2AEndpoints_Approval(t *testing.T) {
	baseURL := "https://test.grafana.net/api/plugins/grafana-assistant-app/resources/api/v1"
	endpoints := assistant.GetA2AEndpoints(baseURL)

	got := endpoints.Approval("test-approval-id")
	want := "https://test.grafana.net/api/plugins/grafana-assistant-app/resources/api/v1/a2a/approval/test-approval-id"

	if got != want {
		t.Errorf("Approval() = %q, want %q", got, want)
	}
}

func TestA2AEndpoints_DirectAPI(t *testing.T) {
	baseURL := "https://assistant-api.example.com/api/cli/v1"
	endpoints := assistant.GetA2AEndpoints(baseURL)

	got := endpoints.Approval("test-id")
	want := "https://assistant-api.example.com/api/cli/v1/a2a/approval/test-id"

	if got != want {
		t.Errorf("Approval() = %q, want %q", got, want)
	}
}

func TestNew_BaseURL_PluginProxy(t *testing.T) {
	c := assistant.New(assistant.ClientOptions{
		GrafanaURL: "https://test.grafana.net",
		Token:      "test-token",
	})

	want := "https://test.grafana.net/api/plugins/grafana-assistant-app/resources/api/v1"
	if c.GetBaseURL() != want {
		t.Errorf("GetBaseURL() = %q, want %q", c.GetBaseURL(), want)
	}
}

func TestNew_BaseURL_DirectAPI(t *testing.T) {
	c := assistant.New(assistant.ClientOptions{
		GrafanaURL:  "https://test.grafana.net",
		Token:       "gat_token",
		APIEndpoint: "https://assistant-api.example.com",
	})

	want := "https://assistant-api.example.com/api/cli/v1"
	if c.GetBaseURL() != want {
		t.Errorf("GetBaseURL() = %q, want %q", c.GetBaseURL(), want)
	}
}

func TestNew_BaseURL_DirectAPITrailingSlash(t *testing.T) {
	c := assistant.New(assistant.ClientOptions{
		GrafanaURL:  "https://test.grafana.net",
		Token:       "gat_token",
		APIEndpoint: "https://assistant-api.example.com/",
	})

	want := "https://assistant-api.example.com/api/cli/v1"
	if c.GetBaseURL() != want {
		t.Errorf("GetBaseURL() = %q, want %q", c.GetBaseURL(), want)
	}
}
