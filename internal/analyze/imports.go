// Package analyze extracts import dependencies from source files.
//
// v0 uses lightweight per-language extraction (regex for TS/JS/Python,
// go/parser for Go). This is the piece that will be replaced by a
// tree-sitter backend; the rest of the pipeline only sees []Import.
package analyze

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Import is one dependency declaration found in a file.
type Import struct {
	// Raw is the import specifier as written ("./foo", "react", "app/domain").
	Raw string
	// Resolved is the slash-relative path of the imported file within the
	// project, or "" if the import points outside the project (stdlib,
	// external package) or could not be resolved.
	Resolved string
	// Line is the 1-based line number of the declaration.
	Line int
}

// Result holds the analysis of a single file.
type Result struct {
	Imports []Import
	Lines   int
	// Exports are the file's exported top-level symbols.
	Exports []Symbol
	// Hits are occurrences of the project's forbidden text patterns.
	Hits []PatternHit
}

// Project carries context needed to resolve imports to project files.
type Project struct {
	Root string
	// GoModule is the module path from go.mod at the root, if any.
	GoModule string
	// Patterns are forbidden substrings to search for in every file
	// (suppression markers, placeholders, banned calls).
	Patterns []string
	// files is the set of known project files (slash-relative paths).
	files map[string]bool
}

// NewProject builds resolution context for the given root and file set.
func NewProject(root string, relPaths []string, patterns []string) *Project {
	p := &Project{Root: root, Patterns: patterns, files: make(map[string]bool, len(relPaths))}
	for _, f := range relPaths {
		p.files[f] = true
	}
	if data, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if mod, ok := strings.CutPrefix(line, "module "); ok {
				p.GoModule = strings.TrimSpace(mod)
				break
			}
		}
	}
	return p
}

// File analyzes one file (slash-relative path).
func (p *Project) File(rel string) (*Result, error) {
	abs := filepath.Join(p.Root, filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	src := string(data)
	res := &Result{Lines: countLines(src)}
	switch path.Ext(rel) {
	case ".go":
		res.Imports = p.goImports(src)
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		res.Imports = p.jsImports(rel, src)
	case ".py":
		res.Imports = p.pyImports(src)
	default:
		return res, fmt.Errorf("unsupported extension: %s", rel)
	}
	res.Exports = exports(rel, src)
	res.Hits = scanPatterns(src, p.Patterns)
	return res, nil
}

func countLines(src string) int {
	n := 0
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		n++
	}
	return n
}

// --- Go ---

var goImportBlock = regexp.MustCompile(`(?m)^import\s*(?:\(([^)]*)\)|"([^"]+)"|\w+\s+"([^"]+)")`)
var goImportLine = regexp.MustCompile(`"([^"]+)"`)

func (p *Project) goImports(src string) []Import {
	var out []Import
	for _, m := range goImportBlock.FindAllStringSubmatchIndex(src, -1) {
		// Single import: group 2 or 3. Block import: group 1, scan strings inside.
		for _, g := range []int{2, 3} {
			if m[2*g] >= 0 {
				raw := src[m[2*g]:m[2*g+1]]
				out = append(out, Import{Raw: raw, Resolved: p.resolveGo(raw), Line: lineAt(src, m[2*g])})
			}
		}
		if m[2] >= 0 { // block
			block := src[m[2]:m[3]]
			for _, sm := range goImportLine.FindAllStringSubmatchIndex(block, -1) {
				raw := block[sm[2]:sm[3]]
				out = append(out, Import{Raw: raw, Resolved: p.resolveGo(raw), Line: lineAt(src, m[2]+sm[2])})
			}
		}
	}
	return out
}

func (p *Project) resolveGo(raw string) string {
	if p.GoModule == "" {
		return ""
	}
	sub, ok := strings.CutPrefix(raw, p.GoModule+"/")
	if !ok {
		return ""
	}
	// A Go import points at a package directory; map it to any file inside.
	for f := range p.files {
		if path.Dir(f) == sub {
			return f
		}
	}
	return ""
}

// --- JS / TS ---

var jsImport = regexp.MustCompile(`(?m)(?:^|\s)(?:import|export)\b[^;'"]*?from\s*['"]([^'"]+)['"]|\brequire\(\s*['"]([^'"]+)['"]\s*\)|\bimport\(\s*['"]([^'"]+)['"]\s*\)|^import\s*['"]([^'"]+)['"]`)

var jsExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}

func (p *Project) jsImports(rel, src string) []Import {
	var out []Import
	for _, m := range jsImport.FindAllStringSubmatchIndex(src, -1) {
		for _, g := range []int{1, 2, 3, 4} {
			if m[2*g] >= 0 {
				raw := src[m[2*g]:m[2*g+1]]
				out = append(out, Import{Raw: raw, Resolved: p.resolveJS(rel, raw), Line: lineAt(src, m[2*g])})
			}
		}
	}
	return out
}

func (p *Project) resolveJS(rel, raw string) string {
	if !strings.HasPrefix(raw, "./") && !strings.HasPrefix(raw, "../") {
		return "" // bare specifier: external package (alias support is planned)
	}
	base := path.Join(path.Dir(rel), raw)
	candidates := []string{base}
	for _, ext := range jsExts {
		candidates = append(candidates, base+ext, path.Join(base, "index"+ext))
	}
	for _, c := range candidates {
		if p.files[c] {
			return c
		}
	}
	return ""
}

// --- Python ---

var pyImport = regexp.MustCompile(`(?m)^\s*(?:from\s+([.\w]+)\s+import|import\s+([.\w]+))`)

func (p *Project) pyImports(src string) []Import {
	var out []Import
	for _, m := range pyImport.FindAllStringSubmatchIndex(src, -1) {
		for _, g := range []int{1, 2} {
			if m[2*g] >= 0 {
				raw := src[m[2*g]:m[2*g+1]]
				out = append(out, Import{Raw: raw, Resolved: p.resolvePy(raw), Line: lineAt(src, m[2*g])})
			}
		}
	}
	return out
}

func (p *Project) resolvePy(raw string) string {
	if strings.HasPrefix(raw, ".") {
		return "" // relative imports need package context; planned
	}
	base := strings.ReplaceAll(raw, ".", "/")
	for _, c := range []string{base + ".py", base + "/__init__.py"} {
		if p.files[c] {
			return c
		}
	}
	return ""
}

func lineAt(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}
