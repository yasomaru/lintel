package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasomaru/lintel/internal/analyze"
	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/scan"
)

// project builds a temp project from path -> content and runs the pipeline.
func project(t *testing.T, cfgYAML string, files map[string]string) []Violation {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(root, "arch.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	scanned, err := scan.Walk(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rels := make([]string, len(scanned))
	for i, f := range scanned {
		rels[i] = f.Path
	}
	proj := analyze.NewProject(root, rels, analyze.Options{Patterns: TextPatterns(cfg), Aliases: cfg.AliasMap()})
	results := make(map[string]*analyze.Result)
	for _, f := range scanned {
		res, err := proj.File(f.Path)
		if err != nil {
			t.Fatalf("analyze %s: %v", f.Path, err)
		}
		results[f.Path] = res
	}
	return Check(cfg, root, scanned, results)
}

const layeredCfg = `
layers:
  domain:
    path: "src/domain/**"
  infra:
    path: "src/infra/**"
rules:
  - allow: infra -> domain
  - deny: domain -> "*"
    reason: keep the domain pure
`

func TestDenyViolation(t *testing.T) {
	vs := project(t, layeredCfg, map[string]string{
		"src/domain/user.ts": `import { db } from "../infra/db";`,
		"src/infra/db.ts":    `export const db = 1;`,
	})
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want 1: %+v", len(vs), vs)
	}
	v := vs[0]
	if v.File != "src/domain/user.ts" || v.Line != 1 {
		t.Errorf("wrong location: %+v", v)
	}
	if !strings.Contains(v.Rule, "deny") || v.Reason != "keep the domain pure" {
		t.Errorf("rule/reason not carried: %+v", v)
	}
}

func TestAllowedEdgeAndExternals(t *testing.T) {
	vs := project(t, layeredCfg, map[string]string{
		"src/infra/db.ts":    `import { User } from "../domain/user";` + "\n" + `import pg from "pg";`,
		"src/domain/user.ts": `export type User = { id: string };`,
	})
	if len(vs) != 0 {
		t.Fatalf("violations = %d, want 0: %+v", len(vs), vs)
	}
}

func TestStrictUndeclared(t *testing.T) {
	cfg := `
strict: true
layers:
  a:
    path: "a/**"
  b:
    path: "b/**"
rules:
  - deny: b -> a
`
	vs := project(t, cfg, map[string]string{
		"a/x.ts": `import { y } from "../b/y";`,
		"b/y.ts": `export const y = 1;`,
	})
	if len(vs) != 1 || !strings.Contains(vs[0].Rule, "strict") {
		t.Fatalf("want 1 strict violation, got %+v", vs)
	}
}

func TestMetrics(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
metrics:
  - target: "src/hooks/**"
    max-lines: 3
    max-imports: 1
    reason: split fat hooks
`
	vs := project(t, cfg, map[string]string{
		"src/hooks/useBig.ts": "import a from \"./a\";\nimport b from \"./b\";\nconst x = 1;\nconst y = 2;\nexport default x;",
		"src/hooks/a.ts":      "export default 1;",
		"src/hooks/b.ts":      "export default 2;",
	})
	var got []string
	for _, v := range vs {
		if v.File == "src/hooks/useBig.ts" {
			got = append(got, v.Rule)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want max-lines and max-imports violations, got %+v", vs)
	}
}

func TestBaselineFilter(t *testing.T) {
	vs := []Violation{
		{File: "a.ts", Rule: "deny: x -> y", Detail: "d1"},
		{File: "b.ts", Rule: "deny: x -> y", Detail: "d2"},
	}
	path := filepath.Join(t.TempDir(), "base.json")
	if err := WriteBaseline(path, vs[:1]); err != nil {
		t.Fatal(err)
	}
	b, err := LoadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	fresh, baselined := b.Filter(vs)
	if len(fresh) != 1 || fresh[0].File != "b.ts" || len(baselined) != 1 {
		t.Errorf("filter wrong: fresh=%+v baselined=%+v", fresh, baselined)
	}
}

func TestGoImports(t *testing.T) {
	cfg := `
layers:
  core:
    path: "internal/**"
  cli:
    path: "cmd/**"
rules:
  - allow: cli -> core
  - deny: core -> cli
`
	vs := project(t, cfg, map[string]string{
		"go.mod":               "module example.com/app\n",
		"cmd/app/main.go":      "package main\n\nimport (\n\t\"fmt\"\n\t\"example.com/app/internal/core\"\n)\n\nfunc main() { fmt.Println(core.V) }\n",
		"internal/core/c.go":   "package core\n\nvar V = 1\n",
		"internal/core/bad.go": "package core\n\nimport _ \"example.com/app/cmd/app\"\n",
	})
	if len(vs) != 1 || vs[0].File != "internal/core/bad.go" {
		t.Fatalf("want 1 violation in bad.go, got %+v", vs)
	}
}
