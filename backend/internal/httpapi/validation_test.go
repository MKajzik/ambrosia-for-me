package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// validationError runs the real OpenAPI request validation for a request and
// returns the error kin-openapi produces.
func validationError(t *testing.T, method, path, body string) error {
	t.Helper()
	return validationErrorWithContentType(t, method, path, body, "application/json")
}

// validationErrorWithContentType allows overriding or omitting the Content-Type header.
// Pass an empty contentType to omit the header, or a custom value to use instead of "application/json".
func validationErrorWithContentType(t *testing.T, method, path, body, contentType string) error {
	t.Helper()
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	req := httptest.NewRequest(method, "http://localhost:8080/v1"+path, strings.NewReader(body))
	if body != "" && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	route, params, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("find route: %v", err)
	}
	return openapi3filter.ValidateRequest(context.Background(), &openapi3filter.RequestValidationInput{
		Request: req, PathParams: params, Route: route,
		Options: &openapi3filter.Options{
			MultiError:         true,
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
	})
}

func TestDescribeValidation(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		wantDetail string
		wantFields []string // "field:code", sorted by field
	}{
		{"missing required fields", "/auth/register", `{"email":"a@example.com"}`, "", []string{"display_name:required", "password:required"}},
		{"password too short", "/auth/register", `{"email":"a@example.com","password":"short","display_name":"A"}`, "", []string{"password:too_short"}},
		{"password too long", "/auth/register", `{"email":"a@example.com","password":"` + strings.Repeat("x", 129) + `","display_name":"A"}`, "", []string{"password:too_long"}},
		{"email is not an address", "/auth/login", `{"email":"nope","password":"x"}`, "", []string{"email:invalid_format"}},
		{"email with a display name", "/auth/login", `{"email":"Alice <a@example.com>","password":"x"}`, "", []string{"email:invalid_format"}},
		{"wrong type", "/auth/register", `{"email":"a@example.com","password":12345,"display_name":"A"}`, "", []string{"password:invalid_type"}},
		{"unknown field", "/auth/login", `{"email":"a@example.com","password":"x","admin":true}`, "", []string{"admin:unknown_field"}},
		{"several problems, sorted", "/auth/register", `{"email":"nope","password":"short","display_name":""}`, "", []string{"display_name:too_short", "email:invalid_format", "password:too_short"}},
		{"invalid JSON", "/auth/login", `{"email":`, "request body is not valid JSON", nil},
		{"empty body", "/auth/login", ``, "request body is required", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validationError(t, http.MethodPost, tt.path, tt.body)
			if err == nil {
				t.Fatal("validation passed, want an error")
			}

			res, ok := describeValidation(err)

			if !ok {
				t.Fatalf("describeValidation reported an unexpected error: %v", err)
			}
			if res.detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", res.detail, tt.wantDetail)
			}
			var got []string
			for _, f := range res.fields {
				got = append(got, f.Field+":"+f.Code)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantFields, ",") {
				t.Errorf("fields = %v, want %v", got, tt.wantFields)
			}
		})
	}
}

func TestDescribeValidationAcceptsAValidRequest(t *testing.T) {
	err := validationError(t, http.MethodPost, "/auth/register", `{"email":"a@example.com","password":"a-long-enough-pw","display_name":"A"}`)

	if err != nil {
		t.Errorf("a valid request failed validation: %v", err)
	}
}

func TestDescribeValidationRejectsErrorsItDoesNotUnderstand(t *testing.T) {
	if _, ok := describeValidation(errors.New("something else went wrong")); ok {
		t.Error("describeValidation claimed to understand an arbitrary error")
	}
}

func TestDescribeValidationMissingContentType(t *testing.T) {
	err := validationErrorWithContentType(t, http.MethodPost, "/auth/login", `{"email":"a@example.com","password":"x"}`, "")

	if err == nil {
		t.Fatal("validation passed, want an error")
	}

	res, ok := describeValidation(err)

	if !ok {
		t.Fatalf("describeValidation reported an unexpected error: %v", err)
	}
	if res.detail != "request body must be sent as application/json" {
		t.Errorf("detail = %q, want %q", res.detail, "request body must be sent as application/json")
	}
	if len(res.fields) != 0 {
		t.Errorf("fields = %v, want empty", res.fields)
	}
}

func TestDescribeValidationWrongContentType(t *testing.T) {
	err := validationErrorWithContentType(t, http.MethodPost, "/auth/login", `{"email":"a@example.com","password":"x"}`, "text/plain")

	if err == nil {
		t.Fatal("validation passed, want an error")
	}

	res, ok := describeValidation(err)

	if !ok {
		t.Fatalf("describeValidation reported an unexpected error: %v", err)
	}
	if res.detail != "request body must be sent as application/json" {
		t.Errorf("detail = %q, want %q", res.detail, "request body must be sent as application/json")
	}
	if len(res.fields) != 0 {
		t.Errorf("fields = %v, want empty", res.fields)
	}
}

