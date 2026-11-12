package config

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
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
	s.ID = SchemaID
	s.Title = "agents configuration"
	s.Description = "The standalone agents configuration file (config.toml or a project agents.toml)."
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	return append(b, '\n'), nil
}
