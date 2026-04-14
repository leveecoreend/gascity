package api

import (
	"net/http"

	"github.com/gastownhall/gascity/contracts"
)

// handleAsyncAPISpec serves the AsyncAPI YAML spec for the WebSocket protocol.
func handleAsyncAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(contracts.AsyncAPISpec) //nolint:errcheck
}

// handleOpenAPISpec serves the OpenAPI YAML spec for the HTTP endpoints.
func handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(contracts.OpenAPISpec) //nolint:errcheck
}
