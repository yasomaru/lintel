package analyze

import (
	"path"
	"regexp"
	"strings"
)

// Symbol is an exported (public) top-level declaration.
type Symbol struct {
	Name string
	Line int
}

// PatternHit records a forbidden substring found in the source.
type PatternHit struct {
	Pattern string
	Line    int
}

var (
	// Top-level exported declarations. Var/const blocks and methods are not
	// covered in v0; the tree-sitter backend will close that gap.
	goExport   = regexp.MustCompile(`(?m)^(?:func|type|var|const)\s+([A-Z]\w*)`)
	jsExport   = regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?(?:abstract\s+)?(?:async\s+)?(?:function\*?|class|const|let|var|interface|type|enum)\s+(\w+)`)
	pyTop      = regexp.MustCompile(`(?m)^(?:def|class)\s+([A-Za-z]\w*)`)
	javaPublic = regexp.MustCompile(`(?m)^\s*public\s+(?:final\s+|abstract\s+|sealed\s+|non-sealed\s+|static\s+|strictfp\s+)*(?:class|interface|enum|record|@interface)\s+(\w+)`)
)

// exports extracts exported top-level symbols for the file's language.
func exports(rel, src string) []Symbol {
	var re *regexp.Regexp
	switch path.Ext(rel) {
	case ".go":
		re = goExport
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		re = jsExport
	case ".py":
		re = pyTop
	case ".java":
		re = javaPublic
	default:
		return nil
	}
	var out []Symbol
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, Symbol{Name: src[m[2]:m[3]], Line: lineAt(src, m[2])})
	}
	return out
}

// scanPatterns finds forbidden substrings, one hit per pattern per line.
func scanPatterns(src string, patterns []string) []PatternHit {
	if len(patterns) == 0 {
		return nil
	}
	var out []PatternHit
	for i, line := range strings.Split(src, "\n") {
		for _, pat := range patterns {
			if strings.Contains(line, pat) {
				out = append(out, PatternHit{Pattern: pat, Line: i + 1})
			}
		}
	}
	return out
}
