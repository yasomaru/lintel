package analyze

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// aliasRule maps an import prefix pattern to candidate project paths.
// From may contain one trailing "*" (tsconfig-style), e.g. "@/*" -> "src/*".
type aliasRule struct {
	From string
	To   []string
}

// buildAliases merges manual aliases (from arch.yaml, wins) with aliases
// auto-detected from tsconfig.json / jsconfig.json. Longest prefix first.
func buildAliases(root string, manual map[string][]string) []aliasRule {
	var out []aliasRule
	seen := map[string]bool{}
	for from, to := range manual {
		out = append(out, aliasRule{From: from, To: to})
		seen[from] = true
	}
	for _, a := range tsconfigAliases(root) {
		if !seen[a.From] {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i].From) > len(out[j].From) })
	return out
}

// tsconfigAliases reads compilerOptions.baseUrl/paths from tsconfig.json
// (or jsconfig.json) at the root. The file may be JSONC.
func tsconfigAliases(root string) []aliasRule {
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		var cfg struct {
			CompilerOptions struct {
				BaseURL string              `json:"baseUrl"`
				Paths   map[string][]string `json:"paths"`
			} `json:"compilerOptions"`
		}
		if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
			continue
		}
		base := cfg.CompilerOptions.BaseURL
		if base == "" {
			base = "."
		}
		var out []aliasRule
		for from, targets := range cfg.CompilerOptions.Paths {
			to := make([]string, len(targets))
			for i, t := range targets {
				to[i] = path.Join(base, t) // path.Join also cleans "./src/*"
			}
			out = append(out, aliasRule{From: from, To: to})
		}
		return out
	}
	return nil
}

// stripJSONC removes // and /* */ comments and trailing commas, which are
// legal in tsconfig.json but not in encoding/json. Two passes: comments
// first, so a comma followed by a comment and then "}" is still trailing.
func stripJSONC(data []byte) []byte {
	uncommented := make([]byte, 0, len(data))
	inStr, esc := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inStr {
			uncommented = append(uncommented, c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			uncommented = append(uncommented, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			uncommented = append(uncommented, '\n')
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++
		default:
			uncommented = append(uncommented, c)
		}
	}

	out := make([]byte, 0, len(uncommented))
	inStr, esc = false, false
	for i := 0; i < len(uncommented); i++ {
		c := uncommented[i]
		if inStr {
			out = append(out, c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			// Drop the comma if the next non-space token closes an object/array.
			j := i + 1
			for j < len(uncommented) && (uncommented[j] == ' ' || uncommented[j] == '\t' || uncommented[j] == '\n' || uncommented[j] == '\r') {
				j++
			}
			if j < len(uncommented) && (uncommented[j] == '}' || uncommented[j] == ']') {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// resolveAlias resolves a bare import specifier through the alias table.
func (p *Project) resolveAlias(raw string) string {
	for _, a := range p.aliases {
		if prefix, hasStar := strings.CutSuffix(a.From, "*"); hasStar {
			rest, ok := strings.CutPrefix(raw, prefix)
			if !ok {
				continue
			}
			for _, t := range a.To {
				if f := p.tryJSFile(strings.Replace(t, "*", rest, 1)); f != "" {
					return f
				}
			}
		} else if raw == a.From {
			for _, t := range a.To {
				if f := p.tryJSFile(t); f != "" {
					return f
				}
			}
		}
	}
	return ""
}
