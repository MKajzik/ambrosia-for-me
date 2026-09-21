package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

const specPath = "../../../openapi.yaml"

func newRouter(t *testing.T, ready func(context.Context) error) http.Handler {
	t.Helper()
	return httpapi.NewRouter(httpapi.Deps{
		Logger:    slog.New(slog.DiscardHandler),
		Ready:     ready,
		WebOrigin: "http://localhost:3000",
	})
}

func alwaysReady(context.Context) error { return nil }

// contract sends the request through handler and fails the test unless both
// the request and the response conform to openapi.yaml. It returns the response.
func contract(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := context.Background()

	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load %s: %v", specPath, err)
	}
	if err := doc.Validate(ctx); err != nil {
		t.Fatalf("openapi.yaml is not a valid OpenAPI document: %v", err)
	}
	oaRouter, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build spec router: %v", err)
	}

	req := httptest.NewRequest(method, "http://localhost:8080/v1"+path, nil)
	route, pathParams, err := oaRouter.FindRoute(req)
	if err != nil {
		if errors.Is(err, routers.ErrPathNotFound) {
			t.Fatalf("%s %s is not declared in openapi.yaml", method, path)
		}
		t.Fatalf("find route: %v", err)
	}
	opts := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	in := &openapi3filter.RequestValidationInput{Request: req, PathParams: pathParams, Route: route, Options: opts}
	if err := openapi3filter.ValidateRequest(ctx, in); err != nil {
		t.Fatalf("request does not match contract: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if err := openapi3filter.ValidateResponse(ctx, out); err != nil {
		t.Fatalf("response %d %q does not match contract: %v", rec.Code, rec.Body.String(), err)
	}
	return rec
}

func TestHealthzMatchesContract(t *testing.T) {
	rec := contract(t, newRouter(t, alwaysReady), http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzMatchesContract(t *testing.T) {
	tests := []struct {
		name       string
		ready      func(context.Context) error
		wantStatus int
		wantCT     string
	}{
		{"ready", alwaysReady, http.StatusOK, "application/json"},
		{
			"database down",
			func(context.Context) error { return errors.New("connection refused") },
			http.StatusServiceUnavailable,
			"application/problem+json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := contract(t, newRouter(t, tt.ready), http.MethodGet, "/readyz")

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantCT {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantCT)
			}
		})
	}
}
