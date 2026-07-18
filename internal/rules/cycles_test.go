package rules

import (
	"strings"
	"testing"
)

func TestCycleDetection(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
cycles:
  deny: true
  reason: break the loop
`
	vs := project(t, cfg, map[string]string{
		"src/a.ts": `import { b } from "./b";`,
		"src/b.ts": `import { c } from "./c";`,
		"src/c.ts": `import { a } from "./a"; export const c = 1;`,
		"src/d.ts": `import { a } from "./a"; export const d = 1;`, // not in the cycle
	})
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want 1: %+v", len(vs), vs)
	}
	v := vs[0]
	if !strings.Contains(v.Rule, "cycles") || !strings.Contains(v.Detail, "3 files") {
		t.Errorf("wrong cycle violation: %+v", v)
	}
	for _, f := range []string{"src/a.ts", "src/b.ts", "src/c.ts"} {
		if !strings.Contains(v.Detail, f) {
			t.Errorf("cycle member %s missing from detail: %s", f, v.Detail)
		}
	}
	if strings.Contains(v.Detail, "src/d.ts") {
		t.Errorf("d.ts is not part of the cycle: %s", v.Detail)
	}
}

func TestNoCycleNoViolation(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
cycles:
  deny: true
`
	vs := project(t, cfg, map[string]string{
		"src/a.ts": `import { b } from "./b";`,
		"src/b.ts": `export const b = 1;`,
	})
	if len(vs) != 0 {
		t.Fatalf("violations = %d, want 0: %+v", len(vs), vs)
	}
}

func TestEncapsulation(t *testing.T) {
	cfg := `
layers:
  domain:
    path: "src/domain/**"
  ui:
    path: "src/ui/**"
encapsulation:
  - layer: domain
    entry: "src/domain/index.ts"
    reason: internals are private
`
	vs := project(t, cfg, map[string]string{
		"src/domain/index.ts":    `export * from "./user";`,
		"src/domain/user.ts":     `export const u = 1;`,
		"src/ui/good.ts":         `import { u } from "../domain/index";`,
		"src/ui/bad.ts":          `import { u } from "../domain/user";`,
		"src/domain/internal.ts": `import { u } from "./user"; export const i = u;`, // same layer: free
	})
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want 1: %+v", len(vs), vs)
	}
	v := vs[0]
	if v.File != "src/ui/bad.ts" || !strings.Contains(v.Rule, "encapsulation") {
		t.Errorf("wrong violation: %+v", v)
	}
	if !strings.Contains(v.Detail, "src/domain/index.ts") {
		t.Errorf("detail should point at the entry file: %s", v.Detail)
	}
}

func TestReactHookMetrics(t *testing.T) {
	cfg := `
layers:
  hooks:
    path: "src/hooks/**"
metrics:
  - target: "src/hooks/**"
    max-use-state: 2
    max-use-effect: 1
    reason: fat hook
`
	vs := project(t, cfg, map[string]string{
		"src/hooks/useBig.ts": `import { useState, useEffect } from "react";
const [a, setA] = useState(0);
const [b, setB] = useState(0);
const [c, setC] = useState(0);
useEffect(() => {}, []);
useEffect(() => {}, [a]);
export default a;`,
		"src/hooks/useOk.ts": `import { useState } from "react";
const [a, setA] = useState(0);
export default a;`,
	})
	var got []string
	for _, v := range vs {
		got = append(got, v.Rule+"|"+v.Detail)
	}
	joined := strings.Join(got, "; ")
	if len(vs) != 2 {
		t.Fatalf("violations = %d, want 2: %v", len(vs), joined)
	}
	if !strings.Contains(joined, "max-use-state: 2|3 useState calls (limit 2)") ||
		!strings.Contains(joined, "max-use-effect: 1|2 useEffect calls (limit 1)") {
		t.Errorf("wrong metric violations: %v", joined)
	}
	for _, v := range vs {
		if v.File != "src/hooks/useBig.ts" {
			t.Errorf("useOk.ts should not violate: %+v", v)
		}
	}
}

func TestSeverityWarn(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
metrics:
  - target: "src/**"
    max-lines: 1
    severity: warn
bans:
  - target: "src/**"
    imports: ["axios"]
`
	vs := project(t, cfg, map[string]string{
		"src/a.ts": `import axios from "axios";` + "\n" + `export const a = 1;`,
	})
	if len(vs) != 2 {
		t.Fatalf("violations = %d, want 2: %+v", len(vs), vs)
	}
	bySeverity := map[string]int{}
	for _, v := range vs {
		bySeverity[v.Severity]++
	}
	if bySeverity["warn"] != 1 || bySeverity["error"] != 1 {
		t.Errorf("severities wrong: %+v", vs)
	}
	if CountErrors(vs) != 1 {
		t.Errorf("CountErrors = %d, want 1", CountErrors(vs))
	}
}
