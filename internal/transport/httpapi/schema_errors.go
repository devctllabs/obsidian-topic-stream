package httpapi

import (
	"errors"
	"fmt"
	"strings"

	"github.com/devctllabs/go-libs/oapivalidator"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

type schemaMessageSet struct {
	messages map[string][]string
	roots    map[string]*openapi3.Schema
}

func schemaMessages(cause error, fields []oapivalidator.FieldError) map[string][]string {
	set := schemaMessageSet{messages: make(map[string][]string), roots: make(map[string]*openapi3.Schema)}
	collectSchemaMessages(cause, "", &set)
	for _, field := range fields {
		key := validationField(field)
		if field.Code == "required" || specificSchemaMessage(set.messages[key]) {
			continue
		}
		rootKey := field.Parameter
		if field.In == oapivalidator.LocationBody {
			rootKey = "body"
		}
		if message := canonicalConstraintMessage(set.roots[rootKey], field.Pointer); message != "" {
			set.messages[key] = []string{message}
		}
	}
	return set.messages
}

func specificSchemaMessage(messages []string) bool {
	return len(messages) > 0 && messages[0] != "Enter a valid value."
}

func collectSchemaMessages(cause error, parameter string, set *schemaMessageSet) {
	switch failure := cause.(type) {
	case openapi3.MultiError:
		for _, child := range failure {
			collectSchemaMessages(child, parameter, set)
		}
	case *openapi3filter.RequestError:
		parameter = collectRequestSchemas(failure, parameter, set)
		collectSchemaMessages(failure.Err, parameter, set)
	case *openapi3.SchemaError:
		if failure.Origin != nil {
			collectSchemaMessages(failure.Origin, parameter, set)
			return
		}
		field := parameter
		if field == "" {
			field = schemaPointer(failure.JSONPointer())
		}
		set.messages[field] = append(set.messages[field], schemaMessage(failure))
	default:
		collectWrappedSchemaMessages(cause, parameter, set)
	}
}

func collectRequestSchemas(failure *openapi3filter.RequestError, parameter string, set *schemaMessageSet) string {
	if failure.Parameter != nil {
		parameter = failure.Parameter.Name
		if failure.Parameter.Schema != nil {
			set.roots[parameter] = failure.Parameter.Schema.Value
		}
	}
	if failure.RequestBody != nil {
		if media := failure.RequestBody.Content.Get("application/json"); media != nil && media.Schema != nil {
			set.roots["body"] = media.Schema.Value
		}
	}
	return parameter
}

func collectWrappedSchemaMessages(cause error, parameter string, set *schemaMessageSet) {
	if joined, ok := cause.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			collectSchemaMessages(child, parameter, set)
		}
	} else if child := errors.Unwrap(cause); child != nil {
		collectSchemaMessages(child, parameter, set)
	}
}

func schemaPointer(parts []string) string {
	if len(parts) == 0 {
		return "body"
	}
	escaped := make([]string, len(parts))
	for index, part := range parts {
		escaped[index] = strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
	}
	return "/" + strings.Join(escaped, "/")
}

func schemaMessage(failure *openapi3.SchemaError) string {
	schema := failure.Schema
	if schema == nil {
		return "Enter a valid value."
	}
	switch failure.SchemaField {
	case "required":
		return "This field is required."
	case "type":
		return "Use the expected value type: " + strings.Join(schema.Type.Slice(), " or ") + "."
	case "enum":
		choices := make([]string, len(schema.Enum))
		for index, choice := range schema.Enum {
			choices[index] = fmt.Sprint(choice)
		}
		return "Choose one of: " + strings.Join(choices, ", ") + "."
	case "pattern", "format":
		return "Use the expected format."
	default:
		return schemaConstraintMessage(failure)
	}
}

func schemaConstraintMessage(failure *openapi3.SchemaError) string {
	schema := failure.Schema
	switch failure.SchemaField {
	case "minimum":
		if schema.Min != nil {
			return fmt.Sprintf("Enter a value of at least %g.", *schema.Min)
		}
	case "maximum":
		if schema.Max != nil {
			return fmt.Sprintf("Enter a value no greater than %g.", *schema.Max)
		}
	case "minLength":
		return lengthConstraintMessage("at least", schema.MinLength)
	case "maxLength":
		if schema.MaxLength != nil {
			return lengthConstraintMessage("at most", *schema.MaxLength)
		}
	}
	return "Enter a valid value."
}

func lengthConstraintMessage(bound string, count uint64) string {
	unit := "characters"
	if count == 1 {
		unit = "character"
	}
	return fmt.Sprintf("Enter %s %d %s.", bound, count, unit)
}

func boundaryFieldMessage(field oapivalidator.FieldError, messages map[string][]string) string {
	if available := messages[validationField(field)]; len(available) > 0 {
		messages[validationField(field)] = available[1:]
		return available[0]
	}
	switch field.Code {
	case "required":
		return "This field is required."
	case "malformed":
		return "Provide a value in the expected format."
	case "unsupported_media_type":
		return "Use a supported content type."
	default:
		return "Enter a valid value."
	}
}
