package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/google/uuid"
	nethttpmw "github.com/oapi-codegen/nethttp-middleware"
)

const authStateKey ctxKey = iota + 200

// authState is attached to every request context by withAuthState. The
// validator's authentication step fills it in; handlers and the request logger
// read it. A pointer is shared because the validator cannot replace the
// request context.
type authState struct {
	userID uuid.UUID
}

// TokenParser validates an access token and returns the user it was issued to.
type TokenParser interface {
	ParseAccess(token string) (uuid.UUID, error)
}

// UserID returns the authenticated user's ID. It reports false on routes that
// do not require authentication and when authentication failed.
func UserID(ctx context.Context) (uuid.UUID, bool) {
	st, _ := ctx.Value(authStateKey).(*authState)
	if st == nil || st.userID == uuid.Nil {
		return uuid.Nil, false
	}
	return st.userID, true
}

func withAuthState(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authStateKey, &authState{})))
	})
}

var errUnauthenticated = errors.New("unauthenticated")

// openAPIValidator validates every routed request against the OpenAPI
// document: the body and parameters must match the schema, and operations that
// declare `bearerAuth` need a valid access token. Because it is driven by the
// spec's own `security` blocks, a route added to openapi.yaml is protected by
// default; only `security: []` opts out.
func openAPIValidator(spec *openapi3.T, tokens TokenParser, logger *slog.Logger) func(http.Handler) http.Handler {
	return nethttpmw.OapiRequestValidatorWithOptions(spec, &nethttpmw.Options{
		Options: openapi3filter.Options{
			MultiError:         true,
			AuthenticationFunc: authenticate(tokens),
		},
		DoNotValidateServers: true,
		Prefix:               "/v1",
		ErrorHandlerWithOpts: func(ctx context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmw.ErrorHandlerOpts) {
			handleValidationError(logger, err, w, r, opts)
		},
	})
}

func authenticate(tokens TokenParser) openapi3filter.AuthenticationFunc {
	return func(ctx context.Context, in *openapi3filter.AuthenticationInput) error {
		if in.SecuritySchemeName != "bearerAuth" {
			return errUnauthenticated
		}
		// More than one Authorization header is ambiguous: a proxy in front may
		// act on a different line than this API, so refuse it outright.
		vals := in.RequestValidationInput.Request.Header.Values("Authorization")
		if len(vals) != 1 {
			return errUnauthenticated
		}
		scheme, token, ok := strings.Cut(vals[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			return errUnauthenticated
		}
		id, err := tokens.ParseAccess(token)
		if err != nil {
			return errUnauthenticated
		}
		if st, _ := ctx.Value(authStateKey).(*authState); st != nil {
			st.userID = id
		}
		return nil
	}
}

func handleValidationError(logger *slog.Logger, err error, w http.ResponseWriter, r *http.Request, opts nethttpmw.ErrorHandlerOpts) {
	switch {
	case hasSecurityError(err):
		// Authentication failure wins over body errors, so an unauthenticated
		// caller learns nothing about the request schema.
		w.Header().Set("WWW-Authenticate", "Bearer")
		WriteProblem(w, http.StatusUnauthorized, CodeUnauthorized, "")
	case opts.MatchedRoute == nil:
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	default:
		res, ok := describeValidation(err)
		if !ok {
			logger.ErrorContext(r.Context(), "request validation failed unexpectedly",
				slog.String("request_id", RequestID(r.Context())), slog.Any("err", err))
			WriteProblem(w, http.StatusInternalServerError, CodeInternal, "")
			return
		}
		if res.detail == "" && len(res.fields) == 0 {
			res.detail = "request is invalid"
		}
		WriteValidationProblem(w, res.detail, res.fields)
	}
}

func hasSecurityError(err error) bool {
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		for _, inner := range multi {
			if hasSecurityError(inner) {
				return true
			}
		}
		return false
	}
	var sec *openapi3filter.SecurityRequirementsError
	return errors.As(err, &sec)
}
