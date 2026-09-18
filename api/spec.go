// Package api carries the OpenAPI description of aeman's REST surface.
package api

import _ "embed"

// Spec is the OpenAPI 3.0 document of the /api/v1 surface, as YAML.
//
//go:embed openapi.yaml
var Spec []byte
