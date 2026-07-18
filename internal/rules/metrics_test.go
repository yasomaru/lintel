package rules

import (
	"strings"
	"testing"
)

func TestStructuralMetricsTS(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
metrics:
  - target: "src/**"
    max-function-lines: 4
    max-params: 3
    max-nesting-depth: 2
    reason: keep functions small
`
	vs := project(t, cfg, map[string]string{
		"src/a.ts": `export function longOne(a: number) {
  let x = a;
  x += 1;
  x += 2;
  x += 3;
  return x;
}
const manyParams = (a: number, b: number, c: Map<string, number>, d: string) => a;
function deep(a: number) {
  if (a > 0) { if (a > 1) { if (a > 2) { return 3; } } }
  return 0;
}
export function fine(a: number, b: number) { return a + b; }
`,
	})
	got := strings.Join(rulesOf(vs), "; ")
	details := ""
	for _, v := range vs {
		details += v.Detail + "; "
	}
	if len(vs) != 3 {
		t.Fatalf("violations = %d, want 3: %v / %v", len(vs), got, details)
	}
	for _, want := range []string{
		"longOne: 7 lines (limit 4)",
		"manyParams: 4 parameters (limit 3)",
		"deep: 3 nesting levels (limit 2)",
	} {
		if !strings.Contains(details, want) {
			t.Errorf("missing %q in %v", want, details)
		}
	}
}

func TestMaxPublicMethodsJava(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
metrics:
  - target: "src/**"
    max-public-methods: 2
`
	vs := project(t, cfg, map[string]string{
		"src/God.java": `public class GodService {
  public void a() {}
  public void b() {}
  public void c() {}
  private void hidden() {}
  void packagePrivate() {}
}
`,
		"src/Ok.java": `public class OkService {
  public void a() {}
  private void b() {}
}
`,
	})
	if len(vs) != 1 || !strings.Contains(vs[0].Detail, "GodService: 3 public methods (limit 2)") {
		t.Fatalf("want 1 GodService violation, got %+v", vs)
	}
}

func TestMaxPublicMethodsGoReceiver(t *testing.T) {
	cfg := `
layers:
  app:
    path: "**"
metrics:
  - target: "**/*.go"
    max-public-methods: 1
`
	vs := project(t, cfg, map[string]string{
		"svc.go": `package svc

type Server struct{}

func (s *Server) Handle() {}
func (s *Server) Serve() {}
func (s *Server) internal() {}

type Small struct{}

func (s Small) Only() {}
`,
	})
	if len(vs) != 1 || !strings.Contains(vs[0].Detail, "Server: 2 public methods (limit 1)") {
		t.Fatalf("want 1 Server violation, got %+v", vs)
	}
}

func TestMaxParamsPythonExcludesSelf(t *testing.T) {
	cfg := `
layers:
  app:
    path: "**"
metrics:
  - target: "**/*.py"
    max-params: 2
`
	vs := project(t, cfg, map[string]string{
		"svc.py": `class Service:
    def ok(self, a, b):
        return a

    def too_many(self, a, b, c):
        return a
`,
	})
	if len(vs) != 1 || !strings.Contains(vs[0].Detail, "too_many: 3 parameters (limit 2)") {
		t.Fatalf("want 1 too_many violation, got %+v", vs)
	}
}
