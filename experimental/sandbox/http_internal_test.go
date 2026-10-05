package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	egress := []Destination{{Host: "Stack.grafana.net"}, {Host: "localhost:8443"}, {Host: "grafana:3000", AllowHTTP: true}, {Host: "devgrafana", AllowHTTP: true}}
	for _, tc := range []struct {
		url   string
		allow bool
	}{
		{"https://stack.grafana.net/api", true},
		{"https://stack.grafana.net:443/api", true},
		{"https://STACK.grafana.net/api", true},
		{"https://localhost:8443/x", true},
		{"https://stack.grafana.net:8443/api", false},
		{"https://other.grafana.net/api", false},
		{"https://evil.com/?stack.grafana.net", false},
		{"https://stack.grafana.net.evil.com/api", false},
		{"http://stack.grafana.net/api", false}, // https only
		{"https://localhost/x", false},
		// AllowHTTP opts a destination into plain http; the default port is 80.
		{"http://grafana:3000/api", true},
		{"https://grafana:3000/api", true},
		{"http://devgrafana/api", true},
		{"http://devgrafana:80/api", true},
		{"http://devgrafana:443/api", false},
		{"http://localhost:8443/x", false},
		{"ftp://stack.grafana.net/x", false},
	} {
		u, _ := url.Parse(tc.url)
		_, err := match(egress, u)
		if (err == nil) != tc.allow {
			t.Errorf("%s: allowed=%v, want %v (err %v)", tc.url, err == nil, tc.allow, err)
		}
	}
}

func TestRoundTripPolicy(t *testing.T) {
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append([]byte("echo:"), body...))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	// sendMethod returns the response body, or the error message the guest
	// would see.
	var sendMethod func(method, rawURL, hostHeader string) (body, errMsg string)
	var authorized []string
	authorize := func(r *http.Request) error {
		authorized = append(authorized, r.Method+" "+r.URL.Path+" auth="+r.Header.Get("Authorization"))
		if r.Method == http.MethodDelete {
			return errors.New("DELETE needs write access")
		}
		return nil
	}
	s := newSession([]Destination{{
		Host:   host,
		Header: http.Header{"authorization": {"Bearer host-secret"}},
	}}, authorize, srv.Client().Transport)

	send := func(rawURL, hostHeader string) (string, string) {
		t.Helper()
		return sendMethod(http.MethodPost, rawURL, hostHeader)
	}
	sendMethod = func(method, rawURL, hostHeader string) (string, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), method, rawURL, strings.NewReader("hi"))
		req.Header.Set("Authorization", "Bearer guest-supplied")
		if hostHeader != "" {
			req.Host = hostHeader
		}
		var raw bytes.Buffer
		if err := req.WriteProxy(&raw); err != nil { // what the guest sends
			t.Fatal(err)
		}
		out, errMsg := s.roundTrip(context.Background(), raw.Bytes())
		if errMsg != "" {
			return "", errMsg
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(out)), req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body), ""
	}

	body, errMsg := send(srv.URL+"/api/x", "")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if body != "echo:hi" {
		t.Errorf("body %q", body)
	}
	if gotAuth != "Bearer host-secret" {
		t.Errorf("server saw Authorization %q, want the host's", gotAuth)
	}

	for _, tc := range []struct{ url, hostHeader string }{
		{"https://other.example/x", ""},
		{"http://" + host + "/x", ""},
		// The guest's transport addresses requests to req.Host when set, so
		// overriding Host redirects the request, and egress judges that.
		{srv.URL + "/x", "other.example"},
	} {
		if _, errMsg := send(tc.url, tc.hostHeader); !strings.Contains(errMsg, "egress denied") {
			t.Errorf("%+v: got %q, want egress denied", tc, errMsg)
		}
	}
	// The authorizer sees each allowed request before credentials are added,
	// and its refusal stops the request.
	if got, want := authorized[0], "POST /api/x auth=Bearer guest-supplied"; got != want {
		t.Errorf("authorizer saw %q, want %q", got, want)
	}
	gotAuth = ""
	if _, errMsg := sendMethod(http.MethodDelete, srv.URL+"/api/x", ""); errMsg != "gcx_http: request refused: DELETE needs write access" {
		t.Errorf("DELETE: got %q, want refusal", errMsg)
	}
	if gotAuth != "" {
		t.Error("refused request reached the server")
	}

	if _, errMsg := s.roundTrip(context.Background(), []byte("garbage")); errMsg == "" {
		t.Error("want error for unparseable request")
	}
}
