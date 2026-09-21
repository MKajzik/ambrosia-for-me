package httpapi

import (
	"encoding/json"
	"net/http"
)

// Stable, machine-readable problem codes. Clients map these to localized text.
const (
	CodeValidationFailed = "validation_failed"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeNotReady         = "not_ready"
	CodeInternal         = "internal_error"
)

// problem is an RFC 9457 problem details document plus a stable code.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code"`
}

// WriteProblem writes an application/problem+json response. detail may be empty.
func WriteProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:   "urn:mealplanner:problem:" + code,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	})
}
