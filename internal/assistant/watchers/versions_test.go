package watchers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestListVersionsOnePage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int
		before int64
		body   string
		count  int
		next   *int64
	}{
		{"empty", 20, 0, `{"revisions":[]}`, 0, nil},
		{"minimum", 1, 0, `{"revisions":[{"version":9}],"nextBeforeVersion":9}`, 1, new(int64(9))},
		{"gaps", 100, 8, `{"revisions":[{"version":7},{"version":3}],"nextBeforeVersion":3}`, 2, new(int64(3))},
		{"exhausted suffix", 20, 3, `{"revisions":[{"version":1}]}`, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, basePath+"/id%2Fwith%3Fcharacters/revisions", r.URL.EscapedPath())
				want := map[string][]string{"limit": {strconv.Itoa(tc.limit)}}
				if tc.before != 0 {
					want["beforeVersion"] = []string{strconv.FormatInt(tc.before, 10)}
				}
				assert.EqualValues(t, want, r.URL.Query())
				_, _ = fmt.Fprintf(w, `{"data":%s}`, tc.body)
			})
			page, err := client.ListVersions(t.Context(), "id/with?characters", tc.limit, tc.before)
			require.NoError(t, err)
			assert.Len(t, page.Items, tc.count)
			assert.NotNil(t, page.Items, "an empty page must remain an array")
			assert.Equal(t, tc.next, page.NextBeforeVersion)
			assert.Equal(t, int32(1), calls.Load(), "do not follow the continuation implicitly")
		})
	}
}

