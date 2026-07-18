package analyze

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yasomaru/lintel/internal/sitter"
)

// FuncInfo describes one function or method for structural metrics.
type FuncInfo struct {
	Name string
	Line int
	// Lines is the declaration's span in lines.
	Lines int
	// Params is the number of declared parameters (self/cls excluded).
	Params int
	// Depth is the maximum block nesting inside the function, where the
	// function body itself is depth 0.
	Depth int
}

// ClassInfo describes one class-like declaration for structural metrics.
type ClassInfo struct {
	Name          string
	Line          int
	PublicMethods int
}

// metricsPatterns are compiled one by one; a pattern that a bundled
// grammar version rejects is dropped instead of disabling the language.
// Captures: @fn (function span), @fn.method (method span), @fn.arrow
// (arrow function span), @fn.name, @fn.params, @fn.recv (Go receiver),
// @class.name, @class.body, @block.
var metricsPatterns = map[string][]string{
	"typescript": tsMetricsPatterns, "tsx": tsMetricsPatterns, "javascript": tsMetricsPatterns,
	"go": {
		`(function_declaration name: (_) @fn.name parameters: (_) @fn.params) @fn`,
		`(method_declaration receiver: (_) @fn.recv name: (_) @fn.name parameters: (_) @fn.params) @fn.method`,
		`(block) @block`,
	},
	"python": {
		`(function_definition name: (_) @fn.name parameters: (_) @fn.params) @fn`,
		`(class_definition name: (_) @class.name body: (_) @class.body)`,
		`(block) @block`,
	},
	"java": {
		`(method_declaration name: (_) @fn.name parameters: (_) @fn.params) @fn.method`,
		`(constructor_declaration name: (_) @fn.name parameters: (_) @fn.params) @fn.method`,
		`(class_declaration name: (_) @class.name body: (_) @class.body)`,
		`(interface_declaration name: (_) @class.name body: (_) @class.body)`,
		`(block) @block`,
	},
}

var tsMetricsPatterns = []string{
	`(function_declaration name: (_) @fn.name parameters: (_) @fn.params) @fn`,
	`(generator_function_declaration name: (_) @fn.name parameters: (_) @fn.params) @fn`,
	`(method_definition name: (_) @fn.name parameters: (_) @fn.params) @fn.method`,
	`(variable_declarator name: (_) @fn.name value: (arrow_function parameters: (_) @fn.params) @fn.arrow)`,
	`(variable_declarator name: (_) @fn.name value: (arrow_function parameter: (_) @fn.params) @fn.arrow)`,
	`(class_declaration name: (_) @class.name body: (_) @class.body)`,
	`(abstract_class_declaration name: (_) @class.name body: (_) @class.body)`,
	`(statement_block) @block`,
}

type span struct{ start, end int }

type fnRaw struct {
	name     capture
	params   capture
	span     span
	isMethod bool
	recv     string
}

type classRaw struct {
	name capture
	body span
}

// structures runs the metrics query and aggregates function/class facts.
func (e *engine) structures(g *grammarState, root sitter.Node, ext, src string) ([]FuncInfo, []ClassInfo, error) {
	if !g.hasMetrics {
		return nil, nil, nil
	}
	qc, err := e.ts.NewQueryCursor(e.ctx)
	if err != nil {
		return nil, nil, err
	}
	defer qc.Close(e.ctx)
	if err := qc.Exec(e.ctx, g.mQuery, root); err != nil {
		return nil, nil, err
	}

	var fns []fnRaw
	var classes []classRaw
	var blocks []span
	seenFn := map[span]bool{}
	for {
		m, ok, err := qc.NextMatch(e.ctx)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			break
		}
		caps := map[string]capture{}
		spans := map[string]span{}
		for _, c := range m.Captures {
			name, err := g.mCaptureName(e.ctx, c.ID)
			if err != nil {
				continue
			}
			start, err1 := c.Node.StartByte(e.ctx)
			end, err2 := c.Node.EndByte(e.ctx)
			if err1 != nil || err2 != nil || start > end || int(end) > len(src) {
				continue
			}
			caps[name] = capture{text: src[start:end], start: int(start)}
			spans[name] = span{int(start), int(end)}
		}
		m.Free(e.ctx)

		switch {
		case has(spans, "block"):
			blocks = append(blocks, spans["block"])
		case has(spans, "class.body"):
			classes = append(classes, classRaw{name: caps["class.name"], body: spans["class.body"]})
		default:
			f := fnRaw{name: caps["fn.name"], params: caps["fn.params"]}
			switch {
			case has(spans, "fn"):
				f.span = spans["fn"]
			case has(spans, "fn.method"):
				f.span, f.isMethod = spans["fn.method"], true
			case has(spans, "fn.arrow"):
				f.span = spans["fn.arrow"]
			default:
				continue
			}
			if seenFn[f.span] {
				continue // e.g. both arrow patterns matched the same node
			}
			seenFn[f.span] = true
			if r, ok := caps["fn.recv"]; ok {
				f.recv = goReceiverType(r.text)
			}
			fns = append(fns, f)
		}
	}
	return buildStructures(ext, src, fns, classes, blocks), buildClasses(ext, src, fns, classes), nil
}

