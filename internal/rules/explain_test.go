package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yasomaru/lintel/internal/config"
)

func TestExplain(t *testing.T) {
	cfgYAML := `
layers:
  domain:
    path: "src/domain/**"
    description: Business logic.
  infra:
    path: "src/infra/**"
rules:
  - deny: domain -> "*"
    reason: keep it pure
  - allow: infra -> domain
metrics:
  - target: "src/**"
    max-lines: 300
bans:
  - target: "src/domain/**"
    imports: ["axios"]
    reason: no I/O
suppressions:
  deny: ["@ts-ignore"]
pairing:
  - target: "src/domain/**/*.ts"
    requires: "tests/**/{name}.test.ts"
`
	cfgPath := filepath.Join(t.TempDir(), "arch.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	e := Explain(cfg, "src/domain/user.ts")
	if e.Layer != "domain" || e.LayerDescription != "Business logic." {
		t.Errorf("layer wrong: %+v", e)
	}
	if len(e.Dependencies) != 1 || e.Dependencies[0].Rule != `deny: domain -> "*"` {
		t.Errorf("dependencies wrong: %+v", e.Dependencies)
	}
	if len(e.Metrics) != 1 || e.Metrics[0].Rule != "max-lines: 300" {
		t.Errorf("metrics wrong: %+v", e.Metrics)
	}
	if len(e.Bans) != 1 || e.Bans[0].Reason != "no I/O" {
		t.Errorf("bans wrong: %+v", e.Bans)
	}
	if e.Suppressions == nil || e.Suppressions.Rule != "deny: @ts-ignore" {
		t.Errorf("suppressions wrong: %+v", e.Suppressions)
	}
	if len(e.Pairing) != 1 || e.Pairing[0].Rule != "requires: tests/**/user.test.ts" {
		t.Errorf("pairing wrong: %+v", e.Pairing)
	}

	// The infra file sees the allow rule but not domain's bans.
	e2 := Explain(cfg, "src/infra/db.ts")
	if len(e2.Dependencies) != 1 || e2.Dependencies[0].Rule != "allow: infra -> domain" {
		t.Errorf("infra dependencies wrong: %+v", e2.Dependencies)
	}
	if len(e2.Bans) != 0 || len(e2.Pairing) != 0 {
		t.Errorf("infra should have no bans/pairing: %+v", e2)
	}

	// A file outside every layer still sees global text rules.
	e3 := Explain(cfg, "scripts/build.ts")
	if e3.Layer != "" || len(e3.Dependencies) != 0 || e3.Suppressions == nil {
		t.Errorf("out-of-layer explanation wrong: %+v", e3)
	}
}