func TestListVersionsRejectsMalformedPage(t *testing.T) {
	for _, body := range []string{
		`SENTINEL`, `{}`, `{"data":null}`, `{"data":{}}`, `{"data":{"revisions":null}}`,
		`{"data":{"revisions":{}}}`, `{"data":{"revisions":[{"version":0}]}}`,
		`{"data":{"revisions":[{"version":-1}]}}`, `{"data":{"revisions":[{"version":9}]}}`,
		`{"data":{"revisions":[{"version":8}]}}`, `{"data":{"revisions":[{"version":2},{"version":3}]}}`,
		`{"data":{"revisions":[{"version":3},{"version":3}]}}`,
		`{"data":{"revisions":[{"version":7},{"version":6},{"version":5}]}}`,
		`{"data":{"revisions":[],"nextBeforeVersion":1}}`,
		`{"data":{"revisions":[{"version":3}],"nextBeforeVersion":0}}`,
		`{"data":{"revisions":[{"version":3}],"nextBeforeVersion":-1}}`,
		`{"data":{"revisions":[{"version":3}],"nextBeforeVersion":2}}`,
		`{"data":{"revisions":[{"version":3}],"nextBeforeVersion":8}}`,
		`{"data":{"revisions":[{"version":9223372036854775808}]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			page, err := client.ListVersions(t.Context(), "id", 2, 8)
			require.Error(t, err)
			assert.Nil(t, page)
			assert.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

func versionTestDetail(version, from int64) watchers.VersionDetail {
	return watchers.VersionDetail{
		VersionSummary: watchers.VersionSummary{
			Version: version, CreatedAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			Actor:      watchers.VersionActor{Type: "user", UserID: "user", Subject: "subject"},
			ChangeKind: "restore", ChangeSummary: "Example summary", ChangedFields: []string{"prompt"}, DefinitionHash: "hash",
			RollbackFromVersion: new(from), PreviousLearningEpoch: new(int64(2)), ResultingLearningEpoch: new(int64(3)),
			LearningTransition: json.RawMessage(`{"impact":"reset","futureNumber":9007199254740993}`),
		},
		Definition: json.RawMessage(`{"schemaVersion":99,"futureField":9007199254740993,"queries":[],"prompt":"Example"}`),
	}
}

func TestGetVersionPreservesDefinitionAndEvidence(t *testing.T) {
	want := versionTestDetail(8, 2)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, basePath+"/id%2Fwith%3Fcharacters/revisions/8", r.URL.EscapedPath())
		assert.Empty(t, r.URL.RawQuery)
		writeData(t, w, want)
	})
	item, err := client.GetVersion(t.Context(), "id/with?characters", 8)
	require.NoError(t, err)
	assert.Equal(t, want, *item)
	assert.Contains(t, string(item.Definition), "9007199254740993")
	assert.Contains(t, string(item.LearningTransition), "9007199254740993")
}

func TestGetVersionRejectsInvalidDetail(t *testing.T) {
	for _, body := range []string{
		`SENTINEL`, `{}`, `{"data":null}`, `{"data":{}}`, `{"data":[]}`,
		`{"data":{"version":2,"definition":{}}}`, `{"data":{"version":0,"definition":{}}}`,
		`{"data":{"version":1}}`, `{"data":{"version":1,"definition":null}}`,
		`{"data":{"version":1,"definition":[]}}`, `{"data":{"version":1,"definition":"SENTINEL"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			item, err := client.GetVersion(t.Context(), "id", 1)
			require.Error(t, err)
			assert.Nil(t, item)
			assert.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

func TestVersionInputValidationDoesNoIO(t *testing.T) {
	client := mutationClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") }))
	for _, limit := range []int{0, -1, 101} {
		_, err := client.ListVersions(t.Context(), "id", limit, 0)
		require.Error(t, err)
	}
	_, err := client.ListVersions(t.Context(), "id", 20, -1)
	require.Error(t, err)
	_, err = client.ListVersions(t.Context(), " ", 20, 0)
	require.Error(t, err)
	for _, version := range []int64{0, -1} {
		_, err = client.GetVersion(t.Context(), "id", version)
		require.Error(t, err)
		_, err = client.RestoreVersion(t.Context(), "id", version, 2, "")
		require.Error(t, err)
		_, err = client.RestoreVersion(t.Context(), "id", 1, version, "")
		require.Error(t, err)
	}
	_, err = client.GetVersion(t.Context(), "\t", 1)
	require.Error(t, err)
	_, err = client.RestoreVersion(t.Context(), "", 1, 2, "")
	require.Error(t, err)
	_, err = client.RestoreVersion(t.Context(), "id", 1, 2, strings.Repeat("\u754c", 1001))
	require.Error(t, err)
}

func TestVersionAPIErrorsRemainClassifiedAndSafe(t *testing.T) {
	methods := []struct {
		name string
		call func(*watchers.Client) error
	}{
		{"list", func(c *watchers.Client) error { _, err := c.ListVersions(t.Context(), "id", 20, 0); return err }},
		{"get", func(c *watchers.Client) error { _, err := c.GetVersion(t.Context(), "id", 1); return err }},
		{"restore", func(c *watchers.Client) error { _, err := c.RestoreVersion(t.Context(), "id", 1, 2, ""); return err }},
	}
	for _, method := range methods {
		for _, tc := range []struct {
			status int
			kind   error
		}{
			{401, watchers.ErrPermissionDenied}, {403, watchers.ErrPermissionDenied},
			{404, watchers.ErrNotFound}, {409, watchers.ErrConflict}, {422, watchers.ErrInvalidConfiguration},
		} {
			t.Run(method.name+"/"+strconv.Itoa(tc.status), func(t *testing.T) {
				client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, `{"message":"SENTINEL","details":"SENTINEL"}`)
				}))
				err := method.call(client)
				require.ErrorIs(t, err, tc.kind)
				apiErr, direct := err.(*watchers.APIError) //nolint:errorlint // Known API rejection stays direct for shared error conversion.
				require.True(t, direct)
				assert.Equal(t, tc.status, apiErr.StatusCode)
				assert.NotContains(t, err.Error(), "SENTINEL")
				if tc.status == 404 {
					assert.Contains(t, err.Error(), "pruned")
					assert.NotErrorIs(t, err, watchers.ErrCapabilityUnavailable)
				}
			})
		}
	}
}

