package httpapi

import (
	"errors"
	"net/mail"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

func init() {
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
	return res, true
}

func collect(err error, res *validationResult) bool {
	// kin-openapi returns these types directly, and MultiError must be tried
	// first: it wraps the others.
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		for _, inner := range multi {
			if !collect(inner, res) {
				return false
			}
		}
		return true
	}
	var reqErr *openapi3filter.RequestError
	if errors.As(err, &reqErr) {
		return collectRequestError(reqErr, res)
	}
	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		res.fields = append(res.fields, schemaFieldError(schemaErr))
		return true
	}
	return false
}

func collectRequestError(e *openapi3filter.RequestError, res *validationResult) bool {
	if e.Parameter != nil {
		res.fields = append(res.fields, FieldError{Field: e.Parameter.Name, Code: FieldInvalidValue})
		return true
	}
	if e.RequestBody == nil || e.Err == nil {
		return false
	}
	var (
		multi     openapi3.MultiError
		schemaErr *openapi3.SchemaError
		parseErr  *openapi3filter.ParseError
	)
	switch {
	case errors.As(e.Err, &multi), errors.As(e.Err, &schemaErr):
		return collect(e.Err, res)
	case errors.As(e.Err, &parseErr):
		res.detail = "request body is not valid JSON"
		return true
	default:
		// kin-openapi reports a missing required body as a plain error.
		res.detail = "request body is required"
		return true
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
			return FieldError{Field: name, Code: FieldUnknown}
		}
	}
	return FieldError{Field: field, Code: FieldInvalidValue}
}
