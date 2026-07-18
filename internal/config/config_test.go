package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "arch.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestLoadValid(t *testing.T) {
	cfg, err := load(t, `
layers:
  domain:
    path: "src/domain/**"
    description: business logic
  ui:
    path: ["apps/web/**", "src/components/**"]
rules:
  - allow: ui -> domain
  - deny: domain -> "*"
    reason: keep the domain pure
metrics:
  - target: "src/hooks/**"
    max-lines: 150
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(cfg.Layers["ui"].Path); got != 2 {
		t.Errorf("ui paths = %d, want 2", got)
	}
	r := cfg.Rules[1]
	if r.Kind != KindDeny || r.From != "domain" || r.To != "*" {
		t.Errorf("deny rule parsed wrong: %+v", r)
	}
}

func TestLoadRejectsUnknownLayer(t *testing.T) {
	_, err := load(t, `
layers:
  domain:
    path: "src/domain/**"
rules:
  - allow: domain -> infra
`)
	if err == nil || !strings.Contains(err.Error(), "unknown layer") {
		t.Errorf("want unknown layer error, got %v", err)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	_, err := load(t, `
layers:
  domain:
    path: "src/domain/**"
ruless:
  - allow: a -> b
`)
	if err == nil {
		t.Error("want error for misspelled field, got nil")
	}
}

func TestLoadRejectsMetricWithoutLimit(t *testing.T) {
	_, err := load(t, `
layers:
  domain:
    path: "src/**"
metrics:
  - target: "src/**"
`)
	if err == nil || !strings.Contains(err.Error(), "at least one limit") {
		t.Errorf("want limit error, got %v", err)
	}
}
