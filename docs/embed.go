// Package docs embeds the OpenAPI specification the daemon serves at api/v1/openapi.json.
package docs

import _ "embed"

// OpenAPI is docs/openapi.json.
//
//go:embed openapi.json
var OpenAPI []byte
