package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

type problemBody struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Errors []struct {
		Field string `json:"field"`
		Code  string `json:"code"`
	} `json:"errors"`
}

func decodeProblemBody(t *testing.T, rec *httptest.ResponseRecorder) problemBody {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json (body %s)", got, rec.Body.String())
	}
	var p problemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return p
}

func TestRequestValidationProblems(t *testing.T) {
	longPassword := strings.Repeat("x", 129)
	tests := []struct {
		name       string
		path       string
		body       string
		wantDetail string
		wantFields []string // "field:code", sorted by field
	}{
		{
			name:       "missing required fields",
			path:       "/auth/register",
			body:       `{"email":"a@example.com"}`,
			wantFields: []string{"display_name:required", "password:required"},
		},
		{
			name:       "password too short",
			path:       "/auth/register",
			body:       `{"email":"a@example.com","password":"short","display_name":"A"}`,
			wantFields: []string{"password:too_short"},
		},
		{
			name:       "password too long",
			path:       "/auth/register",
			body:       `{"email":"a@example.com","password":"` + longPassword + `","display_name":"A"}`,
			wantFields: []string{"password:too_long"},
		},
		{
			name:       "email is not an address",
			path:       "/auth/register",
			body:       `{"email":"not-an-email","password":"long-enough-password","display_name":"A"}`,
			wantFields: []string{"email:invalid_format"},
		},
		{
			name:       "email with a display name is rejected",
			path:       "/auth/login",
			body:       `{"email":"Alice <a@example.com>","password":"x"}`,
			wantFields: []string{"email:invalid_format"},
		},
		{
			name:       "wrong type",
			path:       "/auth/register",
			body:       `{"email":"a@example.com","password":12345,"display_name":"A"}`,
			wantFields: []string{"password:invalid_type"},
		},
		{
			name:       "unknown field",
			path:       "/auth/login",
			body:       `{"email":"a@example.com","password":"x","admin":true}`,
			wantFields: []string{"admin:unknown_field"},
		},
		{
			name:       "several problems are all reported",
			path:       "/auth/register",
			body:       `{"email":"nope","password":"short","display_name":""}`,
			wantFields: []string{"display_name:too_short", "email:invalid_format", "password:too_short"},
		},
		{name: "invalid JSON", path: "/auth/login", body: `{"email":`, wantDetail: "request body is not valid JSON"},
		{name: "empty body", path: "/auth/login", body: ``, wantDetail: "request body is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := contract(t, newTestRouter(t), http.MethodPost, tt.path, withBody(tt.body), withInvalidRequest())

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			p := decodeProblemBody(t, rec)
			if p.Code != httpapi.CodeValidationFailed {
				t.Errorf("code = %q, want %q", p.Code, httpapi.CodeValidationFailed)
			}
			if tt.wantDetail != "" && p.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", p.Detail, tt.wantDetail)
			}
			var got []string
			for _, e := range p.Errors {
				got = append(got, e.Field+":"+e.Code)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantFields, ",") {
				t.Errorf("errors = %v, want %v", got, tt.wantFields)
			}
		})
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	big := `{"email":"a@example.com","password":"` + strings.Repeat("x", 70<<10) + `"}`

	rec := contract(t, newTestRouter(t), http.MethodPost, "/auth/login", withBody(big), withInvalidRequest())

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if p := decodeProblemBody(t, rec); p.Detail != "request body is too large" {
		t.Errorf("detail = %q, want %q", p.Detail, "request body is too large")
	}
}

func TestSecuredOperationsRequireAValidAccessToken(t *testing.T) {
	router := newTestRouter(t)
	tests := []struct {
		name   string
		method string
		path   string
		opts   []requestOption
		want   int
	}{
		{"GET /me without a token", http.MethodGet, "/me", nil, http.StatusUnauthorized},
		{"GET /me with an invalid token", http.MethodGet, "/me", []requestOption{withBearer("garbage")}, http.StatusUnauthorized},
		{"DELETE /me without a token", http.MethodDelete, "/me", nil, http.StatusUnauthorized},
		{
			// Authentication is decided before the body is looked at, so an
			// unauthenticated caller learns nothing about the schema.
			"PATCH /me without a token and with an invalid body",
			http.MethodPatch, "/me",
			[]requestOption{withBody(`{"target_kcal":-5}`), withInvalidRequest()},
			http.StatusUnauthorized,
		},
		{"GET /me with a valid token", http.MethodGet, "/me", []requestOption{withBearer(validToken)}, http.StatusOK},
		{
			"PATCH /me with a valid token and an invalid body",
			http.MethodPatch, "/me",
			[]requestOption{withBearer(validToken), withBody(`{"target_kcal":-5}`), withInvalidRequest()},
			http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := contract(t, router, tt.method, tt.path, tt.opts...)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
			if tt.want == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want Bearer", got)
				}
				if p := decodeProblemBody(t, rec); p.Code != httpapi.CodeUnauthorized || len(p.Errors) != 0 {
					t.Errorf("problem = %+v, want code unauthorized and no field errors", p)
				}
			}
		})
	}
}

