package config_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/giocaizzi/agents/internal/config"
)

var update = flag.Bool("update", false, "rewrite schema/config.schema.json")

const schemaPath = "../../schema/config.schema.json"

func TestSchemaDrift(t *testing.T) {
	t.Parallel()
	got, err := config.Schema()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if werr := os.WriteFile(schemaPath, got, 0o600); werr != nil {
			t.Fatal(werr)
		}
		return
	}
	want, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))) {
		t.Error("schema/config.schema.json is stale; run go test ./internal/config -run TestSchemaDrift -update")
	}
}

func compile(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if aerr := c.AddResource(config.SchemaID, doc); aerr != nil {
		t.Fatal(aerr)
	}
	s, err := c.Compile(config.SchemaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// jsonValue decodes TOML into values the schema validator accepts.
func jsonValue(t *testing.T, data []byte) any {
	t.Helper()
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func tomlFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".toml") {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("no testdata under %s", dir)
	}
	return out
}

func TestValidFilesMatchSchema(t *testing.T) {
	t.Parallel()
	s := compile(t)
	for _, f := range tomlFiles(t, "testdata/valid") {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			v := jsonValue(t, data)
			if v == nil {
				t.Fatal("not valid TOML")
			}
			if err := s.Validate(v); err != nil {
				t.Errorf("schema rejects a valid file: %v", err)
			}
		})
	}
}

func TestInvalidFilesFail(t *testing.T) {
	t.Parallel()
	s := compile(t)
	for _, f := range tomlFiles(t, "testdata/invalid") {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if v := jsonValue(t, data); v != nil && s.Validate(v) != nil {
				return
			}
			r, err := config.Load(config.Options{
				Getenv:     func(string) string { return "" },
				GOOS:       "linux",
				Home:       t.TempDir(),
				ConfigPath: f,
			})
			if err != nil {
				return
			}
			if fs := config.Validate(r.Config); len(fs) == 0 {
				t.Error("neither the schema, Load nor Validate rejects an invalid file")
			}
		})
	}
}
