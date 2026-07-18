package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setup writes a project into a temp dir and chdirs into it.
func setup(t *testing.T, files map[string]string) {
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
	t.Chdir(root)
}

func runCmd(t *testing.T, command string, args ...string) (string, error) {
	t.Helper()
	var b strings.Builder
	err := run(&b, command, args)
	return b.String(), err
}

var violatingProject = map[string]string{
	"arch.yaml": `layers:
  domain:
    path: "src/domain/**"
  infra:
    path: "src/infra/**"
rules:
  - allow: infra -> domain
  - deny: domain -> "*"
    reason: keep the domain pure
baseline: .lintel-baseline.json
`,
	"src/domain/user.ts": `import { db } from "../infra/db";` + "\n" + `export const u = db;`,
	"src/infra/db.ts":    `export const db = 1;`,
}

func TestCheckReportsViolationAndExitSignal(t *testing.T) {
	setup(t, violatingProject)
	out, err := runCmd(t, "check")
	if !errors.Is(err, errViolations) {
		t.Fatalf("want errViolations, got %v", err)
	}
	if !strings.Contains(out, "deny: domain ->") || !strings.Contains(out, "keep the domain pure") {
		t.Errorf("output missing violation details:\n%s", out)
	}
}

func TestCheckJSONFormat(t *testing.T) {
	setup(t, violatingProject)
	out, err := runCmd(t, "check", "--format", "json")
	if !errors.Is(err, errViolations) {
		t.Fatalf("want errViolations, got %v", err)
	}
	var sum struct {
		Violations []struct {
			File     string `json:"file"`
			Severity string `json:"severity"`
		} `json:"violations"`
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &sum); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if sum.OK || len(sum.Violations) != 1 || sum.Violations[0].Severity != "error" {
		t.Errorf("unexpected summary: %+v", sum)
	}
}

func TestCheckGithubFormat(t *testing.T) {
	setup(t, violatingProject)
	out, err := runCmd(t, "check", "--format", "github")
	if !errors.Is(err, errViolations) {
		t.Fatalf("want errViolations, got %v", err)
	}
	if !strings.Contains(out, "::error file=src/domain/user.ts,line=1,") {
		t.Errorf("missing annotation:\n%s", out)
	}
}

func TestCheckUnknownFormat(t *testing.T) {
	setup(t, violatingProject)
	if _, err := runCmd(t, "check", "--format", "xml"); err == nil || errors.Is(err, errViolations) {
		t.Fatalf("want format error, got %v", err)
	}
}

func TestBaselineThenCheckPasses(t *testing.T) {
	setup(t, violatingProject)
	out, err := runCmd(t, "baseline")
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if !strings.Contains(out, "baseline written") {
		t.Errorf("baseline output: %s", out)
	}
	out, err = runCmd(t, "check")
	if err != nil {
		t.Fatalf("check after baseline should pass, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 baselined") {
		t.Errorf("expected baselined count:\n%s", out)
	}
}

func TestInitAndInitScan(t *testing.T) {
	setup(t, map[string]string{"src/domain/a.ts": "export const a = 1;"})
	out, err := runCmd(t, "init", "--scan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "wrote arch.yaml") {
		t.Errorf("init output: %s", out)
	}
	data, err := os.ReadFile("arch.yaml")
	if err != nil || !strings.Contains(string(data), "domain:") {
		t.Errorf("generated config wrong: %v\n%s", err, data)
	}
	if _, err := runCmd(t, "init"); err == nil {
		t.Error("second init should fail: arch.yaml exists")
	}
}

func TestGraphMermaidAndDot(t *testing.T) {
	setup(t, violatingProject)
	out, err := runCmd(t, "graph")
	if err != nil || !strings.Contains(out, "graph LR") {
		t.Errorf("mermaid graph wrong (%v):\n%s", err, out)
	}
	out, err = runCmd(t, "graph", "--format", "dot")
	if err != nil || !strings.Contains(out, "digraph lintel {") {
		t.Errorf("dot graph wrong (%v):\n%s", err, out)
	}
}

func TestRulesFindsNearestConfig(t *testing.T) {
	setup(t, violatingProject)
	out, err := runCmd(t, "rules", "src/domain/user.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"layer": "domain"`) || !strings.Contains(out, "keep the domain pure") {
		t.Errorf("rules output wrong:\n%s", out)
	}
}

func TestContextSummary(t *testing.T) {
	setup(t, violatingProject)
	out, err := runCmd(t, "context")
	if err != nil || !strings.Contains(out, "## Architecture rules") {
		t.Errorf("context wrong (%v):\n%s", err, out)
	}
}

func TestSchemaAndVersionAndHelp(t *testing.T) {
	out, err := runCmd(t, "schema")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Errorf("schema is not JSON: %v", err)
	}
	if out, err := runCmd(t, "version"); err != nil || !strings.Contains(out, "lintel dev") {
		t.Errorf("version wrong (%v): %s", err, out)
	}
	if out, err := runCmd(t, "help"); err != nil || !strings.Contains(out, "Usage:") {
		t.Errorf("help wrong (%v): %s", err, out)
	}
	if _, err := runCmd(t, "nonsense"); err == nil {
		t.Error("unknown command should error")
	}
}
