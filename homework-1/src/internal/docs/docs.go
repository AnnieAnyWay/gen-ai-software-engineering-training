// Package docs serves the OpenAPI spec and a Swagger UI page.
package docs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var spec []byte

//go:embed swagger.html
var ui []byte

// Spec returns the embedded OpenAPI document.
func Spec() []byte { return spec }

func ServeSpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(spec)
}

func ServeUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(ui)
}
