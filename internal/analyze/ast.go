package analyze

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/yasomaru/lintel/internal/sitter"
)

// astLang binds a file extension to a bundled grammar and its fact-
// extraction query. Captures are routed by name:
//
//	@import       an import specifier node (quotes stripped for strings)
//	@import.dyn   a dynamic import argument
//	@import.decl  a whole Java import declaration
//	@require.fn / @require.arg   a call that is an import iff fn == "require"
//	@symbol       an exported/public symbol name (language filters apply)
//	@mods         Java modifiers paired with @symbol in the same match
type astLang struct {
	grammar string
	query   string
}

const jsImportPatterns = `
(import_statement source: (string) @import)
(export_statement source: (string) @import)
(call_expression function: (identifier) @require.fn arguments: (arguments . (string) @require.arg))
(call_expression function: (import) arguments: (arguments . (string) @import.dyn))
`

const tsQuery = jsImportPatterns + `
(export_statement (function_declaration name: (_) @symbol))
(export_statement (generator_function_declaration name: (_) @symbol))
(export_statement (class_declaration name: (_) @symbol))
(export_statement (abstract_class_declaration name: (_) @symbol))
(export_statement (interface_declaration name: (_) @symbol))
(export_statement (type_alias_declaration name: (_) @symbol))
(export_statement (enum_declaration name: (_) @symbol))
(export_statement (lexical_declaration (variable_declarator name: (_) @symbol)))
(export_statement (variable_declaration (variable_declarator name: (_) @symbol)))
`

const jsQuery = jsImportPatterns + `
(export_statement (function_declaration name: (_) @symbol))
(export_statement (generator_function_declaration name: (_) @symbol))
(export_statement (class_declaration name: (_) @symbol))
(export_statement (lexical_declaration (variable_declarator name: (_) @symbol)))
(export_statement (variable_declaration (variable_declarator name: (_) @symbol)))
`

// Spec nodes are matched without a parent constraint so that both the
// single form (var X = 1) and the parenthesized block form are covered.
const goQuery = `
(import_spec path: (_) @import)
(function_declaration name: (_) @symbol)
(type_spec name: (_) @symbol)
(var_spec name: (_) @symbol)
(const_spec name: (_) @symbol)
`

const pyQuery = `
(import_statement name: (dotted_name) @import)
(import_statement name: (aliased_import name: (dotted_name) @import))
(import_from_statement module_name: (dotted_name) @import)
(import_from_statement module_name: (relative_import) @import)
(module (function_definition name: (_) @symbol))
(module (class_definition name: (_) @symbol))
(module (decorated_definition definition: (function_definition name: (_) @symbol)))
(module (decorated_definition definition: (class_definition name: (_) @symbol)))
`

const javaQuery = `
(import_declaration) @import.decl
(class_declaration (modifiers) @mods name: (_) @symbol)
(interface_declaration (modifiers) @mods name: (_) @symbol)
(enum_declaration (modifiers) @mods name: (_) @symbol)
(record_declaration (modifiers) @mods name: (_) @symbol)
(annotation_type_declaration (modifiers) @mods name: (_) @symbol)
`

var astLangs = map[string]astLang{
	".ts":   {grammar: "typescript", query: tsQuery},
	".tsx":  {grammar: "tsx", query: tsQuery},
	".js":   {grammar: "javascript", query: jsQuery},
	".jsx":  {grammar: "javascript", query: jsQuery},
	".mjs":  {grammar: "javascript", query: jsQuery},
	".cjs":  {grammar: "javascript", query: jsQuery},
	".go":   {grammar: "go", query: goQuery},
	".py":   {grammar: "python", query: pyQuery},
	".java": {grammar: "java", query: javaQuery},
}

// rawImport is an import specifier before resolution. Offset is the byte
// offset of the captured node; it becomes a line number during resolution.
type rawImport struct {
	Raw    string
	Offset int
}

// engine wraps one wasm tree-sitter instance. Instances are not safe for
// concurrent use; the package-level pool hands each caller its own.
type engine struct {
	ctx      context.Context
	ts       sitter.TreeSitter
	grammars map[string]*grammarState
}

type grammarState struct {
	parser   sitter.Parser
	query    sitter.Query
	capNames map[uint32]string
}

var enginePool = sync.Pool{}

// getEngine returns a pooled engine, or nil when the AST backend is
// disabled or failed to initialize (callers fall back to regex).
func getEngine(disabled bool) *engine {
	if disabled {
		return nil
	}
	if v := enginePool.Get(); v != nil {
		return v.(*engine)
	}
	ctx := context.Background()
	ts, err := sitter.New(ctx)
	if err != nil {
		return nil
	}
	return &engine{ctx: ctx, ts: ts, grammars: map[string]*grammarState{}}
}

func putEngine(e *engine) {
	if e != nil {
		enginePool.Put(e)
	}
}

