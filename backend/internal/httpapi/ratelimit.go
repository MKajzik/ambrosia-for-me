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
	// AcceptPerHour limits POST /v1/partner/accept, per user and, separately,
	// per client IP. An invite code has about 39 bits, so guessing one is only
	// hopeless while this stays small and covers many accounts behind one IP.
	AcceptPerHour int
}

const (
	defaultAuthPerMinute = 10
	defaultUserPerMinute = 300
	defaultAcceptPerHour = 10
	// acceptWindow is the window AcceptPerHour counts over.
	acceptWindow = time.Hour
	// acceptPath is the one route the accept limiters apply to.
	acceptPath = "/v1/partner/accept"
)

func (l RateLimits) withDefaults() RateLimits {
	if l.AuthPerMinute <= 0 {
		l.AuthPerMinute = defaultAuthPerMinute
	}
	if l.UserPerMinute <= 0 {
		l.UserPerMinute = defaultUserPerMinute
	}
	if l.AcceptPerHour <= 0 {
		l.AcceptPerHour = defaultAcceptPerHour
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

// acceptIPLimiter limits POST /v1/partner/accept per client IP. Like
// authIPLimiter it runs before routing and validation, so unauthenticated and
// malformed attempts count too, and IPv6 clients are bucketed by their /64.
// Counters are in memory: with several API replicas the effective limit is
// per replica.
func acceptIPLimiter(perHour int) func(http.Handler) http.Handler {
	limit := httprate.LimitBy(perHour, acceptWindow,
		func(r *http.Request) (string, error) { return rateLimitKey(ClientIP(r.Context())), nil },
		httprate.WithLimitHandler(rateLimited))
	return onlyForPath(acceptPath, limit)
}

// acceptUserLimiter limits POST /v1/partner/accept per user. It must run after
// the validator, which is what identifies the user; unauthenticated requests
// pass through untouched (acceptIPLimiter has already counted them).
func acceptUserLimiter(perHour int) func(http.Handler) http.Handler {
	limit := httprate.LimitBy(perHour, acceptWindow,
		func(r *http.Request) (string, error) {
			id, _ := UserID(r.Context())
			return id.String(), nil
		},
		httprate.WithLimitHandler(rateLimited))
	authenticated := func(next http.Handler) http.Handler {
		limited := limit(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := UserID(r.Context()); ok {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	return onlyForPath(acceptPath, authenticated)
}

// onlyForPath applies mw to requests for exactly path and passes every other
// request straight through.
func onlyForPath(path string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == path {
				wrapped.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
