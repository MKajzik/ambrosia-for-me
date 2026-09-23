package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

const specPath = "../../../openapi.yaml"

// specRouter loads and validates openapi.yaml once for the whole test binary.
var specRouter = sync.OnceValues(func() (routers.Router, error) {
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		return nil, err
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, err
	}
	return gorillamux.NewRouter(doc)
})

// validToken is an access token stubTokens accepts (see also validToken2).
const validToken = "valid-token"

// validToken2 is a second accepted token, for a different user.
const validToken2 = "valid-token-2" //nolint:gosec // fake token, used only by tests

var (
	stubUserID  = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	stubUserID2 = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

type stubTokens struct{}

func (stubTokens) ParseAccess(token string) (uuid.UUID, error) {
	switch token {
	case validToken:
		return stubUserID, nil
	case validToken2:
		return stubUserID2, nil
	}
	return uuid.Nil, auth.ErrInvalidAccessToken
}

// stubAuth answers Login with invalid credentials and GetUser with a fixed
// user, and panics on anything else, so tests that must not reach the service
// fail loudly if they do.
type stubAuth struct{ httpapi.AuthService }

func (stubAuth) Login(context.Context, string, string) (service.Session, error) {
	return service.Session{}, service.ErrInvalidCredentials
}

func (stubAuth) GetUser(_ context.Context, id uuid.UUID) (service.User, error) {
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	return service.User{ID: id, Email: "stub@example.com", DisplayName: "Stub", CreatedAt: at, UpdatedAt: at}, nil
}

// stubIngredients panics on any call, so tests that must not reach the
// ingredients service fail loudly if they do.
type stubIngredients struct{ httpapi.IngredientsService }

// stubMeals panics on any call, so tests that must not reach the meals
// service fail loudly if they do.
type stubMeals struct{ httpapi.MealsService }

// stubDietTemplates panics on any call, so tests that must not reach the
// diet templates service fail loudly if they do.
type stubDietTemplates struct{ httpapi.DietTemplatesService }

// stubPlan panics on any call, so tests that must not reach the plan
// service fail loudly if they do.
type stubPlan struct{ httpapi.PlanService }

// stubShoppingLists panics on any call, so tests that must not reach the
// shopping lists service fail loudly if they do.
type stubShoppingLists struct{ httpapi.ShoppingListsService }

func init() {
	// kin-openapi ships no body decoder for text/event-stream, so validating
	// the events stream's response would fail as an unsupported content type.
	// Its schema is a plain string: the text/plain decoder is exactly right.
	openapi3filter.RegisterBodyDecoder("text/event-stream", openapi3filter.PlainBodyDecoder)
}

func newTestRouter(t *testing.T, mods ...func(*httpapi.Deps)) http.Handler {
	t.Helper()
	d := httpapi.Deps{
		Logger:        slog.New(slog.DiscardHandler),
		Ready:         func(context.Context) error { return nil },
		WebOrigin:     "http://localhost:3000",
		Auth:          stubAuth{},
		Ingredients:   stubIngredients{},
		Meals:         stubMeals{},
		DietTemplates: stubDietTemplates{},
		Plan:          stubPlan{},
		ShoppingLists: stubShoppingLists{},
		Tokens:        stubTokens{},
	}
	for _, m := range mods {
		m(&d)
	}
	return httpapi.NewRouter(d)
}

func newRouter(t *testing.T, ready func(context.Context) error) http.Handler {
	t.Helper()
	return newTestRouter(t, func(d *httpapi.Deps) { d.Ready = ready })
}

func alwaysReady(context.Context) error { return nil }

type request struct {
	body       string
	bearer     string
	remoteAddr string
	skipReqVal bool
}

type requestOption func(*request)

func withBody(json string) requestOption    { return func(r *request) { r.body = json } }
func withBearer(token string) requestOption { return func(r *request) { r.bearer = token } }
func withRemoteAddr(addr string) requestOption {
	return func(r *request) { r.remoteAddr = addr }
}

// withInvalidRequest skips validating the request itself against the
// contract, for tests that deliberately send a bad request. The response is
// still validated.
func withInvalidRequest() requestOption { return func(r *request) { r.skipReqVal = true } }

// contract sends the request through handler and fails the test unless the
// request (unless withInvalidRequest) and the response conform to
// openapi.yaml. It returns the response.
func contract(t *testing.T, handler http.Handler, method, path string, opts ...requestOption) *httptest.ResponseRecorder {
	t.Helper()
	ctx := context.Background()
	var cfg request
	for _, o := range opts {
		o(&cfg)
	}

	oaRouter, err := specRouter()
	if err != nil {
		t.Fatalf("load %s: %v", specPath, err)
	}

	var body io.Reader
	if cfg.body != "" {
		body = strings.NewReader(cfg.body)
	}
	req := httptest.NewRequest(method, "http://localhost:8080/v1"+path, body)
	if cfg.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cfg.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.bearer)
	}
	if cfg.remoteAddr != "" {
		req.RemoteAddr = cfg.remoteAddr
	}

	route, pathParams, err := oaRouter.FindRoute(req)
	if err != nil {
		if errors.Is(err, routers.ErrPathNotFound) {
			t.Fatalf("%s %s is not declared in openapi.yaml", method, path)
		}
		t.Fatalf("find route: %v", err)
	}
	opts2 := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	in := &openapi3filter.RequestValidationInput{Request: req, PathParams: pathParams, Route: route, Options: opts2}
	if !cfg.skipReqVal {
		if err := openapi3filter.ValidateRequest(ctx, in); err != nil {
			t.Fatalf("request does not match contract: %v", err)
		}
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

func TestReadyzGivesUpOnAHungDatabase(t *testing.T) {
	hung := func(ctx context.Context) error {
		<-ctx.Done() // a wedged database never answers; only the deadline ends the wait
		return ctx.Err()
	}

	rec := contract(t, newRouter(t, hung), http.MethodGet, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestNewRouterPanicsWithoutRequiredDependencies(t *testing.T) {
	full := httpapi.Deps{
		Logger: slog.New(slog.DiscardHandler), Ready: alwaysReady,
		WebOrigin: "http://localhost:3000", Auth: stubAuth{}, Ingredients: stubIngredients{}, Meals: stubMeals{}, Tokens: stubTokens{}, DietTemplates: stubDietTemplates{}, Plan: stubPlan{},
		ShoppingLists: stubShoppingLists{},
	}
	tests := map[string]func(*httpapi.Deps){
		"no logger":           func(d *httpapi.Deps) { d.Logger = nil },
		"no ready":            func(d *httpapi.Deps) { d.Ready = nil },
		"no auth":             func(d *httpapi.Deps) { d.Auth = nil },
		"no ingredients":      func(d *httpapi.Deps) { d.Ingredients = nil },
		"no meals":            func(d *httpapi.Deps) { d.Meals = nil },
		"no diet templates":   func(d *httpapi.Deps) { d.DietTemplates = nil },
		"no plan":             func(d *httpapi.Deps) { d.Plan = nil },
		"no shopping lists":   func(d *httpapi.Deps) { d.ShoppingLists = nil },
		"no tokens":           func(d *httpapi.Deps) { d.Tokens = nil },
		"empty web origin":    func(d *httpapi.Deps) { d.WebOrigin = "" },
		"wildcard web origin": func(d *httpapi.Deps) { d.WebOrigin = "*" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			d := full
			mutate(&d)
			defer func() {
				if recover() == nil {
					t.Error("NewRouter did not panic")
				}
			}()
			httpapi.NewRouter(d)
		})
	}
}

// failingLoginAuth makes Login fail with an error that carries a secret-looking
// detail, to prove an unexpected service failure is a declared, opaque problem.
type failingLoginAuth struct{ httpapi.AuthService }

func (failingLoginAuth) Login(context.Context, string, string) (service.Session, error) {
	return service.Session{}, errors.New("boom: secret detail")
}

func TestHandlerFailuresAreDeclaredProblems(t *testing.T) {
	h := newTestRouter(t, func(d *httpapi.Deps) { d.Auth = failingLoginAuth{} })

	rec := contract(t, h, http.MethodPost, "/auth/login",
		withBody(`{"email":"a@example.com","password":"x"}`))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := decodeProblemBody(t, rec).Code; got != "internal_error" {
		t.Errorf("problem code = %q, want internal_error", got)
	}
	body := rec.Body.String()
	if strings.Contains(body, "boom") || strings.Contains(body, "secret detail") {
		t.Errorf("response leaks the underlying error: %s", body)
	}
}
