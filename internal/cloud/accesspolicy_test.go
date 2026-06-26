package cloud_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
)

func TestGCOMClient_CreateAccessPolicy(t *testing.T) {
	var gotRegion, gotMethod string
	var gotBody cloud.CreateAccessPolicyRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRegion = r.URL.Query().Get("region")
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloud.AccessPolicy{ID: "pol-1", Name: gotBody.Name, Scopes: gotBody.Scopes, Realms: gotBody.Realms})
	}))
	defer srv.Close()

	c, err := cloud.NewGCOMClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.CreateAccessPolicy(context.Background(), "us", cloud.CreateAccessPolicyRequest{
		Name:   "sigil-mystack",
		Scopes: []string{"sigil:write"},
		Realms: []cloud.Realm{{Type: "stack", Identifier: "42"}},
	})
	if err != nil {
		t.Fatalf("CreateAccessPolicy: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotRegion != "us" {
		t.Errorf("region query = %q, want us", gotRegion)
	}
	if got.ID != "pol-1" || got.Name != "sigil-mystack" {
		t.Errorf("policy = %+v", got)
	}
}

func TestGCOMClient_CreateAccessPolicy_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"taken"}`))
	}))
	defer srv.Close()

	c, _ := cloud.NewGCOMClient(srv.URL, "tok")
	_, err := c.CreateAccessPolicy(context.Background(), "us", cloud.CreateAccessPolicyRequest{Name: "x"})

	var httpErr *cloud.GCOMHTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusConflict {
		t.Fatalf("err = %v, want GCOMHTTPError 409", err)
	}
}

func TestGCOMClient_ListAccessPolicies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("region") != "eu" {
			t.Errorf("region query = %q", r.URL.Query().Get("region"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []cloud.AccessPolicy{{ID: "a"}, {ID: "b"}},
		})
	}))
	defer srv.Close()

	c, _ := cloud.NewGCOMClient(srv.URL, "tok")
	got, err := c.ListAccessPolicies(context.Background(), "eu")
	if err != nil {
		t.Fatalf("ListAccessPolicies: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestGCOMClient_CreateToken(t *testing.T) {
	var gotBody cloud.CreateTokenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloud.Token{ID: "tok-1", Name: gotBody.Name, Token: "glc_secret"})
	}))
	defer srv.Close()

	c, _ := cloud.NewGCOMClient(srv.URL, "tok")
	got, err := c.CreateToken(context.Background(), "us", cloud.CreateTokenRequest{AccessPolicyID: "pol-1", Name: "sigil-mystack"})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if got.Token != "glc_secret" {
		t.Errorf("token secret = %q", got.Token)
	}
	if gotBody.AccessPolicyID != "pol-1" {
		t.Errorf("accessPolicyId = %q", gotBody.AccessPolicyID)
	}
}

func TestGCOMClient_ListTokens_FiltersByPolicyAndName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("accessPolicyId") != "pol-1" || q.Get("name") != "sigil-mystack" {
			t.Errorf("query = %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []cloud.Token{{ID: "t1", Name: "sigil-mystack"}}})
	}))
	defer srv.Close()

	c, _ := cloud.NewGCOMClient(srv.URL, "tok")
	got, err := c.ListTokens(context.Background(), "us", "pol-1", "sigil-mystack")
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(got) != 1 || got[0].ID != "t1" {
		t.Errorf("tokens = %+v", got)
	}
}

func TestGCOMClient_DeleteToken(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, _ := cloud.NewGCOMClient(srv.URL, "tok")
	if err := c.DeleteToken(context.Background(), "us", "tok-1"); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/tokens/tok-1" {
		t.Errorf("path = %s", gotPath)
	}
}