func has(m map[string]span, k string) bool { _, ok := m[k]; return ok }

// goReceiverType extracts the type name from a Go receiver like
// "(s *Server)" or "(s Server)".
func goReceiverType(recv string) string {
	s := strings.TrimSpace(recv)
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	t := fields[len(fields)-1]
	t = strings.TrimPrefix(t, "*")
	// Strip generic type parameters: Server[T] -> Server.
	if i := strings.IndexByte(t, '['); i > 0 {
		t = t[:i]
	}
	return t
}

// buildStructures turns raw captures into FuncInfo with computed metrics.
func buildStructures(ext, src string, fns []fnRaw, classes []classRaw, blocks []span) []FuncInfo {
	out := make([]FuncInfo, 0, len(fns))
	for _, f := range fns {
		seg := src[f.span.start:f.span.end]
		out = append(out, FuncInfo{
			Name:   f.name.text,
			Line:   lineAt(src, f.name.start),
			Lines:  strings.Count(seg, "\n") + 1,
			Params: countParams(ext, f.params.text),
			Depth:  nestingDepth(f.span, blocks),
		})
	}
	return out
}

// buildClasses counts public methods per class (or per Go receiver type).
func buildClasses(ext, src string, fns []fnRaw, classes []classRaw) []ClassInfo {
	if ext == ".go" {
		byRecv := map[string]*ClassInfo{}
		var order []string
		for _, f := range fns {
			if f.recv == "" || !isPublicName(".go", f.name.text) {
				continue
			}
			c, ok := byRecv[f.recv]
			if !ok {
				c = &ClassInfo{Name: f.recv, Line: lineAt(src, f.name.start)}
				byRecv[f.recv] = c
				order = append(order, f.recv)
			}
			c.PublicMethods++
		}
		out := make([]ClassInfo, 0, len(order))
		for _, r := range order {
			out = append(out, *byRecv[r])
		}
		return out
	}
	out := make([]ClassInfo, 0, len(classes))
	for _, c := range classes {
		info := ClassInfo{Name: c.name.text, Line: lineAt(src, c.name.start)}
		for _, f := range fns {
			inBody := f.span.start >= c.body.start && f.span.end <= c.body.end
			if !inBody {
				continue
			}
			if ext == ".py" && f.isMethod {
				continue // Python methods are plain functions inside the body
			}
			if ext != ".py" && !f.isMethod {
				continue
			}
			if isPublicMember(ext, src, f) {
				info.PublicMethods++
			}
		}
		out = append(out, info)
	}
	return out
}

// isPublicMember decides visibility from the declaration prefix and name.
func isPublicMember(ext, src string, f fnRaw) bool {
	name := f.name.text
	prefix := src[f.span.start:f.name.start]
	switch ext {
	case ".java":
		return containsWord(prefix, "public")
	case ".py":
		return !strings.HasPrefix(name, "_")
	default: // TS/JS classes
		if strings.HasPrefix(name, "#") || containsWord(prefix, "private") || containsWord(prefix, "protected") {
			return false
		}
		return true
	}
}

func isPublicName(ext, name string) bool {
	switch ext {
	case ".go":
		r, _ := utf8.DecodeRuneInString(name)
		return unicode.IsUpper(r)
	case ".py":
		return !strings.HasPrefix(name, "_")
	default:
		return true
	}
}

func containsWord(s, word string) bool {
	for i := strings.Index(s, word); i >= 0; {
		before := i == 0 || !isIdentByte(s[i-1])
		after := i+len(word) >= len(s) || !isIdentByte(s[i+len(word)])
		if before && after {
			return true
		}
		j := strings.Index(s[i+1:], word)
		if j < 0 {
			return false
		}
		i += 1 + j
	}
	return false
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// countParams counts top-level commas in a parameter list, tracking
// bracket depth so nested generics, tuples, and defaults don't miscount.
// Python's self/cls receiver is excluded.
func countParams(ext, params string) int {
	s := strings.TrimSpace(params)
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")
	if strings.TrimSpace(s) == "" {
		return 0
	}
	depth, n := 0, 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				n++
			}
		}
	}
	if ext == ".py" {
		first := strings.TrimSpace(strings.SplitN(s, ",", 2)[0])
		if first == "self" || first == "cls" {
			n--
		}
	}
	return n
}

// nestingDepth is the deepest chain of blocks inside the function span,
// with the function's own body block at depth 0.
func nestingDepth(fn span, blocks []span) int {
	var inside []span
	for _, b := range blocks {
		if b.start >= fn.start && b.end <= fn.end {
			inside = append(inside, b)
		}
	}
	max := 0
	for _, b := range inside {
		containers := 0
		for _, o := range inside {
			if o != b && o.start <= b.start && b.end <= o.end {
				containers++
			}
		}
		if containers > max {
			max = containers
		}
	}
	return max
}
