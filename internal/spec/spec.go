// Package spec вшиває публічний контракт API PRRO.cloud — копію
// testdata/openapi.yaml із prro-go тієї версії, від якої залежить сервер.
// Оновлюється вручну: make sync-spec.
package spec

import _ "embed"

// OpenAPI — вміст openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
