package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasomaru/lintel/internal/config"
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

// generate runs Generate and asserts the output is a loadable config.
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
	if _, err := config.Load(cfgPath); err != nil {
		t.Fatalf("generated config does not load: %v\n---\n%s", err, out)
	}
	return out
}

func TestGenerateDetectsKnownLayers(t *testing.T) {
	out := generate(t, map[string]string{
		"src/domain/user.ts":        "export const u = 1;",
		"src/infra/db.ts":           "export const db = 1;",
		"src/hooks/useAuth.ts":      "export const a = 1;",
		"apps/web/components/b.tsx": "export const b = 1;",
	})
	for _, want := range []string{
		`domain:`, `path: "src/domain/**"`,
		`infra:`, `hooks:`, `components:`,
		`deny: domain -> "*"`, `allow: infra -> domain`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
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