func TestAuthorizationSchemeIsCaseInsensitiveButMustBeBearer(t *testing.T) {
	router := newTestRouter(t)
	do := func(header string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := do("bearer " + validToken); got != http.StatusOK {
		t.Errorf("lowercase scheme: status = %d, want 200", got)
	}
	for _, h := range []string{"Basic " + validToken, "Bearer", "Bearer  ", validToken} {
		if got := do(h); got != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want 401", h, got)
		}
	}
}

func TestOperationsThatOptOutOfSecurityNeedNoToken(t *testing.T) {
	// The stub rejects every login, but the request must get as far as the
	// handler: a 401 with code invalid_credentials, not the validator's unauthorized.
	rec := contract(t, newTestRouter(t), http.MethodPost, "/auth/login",
		withBody(`{"email":"a@example.com","password":"x"}`))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if p := decodeProblemBody(t, rec); p.Code != httpapi.CodeInvalidCredentials {
		t.Errorf("code = %q, want %q", p.Code, httpapi.CodeInvalidCredentials)
	}
}

func TestAuthEndpointsAreRateLimitedPerClientIP(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{AuthPerMinute: 3} })
	login := func(addr string) *httptest.ResponseRecorder {
		return contract(t, router, http.MethodPost, "/auth/login",
			withBody(`{"email":"a@example.com","password":"x"}`), withRemoteAddr(addr))
	}

	for i := 1; i <= 3; i++ {
		if rec := login("198.51.100.1:1000"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (not yet limited)", i, rec.Code)
		}
	}
	rec := login("198.51.100.1:1000")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th attempt: status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response has no Retry-After header")
	}
	if p := decodeProblemBody(t, rec); p.Code != httpapi.CodeRateLimited {
		t.Errorf("code = %q, want %q", p.Code, httpapi.CodeRateLimited)
	}

	if rec := login("198.51.100.2:1000"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a different client IP was limited too: status = %d, want 401", rec.Code)
	}
	if rec := contract(t, router, http.MethodGet, "/healthz", withRemoteAddr("198.51.100.1:1000")); rec.Code != http.StatusOK {
		t.Errorf("non-auth endpoint was limited by the auth limiter: status = %d", rec.Code)
	}
}

func TestAuthRateLimitBucketsIPv6ByPrefix(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{AuthPerMinute: 2} })
	login := func(addr string) int {
		return contract(t, router, http.MethodPost, "/auth/login",
			withBody(`{"email":"a@example.com","password":"x"}`), withRemoteAddr(addr)).Code
	}

	if got := login("[2001:db8:abcd:1::1]:1000"); got != http.StatusUnauthorized {
		t.Fatalf("first address in the /64: status = %d, want 401", got)
	}
	if got := login("[2001:db8:abcd:1::2]:1000"); got != http.StatusUnauthorized {
		t.Fatalf("second address in the /64: status = %d, want 401", got)
	}
	if got := login("[2001:db8:abcd:1:ffff::3]:1000"); got != http.StatusTooManyRequests {
		t.Errorf("third address in the same /64: status = %d, want 429 (one client rotating addresses)", got)
	}
	if got := login("[2001:db8:abcd:2::1]:1000"); got != http.StatusUnauthorized {
		t.Errorf("a client in a different /64: status = %d, want 401", got)
	}
}

func TestBadRequestsCountTowardTheAuthRateLimit(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{AuthPerMinute: 2} })
	send := func() int {
		return contract(t, router, http.MethodPost, "/auth/login",
			withBody(`{}`), withInvalidRequest(), withRemoteAddr("198.51.100.1:1000")).Code
	}

	if a, b := send(), send(); a != http.StatusBadRequest || b != http.StatusBadRequest {
		t.Fatalf("first two = %d, %d; want 400, 400", a, b)
	}
	if got := send(); got != http.StatusTooManyRequests {
		t.Errorf("third malformed request: status = %d, want 429", got)
	}
}

func TestAuthenticatedRequestsAreRateLimitedPerUser(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{UserPerMinute: 2} })
	me := func(opts ...requestOption) int {
		return contract(t, router, http.MethodGet, "/me", opts...).Code
	}

	if a, b := me(withBearer(validToken)), me(withBearer(validToken)); a != http.StatusOK || b != http.StatusOK {
		t.Fatalf("first two = %d, %d; want 200, 200", a, b)
	}
	if got := me(withBearer(validToken)); got != http.StatusTooManyRequests {
		t.Errorf("third request: status = %d, want 429", got)
	}
	// Unauthenticated requests are rejected by the validator before the
	// per-user limiter, and never count against anyone.
	if got := me(); got != http.StatusUnauthorized {
		t.Errorf("unauthenticated request: status = %d, want 401", got)
	}
}