func (e *engine) forGrammar(al astLang) (*grammarState, error) {
	if g, ok := e.grammars[al.grammar]; ok {
		return g, nil
	}
	lang, err := e.ts.Language(e.ctx, al.grammar)
	if err != nil {
		return nil, err
	}
	parser, err := e.ts.NewParser(e.ctx)
	if err != nil {
		return nil, err
	}
	if err := parser.SetLanguage(e.ctx, lang); err != nil {
		return nil, err
	}
	query, err := e.ts.NewQuery(e.ctx, al.query, lang)
	if err != nil {
		return nil, fmt.Errorf("query for %s: %w", al.grammar, err)
	}
	g := &grammarState{parser: parser, query: query, capNames: map[uint32]string{}}
	e.grammars[al.grammar] = g
	return g, nil
}

func (g *grammarState) captureName(ctx context.Context, id uint32) (string, error) {
	if n, ok := g.capNames[id]; ok {
		return n, nil
	}
	n, err := g.query.CaptureNameForID(ctx, id)
	if err != nil {
		return "", err
	}
	g.capNames[id] = n
	return n, nil
}

type capture struct {
	text  string
	start int
}

// extract parses src and returns raw imports and exported symbols.
func (e *engine) extract(al astLang, ext, src string) ([]rawImport, []Symbol, error) {
	ctx := e.ctx
	g, err := e.forGrammar(al)
	if err != nil {
		return nil, nil, err
	}
	tree, err := g.parser.ParseString(ctx, src)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close(ctx)
	root, err := tree.RootNode(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer root.Free(ctx)
	qc, err := e.ts.NewQueryCursor(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer qc.Close(ctx)
	if err := qc.Exec(ctx, g.query, root); err != nil {
		return nil, nil, err
	}

	var imports []rawImport
	var symbols []Symbol
	for {
		m, ok, err := qc.NextMatch(ctx)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			break
		}
		caps := map[string]capture{}
		for _, c := range m.Captures {
			name, err := g.captureName(ctx, c.ID)
			if err != nil {
				continue
			}
			start, err1 := c.Node.StartByte(ctx)
			end, err2 := c.Node.EndByte(ctx)
			if err1 != nil || err2 != nil || start > end || int(end) > len(src) {
				continue
			}
			caps[name] = capture{text: src[start:end], start: int(start)}
		}
		m.Free(ctx)
		routeMatch(caps, ext, &imports, &symbols)
	}
	// Symbols carried byte offsets so far; convert them to line numbers.
	for i := range symbols {
		symbols[i].Line = lineAt(src, symbols[i].Line)
	}
	return imports, symbols, nil
}

// routeMatch turns one query match into imports or symbols.
func routeMatch(caps map[string]capture, ext string, imports *[]rawImport, symbols *[]Symbol) {
	if fn, ok := caps["require.fn"]; ok {
		if fn.text == "require" {
			if arg, ok := caps["require.arg"]; ok {
				*imports = append(*imports, rawImport{Raw: stripQuotes(arg.text), Offset: arg.start})
			}
		}
		return
	}
	if v, ok := caps["import.dyn"]; ok {
		*imports = append(*imports, rawImport{Raw: stripQuotes(v.text), Offset: v.start})
		return
	}
	if v, ok := caps["import.decl"]; ok {
		if raw := parseJavaImport(v.text); raw != "" {
			*imports = append(*imports, rawImport{Raw: raw, Offset: v.start})
		}
		return
	}
	if v, ok := caps["import"]; ok {
		*imports = append(*imports, rawImport{Raw: stripQuotes(v.text), Offset: v.start})
		return
	}
	if v, ok := caps["symbol"]; ok {
		name := v.text
		switch ext {
		case ".go":
			r, _ := utf8.DecodeRuneInString(name)
			if !unicode.IsUpper(r) {
				return
			}
		case ".py":
			if strings.HasPrefix(name, "_") {
				return
			}
		case ".java":
			mods, ok := caps["mods"]
			if !ok || !strings.Contains(mods.text, "public") {
				return
			}
		}
		// Line temporarily holds the byte offset; extract converts it.
		*symbols = append(*symbols, Symbol{Name: name, Line: v.start})
	}
}

// stripQuotes removes matching string quotes around a specifier.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if q := s[0]; (q == '"' || q == '\'' || q == '`') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// parseJavaImport reduces `import static a.b.C;` to `a.b.C`.
func parseJavaImport(decl string) string {
	t := strings.TrimSpace(decl)
	t = strings.TrimSuffix(t, ";")
	t = strings.TrimSpace(strings.TrimPrefix(t, "import"))
	t = strings.TrimSpace(strings.TrimPrefix(t, "static"))
	return strings.TrimSpace(t)
}

// resolveRaws maps raw specifiers to project files using the per-language
// resolvers shared with the regex backend.
func (p *Project) resolveRaws(rel, ext string, raws []rawImport, src string) []Import {
	out := make([]Import, 0, len(raws))
	for _, r := range raws {
		var resolved string
		switch ext {
		case ".go":
			resolved = p.resolveGo(r.Raw)
		case ".py":
			resolved = p.resolvePy(rel, r.Raw)
		case ".java":
			resolved = p.resolveJava(r.Raw)
		default:
			resolved = p.resolveJS(rel, r.Raw)
		}
		out = append(out, Import{Raw: r.Raw, Resolved: resolved, Line: lineAt(src, r.Offset)})
	}
	return out
}
