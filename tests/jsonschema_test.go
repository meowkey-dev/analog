// A validator for the subset of json schema contracts/openapi.json uses.
//
// The python harness leans on jsonschema's Draft202012Validator; the go suite
// judges the same shapes with this. The schemas use: type (including ["x","null"]
// unions), enum, const, properties, required, additionalProperties (boolean),
// items, oneOf, internal $ref, format: date-time, minimum and maximum. Anything
// else (annotations like title/description/default) is ignored. If an amendment
// ever grows the schema vocabulary, this file grows with it — a red suite is the
// reminder.
package conformance

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"encoding/json"
)

type schemaValidator struct {
	components map[string]any
	errs       []string
}

// assertValid asserts instance matches the named openapi component schema.
func assertValid(t *testing.T, instance any, schemaName string, many bool) {
	t.Helper()
	openapi := openapiDoc(t)
	v := &schemaValidator{components: asMap(asMap(openapi["components"])["schemas"])}
	schema := map[string]any{"$ref": "#/components/schemas/" + schemaName}
	if many {
		schema = map[string]any{"type": "array", "items": schema}
	}
	v.check(instance, schema, "")
	if len(v.errs) > 0 {
		sort.Strings(v.errs)
		if len(v.errs) > 8 {
			v.errs = v.errs[:8]
		}
		t.Errorf("does not match schema %s:\n  %s", schemaName, strings.Join(v.errs, "\n  "))
	}
}

func (v *schemaValidator) failf(loc, format string, args ...any) {
	if loc != "" {
		loc = loc + ": "
	}
	v.errs = append(v.errs, loc+fmt.Sprintf(format, args...))
}

func (v *schemaValidator) resolve(schema map[string]any) map[string]any {
	if ref, ok := schema["$ref"].(string); ok && strings.HasPrefix(ref, "#/components/schemas/") {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if resolved, ok := v.components[name].(map[string]any); ok {
			return resolved
		}
		v.failf("", "unknown $ref %s", ref)
	}
	return schema
}

func (v *schemaValidator) check(instance any, rawSchema any, loc string) {
	schema, ok := rawSchema.(map[string]any)
	if !ok {
		return // no schema, or a boolean form this document does not use
	}
	schema = v.resolve(schema)

	if t, ok := schema["type"]; ok && !v.typeMatches(instance, t) {
		v.failf(loc, "expected type %s, got %s", canonical(t), typeName(instance))
		return
	}
	if allowed, ok := schema["enum"].([]any); ok {
		found := false
		for _, option := range allowed {
			if jsonEqual(instance, option) {
				found = true
			}
		}
		if !found {
			v.failf(loc, "%s is not one of %s", canonical(instance), canonical(allowed))
		}
	}
	if c, ok := schema["const"]; ok && !jsonEqual(instance, c) {
		v.failf(loc, "%s != const %s", canonical(instance), canonical(c))
	}

	switch inst := instance.(type) {
	case map[string]any:
		if props, ok := schema["properties"].(map[string]any); ok {
			for name, sub := range props {
				if value, present := inst[name]; present {
					v.check(value, sub, loc+name)
				}
			}
			if ap, ok := schema["additionalProperties"].(bool); ok && !ap {
				for name := range inst {
					if _, declared := props[name]; !declared {
						v.failf(loc, "additional property %q is not allowed", name)
					}
				}
			}
		}
		for _, name := range asArr(schema["required"]) {
			if _, present := inst[asStr(name)]; !present {
				v.failf(loc, "missing required property %q", asStr(name))
			}
		}
	case []any:
		if items, ok := schema["items"]; ok {
			for i, element := range inst {
				v.check(element, items, fmt.Sprintf("%s[%d]", loc, i))
			}
		}
	}

	if oneOf, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, sub := range oneOf {
			probe := &schemaValidator{components: v.components}
			probe.check(instance, sub, "")
			if len(probe.errs) == 0 {
				matches++
			}
		}
		if matches != 1 {
			v.failf(loc, "matches %d oneOf branches, want exactly 1", matches)
		}
	}

	if format, _ := schema["format"].(string); format == "date-time" {
		if s, ok := instance.(string); ok {
			if _, err := time.Parse(time.RFC3339, s); err != nil {
				v.failf(loc, "%q is not a valid date-time", s)
			}
		}
	}
	if err := bound(schema, "minimum", instance, func(value, limit float64) bool { return value < limit }); err != nil {
		v.failf(loc, "%s", err)
	}
	if err := bound(schema, "maximum", instance, func(value, limit float64) bool { return value > limit }); err != nil {
		v.failf(loc, "%s", err)
	}
}

func bound(schema map[string]any, key string, instance any, violated func(value, limit float64) bool) error {
	limitRaw, ok := schema[key].(json.Number)
	if !ok {
		return nil
	}
	valueRaw, ok := instance.(json.Number)
	if !ok {
		return nil
	}
	limit, err1 := limitRaw.Float64()
	value, err2 := valueRaw.Float64()
	if err1 != nil || err2 != nil {
		return nil
	}
	if violated(value, limit) {
		return fmt.Errorf("%v violates %s %v", instance, key, limitRaw)
	}
	return nil
}

func (v *schemaValidator) typeMatches(instance any, t any) bool {
	switch want := t.(type) {
	case string:
		return oneTypeMatches(instance, want)
	case []any:
		for _, w := range want {
			if oneTypeMatches(instance, asStr(w)) {
				return true
			}
		}
		return false
	}
	return true
}

func oneTypeMatches(instance any, want string) bool {
	switch want {
	case "null":
		return instance == nil
	case "boolean":
		_, ok := instance.(bool)
		return ok
	case "string":
		_, ok := instance.(string)
		return ok
	case "number":
		_, ok := instance.(json.Number)
		return ok
	case "integer":
		n, ok := instance.(json.Number)
		if !ok {
			return false
		}
		return !strings.ContainsAny(n.String(), ".eE")
	case "array":
		_, ok := instance.([]any)
		return ok
	case "object":
		_, ok := instance.(map[string]any)
		return ok
	}
	return false
}

func typeName(instance any) string {
	switch instance.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return reflect.TypeOf(instance).String()
}
