package httpapi

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// canonicalConstraintMessage uses schema facts when the OpenAPI 3.1 validator has discarded them.
// It never parses diagnostic reason strings or includes the submitted value.
func canonicalConstraintMessage(root *openapi3.Schema, pointer string) string {
	var parts []string
	if pointer != "" {
		parts = strings.Split(strings.TrimPrefix(pointer, "/"), "/")
		for index, part := range parts {
			parts[index] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		}
	}
	var message string
	for _, schema := range schemasAtPath(root, parts) {
		candidate := schemaHint(schema)
		if candidate == "" {
			continue
		}
		if message != "" && message != candidate {
			return "" // Ambiguous union branches require the generic fallback.
		}
		message = candidate
	}
	return message
}

func schemasAtPath(schema *openapi3.Schema, parts []string) []*openapi3.Schema {
	if schema == nil {
		return nil
	}
	if len(parts) == 0 {
		return []*openapi3.Schema{schema}
	}
	if property := schema.Properties[parts[0]]; property != nil {
		return schemasAtPath(property.Value, parts[1:])
	}
	if index, err := strconv.Atoi(parts[0]); err == nil && index >= 0 && schema.Items != nil {
		return schemasAtPath(schema.Items.Value, parts[1:])
	}
	var matches []*openapi3.Schema
	for _, group := range []openapi3.SchemaRefs{schema.OneOf, schema.AnyOf, schema.AllOf} {
		for _, branch := range group {
			matches = append(matches, schemasAtPath(branch.Value, parts)...)
		}
	}
	return matches
}

func schemaHint(schema *openapi3.Schema) string {
	if len(schema.Enum) > 0 {
		return schemaMessage(&openapi3.SchemaError{Schema: schema, SchemaField: "enum"})
	}
	if schema.Min != nil && schema.Max != nil {
		return fmt.Sprintf("Enter a value between %g and %g.", *schema.Min, *schema.Max)
	}
	if schema.Min != nil {
		return schemaConstraintMessage(&openapi3.SchemaError{Schema: schema, SchemaField: "minimum"})
	}
	if schema.Max != nil {
		return schemaConstraintMessage(&openapi3.SchemaError{Schema: schema, SchemaField: "maximum"})
	}
	return schemaTextHint(schema)
}

func schemaTextHint(schema *openapi3.Schema) string {
	if schema.Format != "" || schema.Pattern != "" {
		return "Use the expected format."
	}
	if schema.MaxLength != nil {
		return fmt.Sprintf("Enter between %d and %d characters.", schema.MinLength, *schema.MaxLength)
	}
	if schema.MinLength > 0 {
		return lengthConstraintMessage("at least", schema.MinLength)
	}
	if schema.Type != nil {
		return schemaMessage(&openapi3.SchemaError{Schema: schema, SchemaField: "type"})
	}
	return ""
}