func TestDescribeValidationBodyTooLarge(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		t.Fatalf("build router: %v", err)
	}

	body := strings.Repeat("x", 100)
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Wrap the body with MaxBytesReader to simulate a body that is too large
	rec := httptest.NewRecorder()
	req.Body = http.MaxBytesReader(rec, req.Body, 16)

	route, params, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("find route: %v", err)
	}

	validationErr := openapi3filter.ValidateRequest(context.Background(), &openapi3filter.RequestValidationInput{
		Request: req, PathParams: params, Route: route,
		Options: &openapi3filter.Options{
			MultiError:         true,
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
	})

	if validationErr == nil {
		t.Fatal("validation passed, want an error")
	}

	res, ok := describeValidation(validationErr)

	if !ok {
		t.Fatalf("describeValidation reported an unexpected error: %v", validationErr)
	}
	if res.detail != "request body is too large" {
		t.Errorf("detail = %q, want %q", res.detail, "request body is too large")
	}
	if len(res.fields) != 0 {
		t.Errorf("fields = %v, want empty", res.fields)
	}
}

func TestDescribeValidationFieldOutOfRange(t *testing.T) {
	// PATCH /me is a valid endpoint that accepts target_kcal field
	err := validationError(t, http.MethodPatch, "/me", `{"target_kcal":-5}`)

	if err == nil {
		t.Fatal("validation passed, want an error")
	}

	res, ok := describeValidation(err)

	if !ok {
		t.Fatalf("describeValidation reported an unexpected error: %v", err)
	}
	if len(res.fields) != 1 || res.fields[0].Field != "target_kcal" || res.fields[0].Code != "out_of_range" {
		t.Errorf("fields = %v, want one field target_kcal:out_of_range", res.fields)
	}
}

func TestDescribeValidationLongUnknownFieldName(t *testing.T) {
	longKey := strings.Repeat("x", 200)
	body := `{"email":"a@example.com","password":"x","` + longKey + `":true}`
	err := validationError(t, http.MethodPost, "/auth/login", body)

	if err == nil {
		t.Fatal("validation passed, want an error")
	}

	res, ok := describeValidation(err)

	if !ok {
		t.Fatalf("describeValidation reported an unexpected error: %v", err)
	}

	var unknownFieldFound bool
	for _, f := range res.fields {
		if f.Code == "unknown_field" {
			unknownFieldFound = true
			if n := len([]rune(f.Field)); n == 0 || n != 64 {
				t.Errorf("field name %q is %d runes, want exactly 64", f.Field, n)
			}
		}
	}
	if !unknownFieldFound {
		t.Error("no unknown_field error found in results")
	}
}

func TestDescribeValidationManyUnknownFields(t *testing.T) {
	// Create a JSON body with 30 distinct unknown fields
	bodyParts := []string{`{"email":"a@example.com","password":"x"`}
	for i := 0; i < 30; i++ {
		bodyParts = append(bodyParts, fmt.Sprintf(`,"unknown%d":true`, i))
	}
	bodyParts = append(bodyParts, "}")
	body := strings.Join(bodyParts, "")

	err := validationError(t, http.MethodPost, "/auth/login", body)

	if err == nil {
		t.Fatal("validation passed, want an error")
	}

	res, ok := describeValidation(err)

	if !ok {
		t.Fatalf("describeValidation reported an unexpected error: %v", err)
	}

	if len(res.fields) != 20 {
		t.Errorf("got %d field errors, want exactly 20", len(res.fields))
	}
}

func TestDescribeValidationUnrecognizedError(t *testing.T) {
	// Test that unrecognized RequestError with non-nil Err returns ok=false
	baseErr := errors.New("disk on fire")
	reqErr := &openapi3filter.RequestError{
		RequestBody: &openapi3.RequestBody{},
		Reason:      "reading failed",
		Err:         baseErr,
	}

	res, ok := describeValidation(reqErr)

	if ok {
		t.Errorf("describeValidation returned ok=true for unrecognized error, want false; got detail=%q", res.detail)
	}
}

func TestDescribeValidationOtherRequestErrorReason(t *testing.T) {
	// Test that RequestError with Err=nil and a non-Content-Type Reason returns ok=false
	reqErr := &openapi3filter.RequestError{
		RequestBody: &openapi3.RequestBody{},
		Reason:      "something else",
		Err:         nil,
	}

	res, ok := describeValidation(reqErr)

	if ok {
		t.Errorf("describeValidation returned ok=true for non-Content-Type error, want false; got detail=%q", res.detail)
	}
}

// The password is 9 characters because the spec requires at least 10.
func TestValidationErrorsDoNotContainTheOffendingValue(t *testing.T) {
	err := validationError(t, http.MethodPost, "/auth/register",
		`{"email":"a@example.com","password":"tiny-XYZ9","display_name":"A"}`)
	if err == nil {
		t.Fatal("expected a validation error for a too-short password")
	}

	if strings.Contains(err.Error(), "tiny-XYZ9") {
		t.Errorf("validation error leaks the offending value: %v", err)
	}
}
