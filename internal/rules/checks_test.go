package rules

import (
	"strings"
	"testing"
)

func rulesOf(vs []Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Rule
	}
	return out
}

func TestNamingFileAndSymbol(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
naming:
  - target: "src/hooks/**"
    file-pattern: "use[A-Z]*.ts"
    reason: hooks start with use
  - target: "src/repository/**"
    symbol-pattern: "*Repository"
`
	vs := project(t, cfg, map[string]string{
		"src/hooks/useAuth.ts":   `export const useAuth = 1;`,
		"src/hooks/helpers.ts":   `export const x = 1;`,
		"src/repository/user.ts": `export class UserRepository {}` + "\n" + `export class UserCache {}`,
	})
	got := strings.Join(rulesOf(vs), "; ")
	if !strings.Contains(got, "file-pattern") {
		t.Errorf("want file-pattern violation for helpers.ts, got %+v", vs)
	}
	if !strings.Contains(got, "symbol-pattern") {
		t.Errorf("want symbol-pattern violation for UserCache, got %+v", vs)
	}
	if len(vs) != 2 {
		t.Errorf("violations = %d, want 2: %+v", len(vs), vs)
	}
}

func TestBans(t *testing.T) {
	cfg := `
layers:
  domain:
    path: "src/domain/**"
bans:
  - target: "src/domain/**"
    imports: ["axios", "@prisma/*"]
    calls: ["console.log"]
    reason: domain stays pure
`
	vs := project(t, cfg, map[string]string{
		"src/domain/user.ts": `import axios from "axios";` + "\n" +
			`import { PrismaClient } from "@prisma/client";` + "\n" +
			`console.log("hi");`,
		"src/domain/ok.ts": `import { z } from "zod";`,
	})
	if len(vs) != 3 {
		t.Fatalf("violations = %d, want 3: %+v", len(vs), vs)
	}
	for _, v := range vs {
		if v.File != "src/domain/user.ts" || v.Line == 0 {
			t.Errorf("bad violation location: %+v", v)
		}
	}
}

func TestSuppressionsAndPlaceholders(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
suppressions:
  deny: ["@ts-ignore"]
  reason: fix the root cause
placeholders:
  deny: ["TODO: implement"]
  except: "src/legacy/**"
`
	vs := project(t, cfg, map[string]string{
		"src/a.ts":        "// @ts-ignore\nconst x: number = \"s\";",
		"src/b.ts":        "// TODO: implement later\nexport const b = 1;",
		"src/legacy/c.ts": "// TODO: implement someday",
	})
	got := strings.Join(rulesOf(vs), "; ")
	if len(vs) != 2 || !strings.Contains(got, "suppressions") || !strings.Contains(got, "placeholders") {
		t.Fatalf("want 1 suppression + 1 placeholder (legacy excepted), got %+v", vs)
	}
}

func TestCoverageRequireLayer(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
coverage:
  require-layer: true
  except: "*.config.js"
`
	vs := project(t, cfg, map[string]string{
		"src/a.ts":       `export const a = 1;`,
		"utils/dump.ts":  `export const d = 1;`,
		"vite.config.js": `export default {};`,
	})
	if len(vs) != 1 || vs[0].File != "utils/dump.ts" {
		t.Fatalf("want 1 coverage violation for utils/dump.ts, got %+v", vs)
	}
}

func TestPairing(t *testing.T) {
	cfg := `
layers:
  usecase:
    path: "src/usecase/**"
pairing:
  - target: "src/usecase/**/*.ts"
    requires: "tests/**/{name}.test.ts"
    reason: every usecase needs a test
`
	vs := project(t, cfg, map[string]string{
		"src/usecase/create.ts":        `export const create = 1;`,
		"src/usecase/delete.ts":        `export const del = 1;`,
		"tests/usecase/create.test.ts": `import { create } from "../../src/usecase/create";`,
	})
	if len(vs) != 1 || vs[0].File != "src/usecase/delete.ts" {
		t.Fatalf("want 1 pairing violation for delete.ts, got %+v", vs)
	}
}

func TestDependencyGate(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
dependencies:
  policy: allowlist
  allow: ["react", "@tanstack/*"]
  deny: ["moment"]
  reason: keep deps curated
`
	vs := project(t, cfg, map[string]string{
		"src/a.ts": `export const a = 1;`,
		"package.json": `{
  "dependencies": {"react": "^19.0.0", "moment": "^2.30.0"},
  "devDependencies": {"left-pad": "^1.3.0"}
}`,
	})
	got := strings.Join(rulesOf(vs), "; ")
	if len(vs) != 2 {
		t.Fatalf("violations = %d, want 2 (moment denied, left-pad not allowed): %+v", len(vs), vs)
	}
	if !strings.Contains(got, "deny moment") || !strings.Contains(got, "not in allowlist") {
		t.Errorf("wrong rules: %v", got)
	}
}

func TestGoModDependencyGate(t *testing.T) {
	cfg := `
layers:
  app:
    path: "cmd/**"
dependencies:
  deny: ["github.com/pkg/errors"]
  reason: use stdlib errors
`
	vs := project(t, cfg, map[string]string{
		"cmd/main.go": "package main\n\nfunc main() {}\n",
		"go.mod":      "module example.com/app\n\ngo 1.24\n\nrequire (\n\tgithub.com/pkg/errors v0.9.1\n\tgopkg.in/yaml.v3 v3.0.1 // indirect\n)\n",
	})
	if len(vs) != 1 || vs[0].File != "go.mod" || vs[0].Line != 6 {
		t.Fatalf("want 1 go.mod violation at line 6, got %+v", vs)
	}
}
