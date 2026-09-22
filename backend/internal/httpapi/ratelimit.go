package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/httprate"
)

const rateWindow = time.Minute

// RateLimits are the per-minute request limits. Zero values use the defaults.
type RateLimits struct {
	// AuthPerMinute limits /v1/auth/* per client IP (credential-guessing defence).
	AuthPerMinute int
	// UserPerMinute limits every other authenticated request per user.
	UserPerMinute int
}

const (
	defaultAuthPerMinute = 10
	defaultUserPerMinute = 300
)

func (l RateLimits) withDefaults() RateLimits {
	if l.AuthPerMinute <= 0 {
		l.AuthPerMinute = defaultAuthPerMinute
	}
	if l.UserPerMinute <= 0 {
		l.UserPerMinute = defaultUserPerMinute
	}
	return l
}

func rateLimited(w http.ResponseWriter, _ *http.Request) {
	if w.Header().Get("Retry-After") == "" {
		w.Header().Set("Retry-After", strconv.Itoa(int(rateWindow.Seconds())))
	}
	WriteProblem(w, http.StatusTooManyRequests, CodeRateLimited, "")
}

// authIPLimiter limits /v1/auth/* requests per client IP. It runs before
// routing and validation, so malformed and failed attempts count too.
// IPv6 clients are bucketed by their /64 network, so one client cannot dodge
// the limit by rotating addresses. Counters are in memory: with several API
// replicas the effective limit is per replica.
func authIPLimiter(perMinute int) func(http.Handler) http.Handler {
	limit := httprate.LimitBy(perMinute, rateWindow,
		func(r *http.Request) (string, error) { return rateLimitKey(ClientIP(r.Context())), nil },
		httprate.WithLimitHandler(rateLimited))
	return func(next http.Handler) http.Handler {
		limited := limit(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/v1/auth/") {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// userLimiter limits authenticated requests per user. It must run after the
// validator, which is what identifies the user; unauthenticated requests pass
// through untouched.
func userLimiter(perMinute int) func(http.Handler) http.Handler {
	limit := httprate.LimitBy(perMinute, rateWindow,
		func(r *http.Request) (string, error) {
			id, _ := UserID(r.Context())
			return id.String(), nil
		},
		httprate.WithLimitHandler(rateLimited))
	return func(next http.Handler) http.Handler {
		limited := limit(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := UserID(r.Context()); ok {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
