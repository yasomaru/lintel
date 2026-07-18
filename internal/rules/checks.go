package rules

import (
	"fmt"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/yasomaru/lintel/internal/analyze"
	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/scan"
)

// TextPatterns collects every forbidden substring the analyzer should
// search for: suppression markers, placeholders, and banned calls.
func TextPatterns(cfg *config.Config) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ps []string) {
		for _, p := range ps {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	if cfg.Suppressions != nil {
		add(cfg.Suppressions.Deny)
	}
	if cfg.Placeholders != nil {
		add(cfg.Placeholders.Deny)
	}
	for _, b := range cfg.Bans {
		add(b.Calls)
	}
	return out
}

func checkNaming(cfg *config.Config, f scan.File, res *analyze.Result) []Violation {
	var out []Violation
	for _, n := range cfg.Naming {
		if !scan.Match(n.Target, f.Path) {
			continue
		}
		if n.FilePattern != "" {
			base := path.Base(f.Path)
			if ok, _ := doublestar.Match(n.FilePattern, base); !ok {
				out = append(out, Violation{
					File:     f.Path,
					Rule:     fmt.Sprintf("naming: file-pattern %s", n.FilePattern),
					Detail:   fmt.Sprintf("file name %q does not match %q", base, n.FilePattern),
					Reason:   n.Reason,
					Severity: severityOf(n.Severity),
				})
			}
		}
		if n.SymbolPattern != "" {
			for _, s := range res.Exports {
				if ok, _ := doublestar.Match(n.SymbolPattern, s.Name); !ok {
					out = append(out, Violation{
						File: f.Path, Line: s.Line,
						Rule:     fmt.Sprintf("naming: symbol-pattern %s", n.SymbolPattern),
						Detail:   fmt.Sprintf("exported symbol %q does not match %q", s.Name, n.SymbolPattern),
						Reason:   n.Reason,
						Severity: severityOf(n.Severity),
					})
				}
			}
		}
	}
	return out
}

func checkBans(cfg *config.Config, f scan.File, res *analyze.Result) []Violation {
	var out []Violation
	for _, b := range cfg.Bans {
		if !scan.Match(b.Target, f.Path) || scan.Match(b.Except, f.Path) {
			continue
		}
		for _, imp := range res.Imports {
			for _, pat := range b.Imports {
				if ok, _ := doublestar.Match(pat, imp.Raw); ok {
					out = append(out, Violation{
						File: f.Path, Line: imp.Line,
						Rule:     fmt.Sprintf("bans: import %s", pat),
						Detail:   fmt.Sprintf("import %q is banned here", imp.Raw),
						Reason:   b.Reason,
						Severity: severityOf(b.Severity),
					})
				}
			}
		}
		banned := map[string]bool{}
		for _, c := range b.Calls {
			banned[c] = true
		}
		for _, hit := range res.Hits {
			if banned[hit.Pattern] {
				out = append(out, Violation{
					File: f.Path, Line: hit.Line,
					Rule:     fmt.Sprintf("bans: call %s", hit.Pattern),
					Detail:   fmt.Sprintf("%q is banned here", hit.Pattern),
					Reason:   b.Reason,
					Severity: severityOf(b.Severity),
				})
			}
		}
	}
	return out
}

// checkPatterns handles both suppressions and placeholders.
func checkPatterns(kind string, pr *config.PatternRule, f scan.File, res *analyze.Result) []Violation {
	if pr == nil || scan.Match(pr.Except, f.Path) {
		return nil
	}
	denied := map[string]bool{}
	for _, d := range pr.Deny {
		denied[d] = true
	}
	var out []Violation
	for _, hit := range res.Hits {
		if denied[hit.Pattern] {
			out = append(out, Violation{
				File: f.Path, Line: hit.Line,
				Rule:     fmt.Sprintf("%s: %s", kind, hit.Pattern),
				Detail:   fmt.Sprintf("%q found", hit.Pattern),
				Reason:   pr.Reason,
				Severity: severityOf(pr.Severity),
			})
		}
	}
	return out
}

func checkCoverage(cfg *config.Config, f scan.File) []Violation {
	cov := cfg.Coverage
	if cov == nil || !cov.RequireLayer || f.Layer != "" || scan.Match(cov.Except, f.Path) {
		return nil
	}
	return []Violation{{
		File:     f.Path,
		Rule:     "coverage: require-layer",
		Detail:   fmt.Sprintf("%s does not belong to any layer (known: %s)", f.Path, strings.Join(cfg.LayerNames(), ", ")),
		Reason:   cov.Reason,
		Severity: severityOf(cov.Severity),
	}}
}

func checkPairing(cfg *config.Config, files []scan.File) []Violation {
	if len(cfg.Pairing) == 0 {
		return nil
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	var out []Violation
	for _, p := range cfg.Pairing {
		for _, f := range files {
			if !scan.Match(p.Target, f.Path) {
				continue
			}
			base := path.Base(f.Path)
			name := strings.TrimSuffix(base, path.Ext(base))
			want := strings.ReplaceAll(p.Requires, "{name}", name)
			if anyMatch(paths, want) {
				continue
			}
			out = append(out, Violation{
				File:     f.Path,
				Rule:     fmt.Sprintf("pairing: %s", p.Requires),
				Detail:   fmt.Sprintf("no file matches %q", want),
				Reason:   p.Reason,
				Severity: severityOf(p.Severity),
			})
		}
	}
	return out
}

func anyMatch(paths []string, pattern string) bool {
	for _, p := range paths {
		if ok, _ := doublestar.Match(pattern, p); ok {
			return true
		}
	}
	return false
}
