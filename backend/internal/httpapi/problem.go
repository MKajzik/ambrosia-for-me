package httpapi

import (
	"encoding/json"
	"net/http"
)

// Stable, machine-readable problem codes. Clients map these to localized text.
const (
	CodeValidationFailed    = "validation_failed"
	CodeNotFound            = "not_found"
	CodeMethodNotAllowed    = "method_not_allowed"
	CodeNotReady            = "not_ready"
	CodeInternal            = "internal_error"
	CodeUnauthorized        = "unauthorized"
	CodeRateLimited         = "rate_limited"
	CodeEmailTaken          = "email_taken"
	CodeInvalidCredentials  = "invalid_credentials" //nolint:gosec // an error code, not a credential
	CodeInvalidRefreshToken = "invalid_refresh_token"
)

// Stable codes for FieldError.Code.
const (
	FieldRequired     = "required"
	FieldTooShort     = "too_short"
	FieldTooLong      = "too_long"
	FieldInvalidType  = "invalid_type"
	FieldInvalidForm  = "invalid_format"
	FieldInvalidValue = "invalid_value"
	FieldOutOfRange   = "out_of_range"
	FieldUnknown      = "unknown_field"
)

// FieldError describes one invalid field of a request body.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// problem is an RFC 9457 problem details document plus a stable code.
type problem struct {
	Type   string       `json:"type"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Code   string       `json:"code"`
	Errors []FieldError `json:"errors,omitempty"`
}

// WriteProblem writes an application/problem+json response. detail may be empty.
func WriteProblem(w http.ResponseWriter, status int, code, detail string) {
	writeProblem(w, problem{
		Type:   "urn:mealplanner:problem:" + code,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	})
}

// WriteValidationProblem writes a 400 validation_failed problem listing the
// invalid fields. detail and fields may each be empty.
func WriteValidationProblem(w http.ResponseWriter, detail string, fields []FieldError) {
	writeProblem(w, problem{
		Type:   "urn:mealplanner:problem:" + CodeValidationFailed,
		Title:  http.StatusText(http.StatusBadRequest),
		Status: http.StatusBadRequest,
		Detail: detail,
		Code:   CodeValidationFailed,
		Errors: fields,
	})
}

func writeProblem(w http.ResponseWriter, p problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
