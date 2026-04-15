package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestOpenAPISpecContainsOnlyHTTPSurvivorPaths(t *testing.T) {
	state := newFakeState(t)
	srv := New(state)
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v0/openapi.yaml")
	if err != nil {
		t.Fatalf("GET /v0/openapi.yaml: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var doc struct {
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal openapi yaml: %v", err)
	}

	var got []string
	for path := range doc.Paths {
		got = append(got, path)
	}
	sort.Strings(got)

	want := []string{
		"/health",
		"/v0/asyncapi.yaml",
		"/v0/city",
		"/v0/openapi.yaml",
		"/v0/provider-readiness",
		"/v0/readiness",
		"/v0/ws",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("openapi paths = %v, want %v", got, want)
	}
}

func TestAsyncAPISpecContainsExpectedProtocolChannels(t *testing.T) {
	state := newFakeState(t)
	srv := New(state)
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v0/asyncapi.yaml")
	if err != nil {
		t.Fatalf("GET /v0/asyncapi.yaml: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var doc struct {
		Channels map[string]any `yaml:"channels"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal asyncapi yaml: %v", err)
	}

	var gotProtocol []string
	for path := range doc.Channels {
		if strings.HasPrefix(path, "protocol/") {
			gotProtocol = append(gotProtocol, path)
		}
	}
	sort.Strings(gotProtocol)

	wantProtocol := []string{
		"protocol/error",
		"protocol/event",
		"protocol/hello",
		"protocol/subscription-start",
		"protocol/subscription-stop",
	}
	if strings.Join(gotProtocol, "\n") != strings.Join(wantProtocol, "\n") {
		t.Fatalf("asyncapi protocol channels = %v, want %v", gotProtocol, wantProtocol)
	}

	if _, ok := doc.Channels["actions/cities.list/request"]; !ok {
		t.Fatal("asyncapi missing supervisor action channel actions/cities.list/request")
	}
	if _, ok := doc.Channels["actions/health.get/request"]; ok {
		t.Fatal("asyncapi advertised HTTP-only action channel actions/health.get/request")
	}
}

func TestAsyncAPISpecSubscriptionResumeSchema(t *testing.T) {
	doc := fetchAsyncAPIDoc(t)

	schema, ok := doc.Components.Schemas["ApiSubscriptionStartPayload"]
	if !ok {
		t.Fatal("asyncapi missing ApiSubscriptionStartPayload schema")
	}

	assertSchemaProperty(t, schema.Properties, "after_cursor", "string", "Resume from this cursor")
	assertSchemaProperty(t, schema.Properties, "after_seq", "integer", "Resume from this event sequence")
	assertSchemaProperty(t, schema.Properties, "format", "string", "Stream format")
	assertSchemaProperty(t, schema.Properties, "kind", "string", "Subscription type")
	assertSchemaProperty(t, schema.Properties, "target", "string", "Session ID or name")
	assertSchemaProperty(t, schema.Properties, "turns", "integer", "Most recent N turns")

	if got := schema.Properties["after_seq"].Minimum; got != 0 {
		t.Fatalf("ApiSubscriptionStartPayload.after_seq minimum = %v, want 0", got)
	}
	if desc := schema.Properties["kind"].Description; !strings.Contains(desc, "events") || !strings.Contains(desc, "session.stream") {
		t.Fatalf("ApiSubscriptionStartPayload.kind description = %q, want events + session.stream", desc)
	}
	if desc := schema.Properties["format"].Description; !strings.Contains(desc, "text") || !strings.Contains(desc, "raw") || !strings.Contains(desc, "jsonl") {
		t.Fatalf("ApiSubscriptionStartPayload.format description = %q, want text/raw/jsonl", desc)
	}
}

func TestAsyncAPISpecEventAndHelloSchemas(t *testing.T) {
	doc := fetchAsyncAPIDoc(t)

	eventSchema, ok := doc.Components.Schemas["ApiEventEnvelope"]
	if !ok {
		t.Fatal("asyncapi missing ApiEventEnvelope schema")
	}
	assertSchemaProperty(t, eventSchema.Properties, "cursor", "string", "Resume cursor for reconnection")
	assertSchemaProperty(t, eventSchema.Properties, "subscription_id", "string", "Subscription that produced this event")

	helloSchema, ok := doc.Components.Schemas["ApiHelloEnvelope"]
	if !ok {
		t.Fatal("asyncapi missing ApiHelloEnvelope schema")
	}
	assertSchemaProperty(t, helloSchema.Properties, "capabilities", "array", "Sorted list of supported action names")
	assertSchemaProperty(t, helloSchema.Properties, "subscription_kinds", "array", "Supported subscription types")
	assertSchemaProperty(t, helloSchema.Properties, "server_role", "string", "'city' or 'supervisor'")
}

type asyncAPIDoc struct {
	Channels map[string]any `yaml:"channels"`
	Components struct {
		Schemas map[string]asyncAPISchema `yaml:"schemas"`
	} `yaml:"components"`
}

type asyncAPISchema struct {
	Description string                     `yaml:"description"`
	Type        any                        `yaml:"type"`
	Minimum     any                        `yaml:"minimum"`
	Properties  map[string]asyncAPISchema  `yaml:"properties"`
	Items       *asyncAPISchema            `yaml:"items"`
}

func fetchAsyncAPIDoc(t *testing.T) asyncAPIDoc {
	t.Helper()

	state := newFakeState(t)
	srv := New(state)
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v0/asyncapi.yaml")
	if err != nil {
		t.Fatalf("GET /v0/asyncapi.yaml: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var doc asyncAPIDoc
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal asyncapi yaml: %v", err)
	}
	return doc
}

func assertSchemaProperty(t *testing.T, props map[string]asyncAPISchema, name, wantType, wantDesc string) {
	t.Helper()

	prop, ok := props[name]
	if !ok {
		t.Fatalf("schema missing property %q", name)
	}
	if !schemaTypeIncludes(prop.Type, wantType) {
		t.Fatalf("property %s type = %#v, want %s", name, prop.Type, wantType)
	}
	if !strings.Contains(prop.Description, wantDesc) {
		t.Fatalf("property %s description = %q, want substring %q", name, prop.Description, wantDesc)
	}
}

func schemaTypeIncludes(v any, want string) bool {
	switch tv := v.(type) {
	case string:
		return tv == want
	case []any:
		for _, item := range tv {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}
