package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasomaru/lintel/internal/analyze"
	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/rules"
	"github.com/yasomaru/lintel/internal/scan"
)

func tree(t *testing.T, files map[string]string) string {
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
	return root
}

// generate runs Generate and asserts the output loads AND passes a full
// check against the very tree it was generated from.
func generate(t *testing.T, files map[string]string) string {
	t.Helper()
	root := tree(t, files)
	out, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, "arch.yaml")
	if err := os.WriteFile(cfgPath, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("generated config does not load: %v\n---\n%s", err, out)
	}
	scanned, err := scan.Walk(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rels := make([]string, len(scanned))
	for i, f := range scanned {
		rels[i] = f.Path
	}
	results := analyze.NewProject(root, rels, analyze.Options{}).All(rels)
	if vs := rules.Check(cfg, root, scanned, results); len(vs) != 0 {
		t.Fatalf("generated config fails on its own tree: %+v\n---\n%s", vs, out)
	}
	return out
}

func TestGenerateRecordsObservedEdges(t *testing.T) {
	out := generate(t, map[string]string{
		"src/domain/user.ts":        "export const u = 1;",
		"src/infra/db.ts":           `import { u } from "../domain/user"; export const db = u;`,
		"src/hooks/useAuth.ts":      `import { db } from "../infra/db"; export const a = db;`,
		"apps/web/components/b.tsx": "export const b = 1;",
	})
	for _, want := range []string{
		`domain:`, `path: "src/domain/**"`, `infra:`, `hooks:`, `components:`,
		"strict: true",
		"allow: infra -> domain",
		"allow: hooks -> infra",
		`deny: domain -> "*"`, // domain has no outgoing deps -> locked down
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestGenerateImpureDomainOnlySuggestsDeny(t *testing.T) {
	out := generate(t, map[string]string{
		"src/domain/user.ts": `import { db } from "../infra/db"; export const u = db;`,
		"src/infra/db.ts":    "export const db = 1;",
	})
	if !strings.Contains(out, "allow: domain -> infra") {
		t.Errorf("observed edge missing:\n%s", out)
	}
	if !strings.Contains(out, `# - deny: domain -> "*"`) {
		t.Errorf("deny should be a commented suggestion for an impure domain:\n%s", out)
	}
	if strings.Contains(out, "\n  - deny: domain") {
		t.Errorf("deny must not be active for an impure domain:\n%s", out)
	}
}

func TestGenerateGroupsSameNameAcrossTrees(t *testing.T) {
	out := generate(t, map[string]string{
		"apps/web/components/a.tsx":     "export const a = 1;",
		"packages/ui2/components/b.tsx": "export const b = 1;",
	})
	if !strings.Contains(out, `path: ["apps/web/components/**", "packages/ui2/components/**"]`) {
		t.Errorf("components not grouped:\n%s", out)
	}
}

func TestGenerateFallsBackToTopLevelDirs(t *testing.T) {
	out := generate(t, map[string]string{
		"backend/main.go":  "package main\n",
		"frontend/app.tsx": "export const a = 1;",
	})
	for _, want := range []string{`backend:`, `frontend:`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestGenerateSkipsNestedKnownDirs(t *testing.T) {
	out := generate(t, map[string]string{
		"internal/models/user.go": "package models\n",
		"internal/x/y.go":         "package x\n",
	})
	if !strings.Contains(out, `path: "internal/**"`) {
		t.Errorf("want shallow internal/** layer:\n%s", out)
	}
	if strings.Contains(out, "internal/models/**") {
		t.Errorf("nested known dir should defer to ancestor:\n%s", out)
	}
}

func TestGenerateErrorsOnEmptyTree(t *testing.T) {
	root := tree(t, map[string]string{"README.md": "# nothing"})
	if _, err := Generate(root); err == nil {
		t.Error("want error for tree without source files")
	}
}
