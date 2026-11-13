package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/giocaizzi/agents/internal/feature"
)

// SchemaID is the canonical $id of the generated JSON Schema.
const SchemaID = "https://raw.githubusercontent.com/giocaizzi/agents/main/schema/config.schema.json"

// JSONSchema makes Harness an enum.
func (Harness) JSONSchema() *jsonschema.Schema {
	names := All()
	enum := make([]any, len(names))
	for i, h := range names {
		enum[i] = string(h)
	}
	return &jsonschema.Schema{Type: "string", Enum: enum, Description: "A supported harness"}
}

// Schema generates the JSON Schema (draft 2020-12) of Config, indented by two
// spaces and ending in a newline. Property names come from the toml tags, which
// equal the json tags; nothing is required; structs reject unknown keys.
func Schema() ([]byte, error) {
	r := &jsonschema.Reflector{
		FieldNameTag:               "toml",
		AllowAdditionalProperties:  false,
		RequiredFromJSONSchemaTags: true,
	}
	s := r.Reflect(&Config{})
	annotate(s)
	s.ID = SchemaID
	s.Title = "agents configuration"
	s.Description = "The standalone agents configuration file (config.toml or a project agents.toml)."
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	return append(b, '\n'), nil
}

// annotate appends the support of every key, from the feature table, to the
// description of its property. Profiles and OS overlays repeat the same keys
// and are left alone. A "*" stands for a free map key in the path.
func annotate(root *jsonschema.Schema) {
	top := definition(root, root)
	if top == nil {
		return
	}
	walkProperties(root, top, "", map[*jsonschema.Schema]bool{top: true})
}

// definition resolves a #/$defs reference of s, or returns s without one.
func definition(root, s *jsonschema.Schema) *jsonschema.Schema {
	name, ok := strings.CutPrefix(s.Ref, "#/$defs/")
	if !ok {
		return s
	}
	return root.Definitions[name]
}

func walkProperties(root, s *jsonschema.Schema, prefix string, seen map[*jsonschema.Schema]bool) {
	if s == nil || s.Properties == nil {
		return
	}
	for p := s.Properties.Oldest(); p != nil; p = p.Next() {
		if prefix == "" && (p.Key == "profiles" || p.Key == "os") {
			continue
		}
		path := p.Key
		if prefix != "" {
			path = prefix + "." + p.Key
		}
		prop := p.Value
		def := definition(root, prop)
		switch {
		case def != nil && def.Properties != nil:
			if !seen[def] {
				seen[def] = true
				walkProperties(root, def, path, seen)
			}
		case prop.AdditionalProperties != nil && definition(root, prop.AdditionalProperties).Properties != nil:
			describe(prop, path)
			def := definition(root, prop.AdditionalProperties)
			if !seen[def] {
				seen[def] = true
				walkProperties(root, def, path+".*", seen)
			}
		default:
			describe(prop, path)
		}
	}
}

// describe appends the feature support of path to the description of prop.
// The property keeps its own wording; the feature summary stands in only for a
// property that has none.
func describe(prop *jsonschema.Schema, path string) {
	if prop.Description == "" {
		prop.Description = feature.Describe(path)
		return
	}
	if support := feature.Support(path); support != "" {
		prop.Description += ". " + support
	}
}
