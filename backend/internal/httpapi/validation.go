package httpapi

import (
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

func init() {
	// By default a kin-openapi SchemaError appends the offending value to its
	// message. Errors reach the logs (slog.Any("err", err)) on the validation
	// paths, so a rejected password or token could be written to them.
	openapi3.SchemaErrorDetailsDisabled = true

	// kin-openapi does not check `format: email` unless a validator is
	// registered. Accept a bare address only ("Name <a@b.c>" is rejected).
	openapi3.DefineStringFormatValidator("email", openapi3.NewCallbackValidator(func(s string) error {
		addr, err := mail.ParseAddress(s)
		if err != nil || addr.Address != s {
			return errors.New("not an email address")
		}
		return nil
	}))
}

var unsupportedProperty = regexp.MustCompile(`property "([^"]+)" is unsupported`)

// validationResult is what could be learned from a request-validation error.
type validationResult struct {
	detail string
	fields []FieldError
}

// describeValidation translates kin-openapi request-validation errors into
// client-facing field errors. It returns false when err contains anything
// other than request-validation errors (a server-side problem).
func describeValidation(err error) (validationResult, bool) {
	var res validationResult
	if !collect(err, &res) {
		return validationResult{}, false
	}
	sort.Slice(res.fields, func(i, j int) bool {
		if res.fields[i].Field != res.fields[j].Field {
			return res.fields[i].Field < res.fields[j].Field
		}
		return res.fields[i].Code < res.fields[j].Code
	})
	// Deduplicate field errors
	deduped := make([]FieldError, 0, len(res.fields))
	seen := make(map[string]bool)
	for _, f := range res.fields {
		key := f.Field + ":" + f.Code
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, f)
		}
	}
	res.fields = deduped
	// Cap the total number of field errors at 20
	if len(res.fields) > 20 {
		res.fields = res.fields[:20]
	}
	return res, true
}

//nolint:errorlint // kin-openapi returns these concrete types directly; MultiError and RequestError unwrap into each other
func collect(err error, res *validationResult) bool {
	// Use type switch instead of errors.As to avoid unwrapping through RequestError
	switch err := err.(type) {
	case openapi3.MultiError:
		for _, inner := range err {
			if !collect(inner, res) {
				return false
			}
		}
		return true
	case *openapi3filter.RequestError:
		return collectRequestError(err, res)
	case *openapi3.SchemaError:
		res.fields = append(res.fields, schemaFieldError(err))
		return true
	default:
		return false
	}
}

func collectRequestError(e *openapi3filter.RequestError, res *validationResult) bool {
	if e.Parameter != nil {
		res.fields = append(res.fields, FieldError{Field: e.Parameter.Name, Code: FieldInvalidValue})
		return true
	}
	// Handle Content-Type errors: missing or unsupported Content-Type
	if e.RequestBody != nil && e.Err == nil && strings.Contains(e.Reason, "Content-Type") {
		res.detail = "request body must be sent as application/json"
		return true
	}
	if e.RequestBody == nil || e.Err == nil {
		return false
	}

	//nolint:errorlint // kin-openapi returns these concrete types directly within a RequestError
	switch innerErr := e.Err.(type) {
	case openapi3.MultiError:
		return collect(innerErr, res)
	case *openapi3.SchemaError:
		return collect(innerErr, res)
	case *openapi3filter.ParseError:
		res.detail = "request body is not valid JSON"
		return true
	default:
		// Check for ErrInvalidRequired (unwrapped error check)
		if errors.Is(e.Err, openapi3filter.ErrInvalidRequired) {
			res.detail = "request body is required"
			return true
		}
		// Check for MaxBytesError (unwrapped error check)
		var maxBytesErr *http.MaxBytesError
		if errors.As(e.Err, &maxBytesErr) {
			res.detail = "request body is too large"
			return true
		}
		// Unrecognized body error: this is a server-side problem
		return false
	}
}

func schemaFieldError(e *openapi3.SchemaError) FieldError {
	field := strings.Join(e.JSONPointer(), ".")
	switch e.SchemaField {
	case "required":
		return FieldError{Field: field, Code: FieldRequired}
	case "minLength", "minItems":
		return FieldError{Field: field, Code: FieldTooShort}
	case "maxLength", "maxItems":
		return FieldError{Field: field, Code: FieldTooLong}
	case "format", "pattern":
		return FieldError{Field: field, Code: FieldInvalidForm}
	case "type":
		return FieldError{Field: field, Code: FieldInvalidType}
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf":
		return FieldError{Field: field, Code: FieldOutOfRange}
	case "properties":
		if m := unsupportedProperty.FindStringSubmatch(e.Reason); m != nil {
			name := m[1]
			if field != "" {
				name = field + "." + name
			}
			// Truncate the unknown field name to at most 64 runes
			runes := []rune(name)
			if len(runes) > 64 {
				runes = runes[:64]
			}
			return FieldError{Field: string(runes), Code: FieldUnknown}
		}
	}
	return FieldError{Field: field, Code: FieldInvalidValue}
}
