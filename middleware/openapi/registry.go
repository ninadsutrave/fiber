package openapi

import (
	"maps"
	"reflect"

	"github.com/gofiber/utils/v2"
)

// componentsSchemasRef prefixes every reference the registry hands out.
const componentsSchemasRef = "#/components/schemas/"

// schemaRegistry collects named struct types so each is emitted once under
// components.schemas and referenced elsewhere.
type schemaRegistry struct {
	schemas map[string]map[string]any
	names   map[reflect.Type]string
	// taken maps a component name to the type that owns it. A name a user's
	// Components already uses is taken by a nil type, so it is never reused.
	taken map[string]reflect.Type
	// wrapRefSiblings is set for OpenAPI 3.0, where a Reference Object may hold nothing but "$ref".
	wrapRefSiblings bool
}

// newSchemaRegistry starts a registry that never reuses names in Components.schemas.
func newSchemaRegistry(cfg *Config) *schemaRegistry {
	reg := &schemaRegistry{
		schemas: make(map[string]map[string]any),
		names:   make(map[reflect.Type]string),
		taken:   make(map[string]reflect.Type),

		wrapRefSiblings: !versionAtLeast(cfg.OpenAPIVersion, versionOpenAPI31),
	}
	for name := range stringKeyedEntries(cfg.Components["schemas"]) {
		reg.taken[name] = nil
	}
	return reg
}

// resolve turns a schema argument into a schema map; a nil registry reflects inline.
func (reg *schemaRegistry) resolve(schema any) map[string]any {
	switch value := schema.(type) {
	case nil:
		return nil
	case map[string]any:
		if len(value) == 0 {
			return nil
		}
		return maps.Clone(value)
	default:
		return typeSchema(reflect.TypeOf(schema), nil, reg)
	}
}

// ref registers a named struct type and returns a reference to it. The name is
// claimed before the schema is built so self-referencing types do not recurse. The component is
// built with an empty visited set: it must not depend on the types of whichever schema reached it
// first, or the same type would document differently depending on registration order.
func (reg *schemaRegistry) ref(t reflect.Type) map[string]any {
	name, ok := reg.names[t]
	if !ok {
		name = reg.claimName(t)
		reg.names[t] = name
		reg.schemas[name] = nil // reserve the entry so a recursive reference finds the name taken
		reg.schemas[name] = structSchema(t, nil, reg)
	}
	return map[string]any{schemaKeyRef: componentsSchemasRef + name}
}

// claimName picks a unique component name: the type name, then package-qualified, then numbered.
func (reg *schemaRegistry) claimName(t reflect.Type) string {
	base := componentName(t.Name())
	candidates := []string{base}
	if pkg := t.PkgPath(); pkg != "" {
		if _, after, found := utils.LastCutByte(pkg, '/'); found {
			pkg = after
		}
		candidates = append(candidates, componentName(pkg+"."+t.Name()))
	}
	for _, candidate := range candidates {
		if _, exists := reg.taken[candidate]; !exists {
			reg.taken[candidate] = t
			return candidate
		}
	}
	for i := 2; ; i++ {
		candidate := base + "_" + utils.FormatInt(int64(i))
		if _, exists := reg.taken[candidate]; !exists {
			reg.taken[candidate] = t
			return candidate
		}
	}
}

// componentName restricts a name to ^[a-zA-Z0-9.\-_]+$, so generics like Page[pkg.User] stay valid keys.
func componentName(name string) string {
	if key := keyName(name); key != "" {
		return key
	}
	return "_"
}

// componentSchemas returns the registered schemas keyed for components.schemas.
func (reg *schemaRegistry) componentSchemas() map[string]any {
	if reg == nil || len(reg.schemas) == 0 {
		return nil
	}
	out := make(map[string]any, len(reg.schemas))
	for name, schema := range reg.schemas {
		out[name] = schema
	}
	return out
}

// withRefSiblings returns schema ready to carry sibling keys next to a "$ref". OpenAPI 3.1 allows them; in
// 3.0 a Reference Object holds only "$ref", so the reference moves into "allOf" and the siblings stay beside it.
func (reg *schemaRegistry) withRefSiblings(schema map[string]any) map[string]any {
	target, isRef := schema[schemaKeyRef].(string)
	if reg == nil || !reg.wrapRefSiblings || !isRef || len(schema) < 2 {
		return schema
	}
	wrapped := make(map[string]any, len(schema))
	for key, value := range schema {
		if key != schemaKeyRef {
			wrapped[key] = value
		}
	}
	wrapped["allOf"] = []any{map[string]any{schemaKeyRef: target}}
	return wrapped
}