func TestTrustedProxyHeaderDecidesTheRateLimitedClient(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) {
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1}
		d.TrustedProxies = 1
	})
	login := func(forwardedFor string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.0.0.1:80" // the proxy: every request comes from here
		req.Header.Set("X-Forwarded-For", forwardedFor)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := login("198.51.100.1"); got != http.StatusUnauthorized {
		t.Fatalf("client A first request: status = %d, want 401", got)
	}
	if got := login("198.51.100.1"); got != http.StatusTooManyRequests {
		t.Errorf("client A second request: status = %d, want 429", got)
	}
	if got := login("198.51.100.2"); got != http.StatusUnauthorized {
		t.Errorf("client B was limited with client A: status = %d, want 401", got)
	}
	// Forging entries on the left cannot dodge the limit.
	if got := login("6.6.6.6, 198.51.100.1"); got != http.StatusTooManyRequests {
		t.Errorf("client A with a forged prefix: status = %d, want 429", got)
	}
}

func TestDuplicateAuthorizationHeadersAreRejected(t *testing.T) {
	router := newTestRouter(t)
	get := func(headers ...string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		for _, h := range headers {
			req.Header.Add("Authorization", h)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	// A proxy in front might act on a different header line than this API
	// does, so an ambiguous request is refused outright.
	if got := get("Bearer "+validToken, "Bearer "+validToken); got != http.StatusUnauthorized {
		t.Errorf("two Authorization headers: status = %d, want 401", got)
	}
	if got := get("Bearer " + validToken); got != http.StatusOK {
		t.Errorf("one Authorization header: status = %d, want 200", got)
	}
}

func TestSecuredRoutesRejectHeadAndOptionsWithoutAToken(t *testing.T) {
	router := newTestRouter(t)
	for _, method := range []string{http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(method, "/v1/me", nil))

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s /v1/me without a token: status = %d, want 405", method, rec.Code)
			}
		})
	}
}

func TestAuthLimiterPathVariantsNeverReachAHandler(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{AuthPerMinute: 2} })
	for _, path := range []string{
		"/v1//auth/login", "/v1/auth/login/", "/v1/auth//login", "/v1/%61uth/login", "/v1/auth/LOGIN",
	} {
		t.Run(path, func(t *testing.T) {
			for i := 1; i <= 5; i++ {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"a@example.com","password":"x"}`))
				req.Header.Set("Content-Type", "application/json")
				req.RemoteAddr = "198.51.100.9:1000"
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				// The stub answers a login that reaches it with 401, so 401
				// or 200 would mean a variant slipped past routing.
				if rec.Code != http.StatusNotFound && rec.Code != http.StatusTooManyRequests {
					t.Fatalf("request %d to %s: status = %d, want 404 or 429", i, path, rec.Code)
				}
			}
		})
	}
}

func TestPerUserRateLimitBucketsAreSeparate(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{UserPerMinute: 2} })
	me := func(token string) int {
		return contract(t, router, http.MethodGet, "/me", withBearer(token)).Code
	}

	if a, b, c := me(validToken), me(validToken), me(validToken); a != http.StatusOK || b != http.StatusOK || c != http.StatusTooManyRequests {
		t.Fatalf("user 1 = %d, %d, %d; want 200, 200, 429", a, b, c)
	}
	if got := me(validToken2); got != http.StatusOK {
		t.Errorf("user 2 was limited with user 1: status = %d, want 200", got)
	}
}

func TestRejectedRequestsDoNotCountTowardTheUserLimit(t *testing.T) {
	// Documents a known gap: the per-user limiter sits behind the validator,
	// so rejected requests are not counted; a global per-IP backstop is a
	// planned follow-up.
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{UserPerMinute: 2} })

	for i := 1; i <= 4; i++ {
		rec := contract(t, router, http.MethodPatch, "/me", withBearer(validToken),
			withBody(`{"target_fat_g":-1}`), withInvalidRequest())
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("request %d: status = %d, want 400", i, rec.Code)
		}
	}
}

func TestConcurrentAuthenticatedRequests(t *testing.T) {
	const workers = 50
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{UserPerMinute: 10000} })
	codes := make([]int, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			req.Header.Set("Authorization", "Bearer "+validToken)
			rec := httptest.NewRecorder()
			<-start
			router.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}()
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d: status = %d, want 200", i, code)
		}
	}
}