func TestRestoreVersionWireAndUnicodeSummary(t *testing.T) {
	for _, summary := range []string{"", strings.Repeat("\u754c", 1000)} {
		t.Run(strconv.Itoa(len(summary)), func(t *testing.T) {
			want := versionTestDetail(3, 1)
			var calls atomic.Int32
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, basePath+"/id%2Fwith%3Fcharacters/revisions/1/restore", r.URL.EscapedPath())
				assert.Empty(t, r.URL.RawQuery)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var request map[string]json.RawMessage
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				assert.JSONEq(t, `2`, string(request["expectedDefinitionVersion"]))
				if summary == "" {
					assert.NotContains(t, request, "summary")
				} else {
					var got string
					assert.NoError(t, json.Unmarshal(request["summary"], &got))
					assert.Equal(t, summary, got)
				}
				w.WriteHeader(http.StatusCreated)
				writeData(t, w, want)
			}))
			item, err := client.RestoreVersion(t.Context(), "id/with?characters", 1, 2, summary)
			require.NoError(t, err)
			assert.Equal(t, want, *item)
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestRestoreInvalidAcceptanceIsUnknown(t *testing.T) {
	for _, body := range []string{
		`SENTINEL`, `{}`, `{"data":null}`, `{"data":{}}`, `{"data":[]}`,
		`{"data":{"version":3,"rollbackFromVersion":1,"definition":{}}`,
		`{"data":{"version":2,"rollbackFromVersion":1,"definition":{}}}`,
		`{"data":{"version":1,"rollbackFromVersion":1,"definition":{}}}`,
		`{"data":{"version":3,"definition":{}}}`,
		`{"data":{"version":3,"rollbackFromVersion":2,"definition":{}}}`,
		`{"data":{"version":3,"rollbackFromVersion":1,"definition":null}}`,
		`{"data":{"version":3,"rollbackFromVersion":1,"definition":[]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, body)
			}))
			item, err := client.RestoreVersion(t.Context(), "id", 1, 2, "")
			var unknown *watchers.UnknownRestoreError
			require.ErrorAs(t, err, &unknown)
			assert.Nil(t, item)
			assert.Contains(t, err.Error(), "may have applied")
			assert.NotContains(t, err.Error(), "SENTINEL")
			encoded, encodeErr := json.Marshal(err)
			require.NoError(t, encodeErr)
			assert.NotContains(t, string(encoded), "SENTINEL")
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func versionRetryClient(t *testing.T, host string, transport http.RoundTripper) *watchers.Client {
	t.Helper()
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
		Host: host,
		WrapTransport: func(base http.RoundTripper) http.RoundTripper {
			if transport != nil {
				base = transport
			}
			return &retry.Transport{Base: base, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
		},
	}})
	require.NoError(t, err)
	return watchers.NewClient(base)
}

func TestRestoreDoesNotRetryStatusOrTransportUncertainty(t *testing.T) {
	for _, status := range []int{429, 500, 501, 502, 503, 504} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"message":"SENTINEL"}`)
			}))
			t.Cleanup(server.Close)
			client := versionRetryClient(t, server.URL, nil)
			item, err := client.RestoreVersion(t.Context(), "id", 1, 2, "")
			require.Error(t, err)
			assert.Nil(t, item)
			var unknown *watchers.UnknownRestoreError
			if status >= 500 {
				require.ErrorAs(t, err, &unknown)
			} else {
				assert.NotErrorAs(t, err, &unknown)
			}
			assert.NotContains(t, err.Error(), "SENTINEL")
			assert.Equal(t, int32(1), calls.Load())
		})
	}
	for _, cause := range []error{&net.OpError{Op: "read", Net: "tcp", Err: errors.New("SENTINEL")}, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			var calls atomic.Int32
			client := versionRetryClient(t, "https://example.invalid", calibrationRoundTripper(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, cause
			}))
			_, err := client.RestoreVersion(t.Context(), "id", 1, 2, "")
			var unknown *watchers.UnknownRestoreError
			require.ErrorAs(t, err, &unknown)
			require.ErrorIs(t, err, cause)
			assert.NotContains(t, err.Error(), "SENTINEL")
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestRestoreCommittedThenTruncatedIsUnknown(t *testing.T) {
	var committed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		committed.Add(1) // Simulate a saved mutation before the response is lost.
		w.Header().Set("Content-Length", "200")
		_, _ = io.WriteString(w, `{"data":{"version":3,"rollbackFromVersion":1,"definition":{`)
	}))
	t.Cleanup(server.Close)
	client := versionRetryClient(t, server.URL, nil)
	item, err := client.RestoreVersion(t.Context(), "id", 1, 2, "")
	var unknown *watchers.UnknownRestoreError
	require.ErrorAs(t, err, &unknown)
	assert.Nil(t, item)
	assert.Equal(t, int32(1), committed.Load())
}
