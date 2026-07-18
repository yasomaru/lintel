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
	"sort"
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
	// aliases resolve bare specifiers like "@/domain/user" to project paths.
	aliases []aliasRule
	// engineOff disables the AST backend (LINTEL_ENGINE=regex).
	engineOff bool
	// files is the set of known project files (slash-relative paths).
	files map[string]bool
	// goDirs maps a package directory (module-relative) to one file in it.
	goDirs map[string]string
	// javaByBase maps a class/file base name to its paths, sorted.
	javaByBase map[string][]string
	// javaDirs lists directories containing .java files, sorted, with a
	// representative file per directory for wildcard-import resolution.
	javaDirs    []string
	javaDirFile map[string]string
}

// Options tunes project analysis.
type Options struct {
	// Patterns are forbidden substrings to search for in every file.
	Patterns []string
	// Aliases are manual import aliases ("@/*" -> "src/*"). They take
	// precedence over aliases auto-detected from tsconfig.json.
	Aliases map[string][]string
}

// NewProject builds resolution context for the given root and file set.
func NewProject(root string, relPaths []string, opts Options) *Project {
	p := &Project{
		Root: root, Patterns: opts.Patterns,
		files:       make(map[string]bool, len(relPaths)),
		goDirs:      map[string]string{},
		javaByBase:  map[string][]string{},
		javaDirFile: map[string]string{},
	}
	p.aliases = buildAliases(root, opts.Aliases)
	p.engineOff = os.Getenv("LINTEL_ENGINE") == "regex"
	for _, f := range relPaths {
		dir := path.Dir(f)
		switch path.Ext(f) {
		case ".go":
			if cur, ok := p.goDirs[dir]; !ok || f < cur {
				p.goDirs[dir] = f
			}
		case ".java":
			base := strings.TrimSuffix(path.Base(f), ".java")
			p.javaByBase[base] = append(p.javaByBase[base], f)
			if cur, ok := p.javaDirFile[dir]; !ok || f < cur {
				p.javaDirFile[dir] = f
			}
		}
	}
	for _, paths := range p.javaByBase {
		sort.Strings(paths)
	}
	for d := range p.javaDirFile {
		p.javaDirs = append(p.javaDirs, d)
	}
	sort.Strings(p.javaDirs)
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
	eng := getEngine(p.engineOff)
	defer putEngine(eng)
	return p.fileWith(rel, eng)
}

// fileWith analyzes one file using the AST engine when available for the
// file's language, falling back to the regex extractors otherwise.
func (p *Project) fileWith(rel string, eng *engine) (*Result, error) {
	abs := filepath.Join(p.Root, filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	src := string(data)
	res := &Result{Lines: countLines(src)}
	ext := path.Ext(rel)

	if eng != nil {
		if al, ok := astLangs[ext]; ok {
			raws, syms, err := eng.extract(al, ext, src)
			if err == nil {
				res.Imports = p.resolveRaws(rel, ext, raws, src)
				res.Exports = syms
				res.Hits = scanPatterns(src, p.Patterns)
				return res, nil
			}
			// Engine trouble (wasm failure, query mismatch): fall through
			// to the regex path rather than losing the file.
		}
	}

	switch ext {
	case ".go":
		res.Imports = p.goImports(src)
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		res.Imports = p.jsImports(rel, src)
	case ".py":
		res.Imports = p.pyImports(rel, src)
	case ".java":
		res.Imports = p.javaImports(src)
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
	// A Go import points at a package directory; map it to a file inside.
	return p.goDirs[sub]
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
		// Bare specifier: try aliases; otherwise it's an external package.
		return p.resolveAlias(raw)
	}
	return p.tryJSFile(path.Join(path.Dir(rel), raw))
}

// tryJSFile resolves a project-relative base path to an actual file,
// trying the JS/TS extensions and index files.
func (p *Project) tryJSFile(base string) string {
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

func (p *Project) pyImports(rel, src string) []Import {
	var out []Import
	for _, m := range pyImport.FindAllStringSubmatchIndex(src, -1) {
		for _, g := range []int{1, 2} {
			if m[2*g] >= 0 {
				raw := src[m[2*g]:m[2*g+1]]
				out = append(out, Import{Raw: raw, Resolved: p.resolvePy(rel, raw), Line: lineAt(src, m[2*g])})
			}
		}
	}
	return out
}

func (p *Project) resolvePy(rel, raw string) string {
	base := ""
	if strings.HasPrefix(raw, ".") {
		// Relative import: one dot is the file's package, each extra dot
		// walks one package up. "from ..models.user import U" etc.
		dots := 0
		for dots < len(raw) && raw[dots] == '.' {
			dots++
		}
		dir := path.Dir(rel)
		for i := 1; i < dots; i++ {
			dir = path.Dir(dir)
		}
		base = dir
		if rest := raw[dots:]; rest != "" {
			base = path.Join(dir, strings.ReplaceAll(rest, ".", "/"))
		}
	} else {
		base = strings.ReplaceAll(raw, ".", "/")
	}
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
