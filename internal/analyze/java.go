package analyze

import (
	"regexp"
	"strings"
)

// javaImport matches "import a.b.C;", "import static a.b.C.method;",
// and wildcard "import a.b.*;".
var javaImport = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([\w.]+(?:\.\*)?)\s*;`)

func (p *Project) javaImports(src string) []Import {
	var out []Import
	for _, m := range javaImport.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		out = append(out, Import{Raw: raw, Resolved: p.resolveJava(raw), Line: lineAt(src, m[2])})
	}
	return out
}

// resolveJava maps a fully-qualified import to a project file by matching
// the package path against file path suffixes, so it works with any source
// root (src/main/java, src/, or none). Indexes are sorted, so the first
// match is deterministic.
func (p *Project) resolveJava(raw string) string {
	if pkg, ok := strings.CutSuffix(raw, ".*"); ok {
		// Wildcard: any file directly inside the package directory.
		dir := strings.ReplaceAll(pkg, ".", "/")
		for _, d := range p.javaDirs {
			if pathEndsWith(d, dir) {
				return p.javaDirFile[d]
			}
		}
		return ""
	}
	// "a.b.C" or "a.b.C.member" (static import / nested class): try the
	// longest prefix that maps to a file, dropping trailing segments.
	// Candidate files are pre-indexed by class (base) name.
	segs := strings.Split(raw, ".")
	for k := len(segs); k >= 1; k-- {
		want := strings.Join(segs[:k], "/") + ".java"
		for _, f := range p.javaByBase[segs[k-1]] {
			if pathEndsWith(f, want) {
				return f
			}
		}
	}
	return ""
}

// pathEndsWith reports whether p equals suffix or ends with "/"+suffix.
func pathEndsWith(p, suffix string) bool {
	return p == suffix || strings.HasSuffix(p, "/"+suffix)
}
