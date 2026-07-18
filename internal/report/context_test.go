package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasomaru/lintel/internal/config"
)

func TestContext(t *testing.T) {
	cfgYAML := `
strict: true
layers:
  domain:
    path: "src/domain/**"
    description: Business logic.
  infra:
    path: "src/infra/**"
rules:
  - allow: infra -> domain
  - deny: domain -> "*"
    reason: keep it pure
cycles:
  deny: true
suppressions:
  deny: ["@ts-ignore"]
dependencies:
  policy: allowlist
  allow: ["react"]
encapsulation:
  - layer: domain
    entry: "src/domain/index.ts"
`
	cfgPath := filepath.Join(t.TempDir(), "arch.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	Context(&b, cfg)
	out := b.String()
	for _, want := range []string{
		"## Architecture rules",
		"**domain** (`src/domain/**`): Business logic.",
		"`allow: infra -> domain`",
		"— keep it pure",
		"strict mode",
		"circular dependencies",
		"`src/domain/index.ts`",
		"@ts-ignore",
		"allowlist",
		"lintel rules <path>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
