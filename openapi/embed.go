// Package openapi expone el contrato HTTP completo de la API.
package openapi

import _ "embed"

//go:embed openapi.yaml
var Spec []byte
