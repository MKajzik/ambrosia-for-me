// Package httpapi holds the HTTP layer: routing, request decoding and response encoding.
package httpapi

import (
	"encoding/json"
	"net/http"
)

// NewRouter returns the root handler with all /v1 routes mounted.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/healthz", healthz)
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
