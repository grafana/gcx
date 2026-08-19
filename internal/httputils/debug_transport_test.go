package httputils_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/grafana-app-sdk/logging"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// debugLogContext returns a context carrying a logger that writes Debug records
// to buf, using the same handler as the CLI so the message stays verbatim.
func debugLogContext(t *testing.T, buf *bytes.Buffer) context.Context {
	t.Helper()
	level := new(slog.LevelVar)
	level.Set(slog.LevelDebug)
	logger := logging.NewSLogLogger(logs.NewHandler(buf, &logs.Options{Level: level}))
	return logging.Context(t.Context(), logger)
}

// jsonResponse builds a complete response, so the dump writes a valid status
// line and a body.
func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Status:        http.StatusText(status),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

func TestRequestResponseLoggingRoundTripper_Dumps(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		reqBody   string
		respBody  string
		wantInLog []string
	}{
		{
			name:     "post dumps both bodies",
			method:   http.MethodPost,
			reqBody:  `{"name":"primary-rotation","type":"calendar"}`,
			respBody: `{"id":"S123","name":"primary-rotation"}`,
			wantInLog: []string{
				"http request dump",
				`{"name":"primary-rotation","type":"calendar"}`,
				"http response dump",
				`{"id":"S123","name":"primary-rotation"}`,
				// Only DumpRequestOut writes this request header.
				"Accept-Encoding: gzip",
			},
		},
		{
			name:     "get dumps the response body",
			method:   http.MethodGet,
			respBody: `{"results":[]}`,
			wantInLog: []string{
				"http request dump",
				"http response dump",
				`{"results":[]}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotReqBody string
			base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Body != nil {
					b, err := io.ReadAll(r.Body)
					if err != nil {
						return nil, err
					}
					gotReqBody = string(b)
				}
				return jsonResponse(r, http.StatusOK, tt.respBody), nil
			})

			var out bytes.Buffer
			ctx := debugLogContext(t, &out)

			var body io.Reader
			if tt.reqBody != "" {
				body = strings.NewReader(tt.reqBody)
			}
			req, err := http.NewRequestWithContext(ctx, tt.method, "http://example.com/api/schedules/", body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rt := &httputils.RequestResponseLoggingRoundTripper{DecoratedTransport: base}
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			logged := out.String()
			for _, want := range tt.wantInLog {
				if !strings.Contains(logged, want) {
					t.Errorf("log is missing %q\ngot:\n%s", want, logged)
				}
			}

			// The dump must not consume the request body.
			if gotReqBody != tt.reqBody {
				t.Errorf("base transport got request body %q, want %q", gotReqBody, tt.reqBody)
			}

			// The dump must not consume the response body.
			gotRespBody, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(gotRespBody) != tt.respBody {
				t.Errorf("caller got response body %q, want %q", gotRespBody, tt.respBody)
			}
		})
	}
}

// The dump is the innermost transport layer, so it must show a header that an
// outer layer added. internal/config/rest.go relies on this property to expose
// the OAuth bearer token.
func TestRequestResponseLoggingRoundTripper_DumpsHeadersFromOuterLayers(t *testing.T) {
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, http.StatusOK, `{}`), nil
	})
	dump := &httputils.RequestResponseLoggingRoundTripper{DecoratedTransport: base}
	outer := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer test-token")
		return dump.RoundTrip(r)
	})

	var out bytes.Buffer
	ctx := debugLogContext(t, &out)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/api", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, err := outer.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if !strings.Contains(out.String(), "Authorization: Bearer test-token") {
		t.Errorf("dump is missing the Authorization header\ngot:\n%s", out.String())
	}
}

func TestRequestResponseLoggingRoundTripper_TransportError(t *testing.T) {
	wantErr := errors.New("session expired")
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, wantErr
	})

	var out bytes.Buffer
	ctx := debugLogContext(t, &out)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/api", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rt := &httputils.RequestResponseLoggingRoundTripper{DecoratedTransport: base}
	resp, gotErr := rt.RoundTrip(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, gotErr)
	}

	logged := out.String()
	if !strings.Contains(logged, "http request dump") {
		t.Errorf("log is missing the request dump\ngot:\n%s", logged)
	}
	if strings.Contains(logged, "http response dump") {
		t.Errorf("log must hold no response dump after a transport error\ngot:\n%s", logged)
	}
}

func TestLoggingRoundTripper_Success(t *testing.T) {
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	rt := &httputils.LoggingRoundTripper{Base: base}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/api", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestLoggingRoundTripper_TransportError(t *testing.T) {
	wantErr := errors.New("connection refused")
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, wantErr
	})
	rt := &httputils.LoggingRoundTripper{Base: base}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/api", nil)

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestLoggingRoundTripper_5xx(t *testing.T) {
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: http.NoBody}, nil
	})
	rt := &httputils.LoggingRoundTripper{Base: base}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/api", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}
