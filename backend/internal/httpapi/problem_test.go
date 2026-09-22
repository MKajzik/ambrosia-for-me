package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func TestWriteProblem(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteProblem(rec, http.StatusConflict, "ingredient_in_use", "used by 2 meals")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{
		"type":   "urn:mealplanner:problem:ingredient_in_use",
		"title":  "Conflict",
		"status": float64(409),
		"detail": "used by 2 meals",
		"code":   "ingredient_in_use",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
}

func TestWriteProblemOmitsEmptyDetail(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteProblem(rec, http.StatusNotFound, httpapi.CodeNotFound, "")

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["detail"]; ok {
		t.Errorf("detail present, want omitted: %v", body)
	}
}

func TestWriteValidationProblemListsFieldErrors(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteValidationProblem(rec, "", []httpapi.FieldError{
		{Field: "email", Code: httpapi.FieldInvalidForm},
		{Field: "password", Code: httpapi.FieldTooShort},
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	var body struct {
		Code   string `json:"code"`
		Errors []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != httpapi.CodeValidationFailed || len(body.Errors) != 2 ||
		body.Errors[0].Field != "email" || body.Errors[0].Code != "invalid_format" ||
		body.Errors[1].Field != "password" || body.Errors[1].Code != "too_short" {
		t.Errorf("body = %+v", body)
	}
}

func TestWriteValidationProblemOmitsEmptyErrors(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteValidationProblem(rec, "request body is required", nil)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["errors"]; ok {
		t.Errorf("errors present, want omitted: %v", body)
	}
	if body["detail"] != "request body is required" {
		t.Errorf("detail = %v", body["detail"])
	}
}
