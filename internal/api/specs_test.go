package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAsyncAPISpecEndpoint(t *testing.T) {
	state := newFakeState(t)
	srv := New(state)
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v0/asyncapi.yaml")
	if err != nil {
		t.Fatalf("GET /v0/asyncapi.yaml: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/yaml") {
		t.Errorf("Content-Type = %q, want text/yaml", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "asyncapi:") {
		t.Error("response body does not contain AsyncAPI spec")
	}
}

func TestOpenAPISpecEndpoint(t *testing.T) {
	state := newFakeState(t)
	srv := New(state)
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v0/openapi.yaml")
	if err != nil {
		t.Fatalf("GET /v0/openapi.yaml: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/yaml") {
		t.Errorf("Content-Type = %q, want text/yaml", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "openapi:") {
		t.Error("response body does not contain OpenAPI spec")
	}
}
