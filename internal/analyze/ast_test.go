package analyze

import (
	"strings"
	"testing"
)

// These behaviors are only possible with the AST engine — the regex
// backend cannot distinguish code from comments and strings. They double
// as proof that the engine is active and not silently falling back.

func TestASTIgnoresCommentedImports(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"src/real.ts": `export const real = 1;`,
		"src/a.ts": `// import { dead } from "./real";
/* import { dead2 } from "./real"; */
import { real } from "./real";`,
	})
	res := analyzeOne(t, p, "src/a.ts")
	if len(res.Imports) != 1 {
		t.Fatalf("imports = %+v, want exactly the real one", res.Imports)
	}
	if res.Imports[0].Line != 3 {
		t.Errorf("line = %d, want 3", res.Imports[0].Line)
	}
}

func TestASTIgnoresImportLikeStrings(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"src/real.py":  `REAL = 1`,
		"src/a.py":     "doc = \"import src.real\"\n# import src.real\nimport src.real\n",
		"src/real2.go": "package real2\n",
	})
	res := analyzeOne(t, p, "src/a.py")
	if len(res.Imports) != 1 || res.Imports[0].Line != 3 {
		t.Fatalf("imports = %+v, want only the line-3 import", res.Imports)
	}
}

func TestASTIgnoresCommentedGoImports(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"go.mod":          "module example.com/app\n",
		"internal/x/x.go": "package x\n\nvar V = 1\n",
		"main.go":         "package main\n\n// import \"example.com/app/internal/x\"\n\nimport \"example.com/app/internal/x\"\n",
	})
	res := analyzeOne(t, p, "main.go")
	if len(res.Imports) != 1 || res.Imports[0].Line != 5 {
		t.Fatalf("imports = %+v, want only the line-5 import", res.Imports)
	}
}

func TestASTGoVarBlockExports(t *testing.T) {
	// The regex backend missed exported names inside var/const blocks;
	// the AST backend must find them.
	p := proj(t, Options{}, map[string]string{
		"b.go": "package b\n\nvar (\n\tExported = 1\n\thidden   = 2\n)\n",
	})
	res := analyzeOne(t, p, "b.go")
	var names []string
	for _, s := range res.Exports {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "Exported" {
		t.Fatalf("exports = %v, want [Exported]", names)
	}
}

func TestRegexFallbackViaEnv(t *testing.T) {
	t.Setenv("LINTEL_ENGINE", "regex")
	p := proj(t, Options{}, map[string]string{
		"src/real.ts": `export const real = 1;`,
		"src/a.ts":    `import { real } from "./real";`,
	})
	res := analyzeOne(t, p, "src/a.ts")
	if len(res.Imports) != 1 || res.Imports[0].Resolved != "src/real.ts" {
		t.Fatalf("regex fallback broken: %+v", res.Imports)
	}
}
