package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

const requestIDHeader = "X-Request-Id"

type ctxKey int

const requestIDKey ctxKey = iota

// A caller-supplied request ID is only trusted when it is short and printable.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID returns the request ID stored in ctx, or "" when there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// requestID assigns every request an ID, exposes it as X-Request-Id, and
// stores it in the request context.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error on supported platforms
	return hex.EncodeToString(b)
}

// requestLogger logs one structured line per request, after it completes.
// Server errors (5xx) log at error level so they can be alerted on by level.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			attrs := []slog.Attr{
				slog.String("request_id", RequestID(r.Context())),
				slog.String("remote_ip", ClientIP(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			}
			if id, ok := UserID(r.Context()); ok {
				attrs = append(attrs, slog.String("user_id", id.String()))
			}
			logger.LogAttrs(r.Context(), level, "request", attrs...)
		})
	}
}

// maxBodyBytes caps request bodies; every request body in this API is small JSON.
const maxBodyBytes = 64 << 10

// bodyLimit rejects request bodies larger than maxBodyBytes.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// recoverer turns a handler panic into a 500 problem response and logs the stack.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler { //nolint:errorlint // http.ErrAbortHandler is compared by identity per net/http docs
					panic(rec)
				}
				logger.ErrorContext(r.Context(), "panic in handler",
					slog.String("request_id", RequestID(r.Context())),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				WriteProblem(w, http.StatusInternalServerError, CodeInternal, "")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
