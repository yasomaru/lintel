package config

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestSchemaIsValidJSON(t *testing.T) {
	var v map[string]any
	if err := json.Unmarshal(SchemaJSON, &v); err != nil {
		t.Fatalf("embedded schema is not valid JSON: %v", err)
	}
	props, ok := v["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties object")
	}
	// Every top-level config key must be documented in the schema.
	for _, key := range []string{
		"layers", "rules", "metrics", "naming", "bans", "suppressions",
		"placeholders", "dependencies", "coverage", "pairing", "resolve",
		"baseline", "strict",
	} {
		if _, ok := props[key]; !ok {
			t.Errorf("schema is missing top-level property %q", key)
		}
	}
}

func TestSchemaDocsCopyInSync(t *testing.T) {
	docs, err := os.ReadFile("../../docs/arch.schema.json")
	if err != nil {
		t.Fatalf("docs/arch.schema.json missing: %v", err)
	}
	if !bytes.Equal(docs, SchemaJSON) {
		t.Error("docs/arch.schema.json is out of sync — copy internal/config/arch.schema.json over it")
	}
}
