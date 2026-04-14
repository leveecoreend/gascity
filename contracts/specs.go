// Package contracts embeds API specification files for runtime serving.
package contracts

import _ "embed"

//go:embed supervisor-ws/asyncapi.yaml
var AsyncAPISpec []byte

//go:embed http/openapi.yaml
var OpenAPISpec []byte
