package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

// proj builds a temp project from path -> content.
func proj(t *testing.T, opts Options, files map[string]string) *Project {
	t.Helper()
	root := t.TempDir()
	rels := make([]string, 0, len(files))
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		rels = append(rels, rel)
	}
	return NewProject(root, rels, opts)
}

func analyzeOne(t *testing.T, p *Project, rel string) *Result {
	t.Helper()
	res, err := p.File(rel)
	if err != nil {
		t.Fatalf("analyze %s: %v", rel, err)
	}
	return res
}

func resolved(res *Result) map[string]string {
	out := map[string]string{}
	for _, imp := range res.Imports {
		out[imp.Raw] = imp.Resolved
	}
	return out
}

func TestJSImportForms(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"src/a.ts": `import { x } from "./b";
export { y } from "./c";
const d = require("./d");
const e = await import("./sub");
import "./e.css";
import react from "react";`,
		"src/b.ts":         `export const x = 1;`,
		"src/c.tsx":        `export const y = 1;`,
		"src/d.js":         `module.exports = 1;`,
		"src/sub/index.ts": `export default 1;`,
	})
	res := analyzeOne(t, p, "src/a.ts")
	got := resolved(res)
	want := map[string]string{
		"./b": "src/b.ts", "./c": "src/c.tsx", "./d": "src/d.js",
		"./sub": "src/sub/index.ts", "react": "",
	}
	for raw, target := range want {
		if got[raw] != target {
			t.Errorf("resolve(%q) = %q, want %q", raw, got[raw], target)
		}
	}
	if res.Imports[0].Line != 1 || len(res.Imports) < 5 {
		t.Errorf("line numbers or count wrong: %+v", res.Imports)
	}
}

func TestManualAlias(t *testing.T) {
	p := proj(t, Options{Aliases: map[string][]string{"@/*": {"src/*"}}}, map[string]string{
		"src/domain/user.ts": `export const u = 1;`,
		"src/app.ts":         `import { u } from "@/domain/user";`,
	})
	got := resolved(analyzeOne(t, p, "src/app.ts"))
	if got["@/domain/user"] != "src/domain/user.ts" {
		t.Errorf("alias not resolved: %v", got)
	}
}

func TestTsconfigAlias(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"tsconfig.json": `{
  // JSONC: comments and trailing commas are legal here
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "~/*": ["src/*"], /* block comment */
    },
  },
}`,
		"src/lib/util.ts": `export const util = 1;`,
		"src/main.ts":     `import { util } from "~/lib/util";`,
	})
	got := resolved(analyzeOne(t, p, "src/main.ts"))
	if got["~/lib/util"] != "src/lib/util.ts" {
		t.Errorf("tsconfig alias not resolved: %v", got)
	}
}

func TestPythonImports(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"app/models/user.py":     `class User: pass`,
		"app/models/__init__.py": ``,
		"app/service/order.py": `from app.models.user import User
from .helper import calc
from ..models import user
import os`,
		"app/service/helper.py": `def calc(): pass`,
	})
	got := resolved(analyzeOne(t, p, "app/service/order.py"))
	want := map[string]string{
		"app.models.user": "app/models/user.py",
		".helper":         "app/service/helper.py",
		"..models":        "app/models/__init__.py",
		"os":              "",
	}
	for raw, target := range want {
		if got[raw] != target {
			t.Errorf("resolve(%q) = %q, want %q", raw, got[raw], target)
		}
	}
}

func TestGoImportForms(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"go.mod":          "module example.com/app\n",
		"internal/x/x.go": "package x\n\nvar V = 1\n",
		"single.go":       "package main\n\nimport \"example.com/app/internal/x\"\n",
		"block.go":        "package main\n\nimport (\n\t\"fmt\"\n\t\"example.com/app/internal/x\"\n)\n",
	})
	for _, file := range []string{"single.go", "block.go"} {
		got := resolved(analyzeOne(t, p, file))
		if got["example.com/app/internal/x"] != "internal/x/x.go" {
			t.Errorf("%s: module import not resolved: %v", file, got)
		}
	}
}

func TestExports(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"a.ts": "export class UserRepository {}\nexport default function main() {}\nconst hidden = 1;",
		"b.go": "package b\n\nfunc Public() {}\n\nfunc private() {}\n\ntype Thing struct{}\n",
		"c.py": "class Service: pass\ndef handler(): pass\ndef _private(): pass\n",
	})
	cases := map[string][]string{
		"a.ts": {"UserRepository", "main"},
		"b.go": {"Public", "Thing"},
		"c.py": {"Service", "handler"},
	}
	for file, want := range cases {
		res := analyzeOne(t, p, file)
		var names []string
		for _, s := range res.Exports {
			names = append(names, s.Name)
		}
		if len(names) != len(want) {
			t.Errorf("%s exports = %v, want %v", file, names, want)
			continue
		}
		for i := range want {
			if names[i] != want[i] {
				t.Errorf("%s exports = %v, want %v", file, names, want)
			}
		}
	}
}

func TestScanPatterns(t *testing.T) {
	p := proj(t, Options{Patterns: []string{"@ts-ignore", "console.log"}}, map[string]string{
		"a.ts": "// @ts-ignore\nconsole.log(1);\nconst ok = 1;",
	})
	res := analyzeOne(t, p, "a.ts")
	if len(res.Hits) != 2 || res.Hits[0].Line != 1 || res.Hits[1].Line != 2 {
		t.Errorf("hits = %+v", res.Hits)
	}
}

func TestAllParallel(t *testing.T) {
	files := map[string]string{}
	var rels []string
	for _, r := range []string{"a.ts", "b.ts", "c.ts", "sub/d.ts"} {
		files[r] = `import { x } from "./nothing"; export const v = 1;`
		rels = append(rels, r)
	}
	p := proj(t, Options{}, files)
	results := p.All(rels)
	if len(results) != len(rels) {
		t.Fatalf("results = %d, want %d", len(results), len(rels))
	}
	for rel, res := range results {
		if res.Lines != 1 {
			t.Errorf("%s: lines = %d", rel, res.Lines)
		}
	}
}
