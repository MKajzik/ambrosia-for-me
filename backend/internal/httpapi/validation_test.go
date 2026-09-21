package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// validationError runs the real OpenAPI request validation for a request and
// returns the error kin-openapi produces.
func validationError(t *testing.T, method, path, body string) error {
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
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
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
