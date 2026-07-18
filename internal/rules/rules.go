// Package rules evaluates dependency and metric rules over analyzed files.
package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yasomaru/lintel/internal/analyze"
	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/scan"
)

// Violation is a single rule violation, structured so that both humans
// and AI agents can act on it.
type Violation struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
	Reason string `json:"reason,omitempty"`
	// Severity is "error" (fails the check) or "warn" (reported only).
	Severity string `json:"severity"`
	// key overrides the baseline fingerprint. Checks whose Detail contains
	// unstable values (current line counts etc.) set it so that baselined
	// violations don't resurface on unrelated edits.
	key string
}

// severityOf normalizes a rule's severity field; empty means error.
func severityOf(s string) string {
	if s == "warn" {
		return "warn"
	}
	return "error"
}

// CountErrors returns how many violations are error-severity.
func CountErrors(vs []Violation) int {
	n := 0
	for _, v := range vs {
		if v.Severity != "warn" {
			n++
		}
	}
	return n
}

// Fingerprint identifies a violation for baseline matching. It excludes
// the line number so violations don't churn on unrelated edits.
func (v Violation) Fingerprint() string {
	if v.key != "" {
		return v.key
	}
	return v.File + "|" + v.Rule + "|" + v.Detail
}

// Check runs all rules and returns violations sorted by file.
func Check(cfg *config.Config, root string, files []scan.File, results map[string]*analyze.Result) []Violation {
	var out []Violation
	layerOf := make(map[string]string, len(files))
	for _, f := range files {
		layerOf[f.Path] = f.Layer
	}
	for _, f := range files {
		out = append(out, checkCoverage(cfg, f)...)
		res := results[f.Path]
		if res == nil {
			continue
		}
		out = append(out, checkLayerDeps(cfg, f, res, layerOf)...)
		out = append(out, checkEncapsulation(cfg, f, res, layerOf)...)
		out = append(out, checkMetrics(cfg, f, res)...)
		out = append(out, checkNaming(cfg, f, res)...)
		out = append(out, checkBans(cfg, f, res)...)
		out = append(out, checkPatterns("suppressions", cfg.Suppressions, f, res)...)
		out = append(out, checkPatterns("placeholders", cfg.Placeholders, f, res)...)
	}
	out = append(out, checkPairing(cfg, files)...)
	out = append(out, checkDeps(cfg, root)...)
	out = append(out, checkCycles(cfg, files, results)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func checkLayerDeps(cfg *config.Config, f scan.File, res *analyze.Result, layerOf map[string]string) []Violation {
	if f.Layer == "" {
		return nil
	}
	var out []Violation
	for _, imp := range res.Imports {
		if imp.Resolved == "" {
			continue // external or unresolved import
		}
		toLayer := layerOf[imp.Resolved]
		if toLayer == "" || toLayer == f.Layer {
			continue
		}
		verdict, rule := judge(cfg, f.Layer, toLayer)
		switch verdict {
		case verdictDenied:
			out = append(out, Violation{
				File: f.Path, Line: imp.Line,
				Rule:     fmt.Sprintf("deny: %s", rule.Expr()),
				Detail:   fmt.Sprintf("%s (%s) imports %s (%s)", f.Path, f.Layer, imp.Resolved, toLayer),
				Reason:   rule.Reason,
				Severity: severityOf(rule.Severity),
			})
		case verdictUndeclared:
			if cfg.Strict {
				out = append(out, Violation{
					File: f.Path, Line: imp.Line,
					Rule:     "strict: undeclared dependency",
					Detail:   fmt.Sprintf("%s -> %s is not covered by any allow rule", f.Layer, toLayer),
					Severity: "error",
				})
			}
		}
	}
	return out
}

type verdict int

const (
	verdictAllowed verdict = iota
	verdictDenied
	verdictUndeclared
)

// judge decides the fate of an edge fromLayer -> toLayer.
// Deny rules win over allow rules; "*" matches any layer.
func judge(cfg *config.Config, from, to string) (verdict, *config.Rule) {
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if r.Kind == config.KindDeny && matches(r, from, to) {
			return verdictDenied, r
		}
	}
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if r.Kind == config.KindAllow && matches(r, from, to) {
			return verdictAllowed, r
		}
	}
	return verdictUndeclared, nil
}

func matches(r *config.Rule, from, to string) bool {
	return (r.From == "*" || r.From == from) && (r.To == "*" || r.To == to)
}

func checkMetrics(cfg *config.Config, f scan.File, res *analyze.Result) []Violation {
	var out []Violation
	for _, m := range cfg.Metrics {
		if !scan.Match(m.Target, f.Path) {
			continue
		}
		out = append(out, fileSizeViolations(m, f, res)...)
		out = append(out, funcViolations(m, f, res)...)
		out = append(out, classViolations(m, f, res)...)
		out = append(out, hookViolations(m, f, res)...)
	}
	return out
}

// fileSizeViolations checks the whole-file limits.
func fileSizeViolations(m config.MetricGroup, f scan.File, res *analyze.Result) []Violation {
	var out []Violation
	if m.MaxLines > 0 && res.Lines > m.MaxLines {
		rule := fmt.Sprintf("max-lines: %d", m.MaxLines)
		out = append(out, Violation{
			File:     f.Path,
			Rule:     rule,
			Detail:   fmt.Sprintf("%d lines (limit %d)", res.Lines, m.MaxLines),
			Reason:   m.Reason,
			Severity: severityOf(m.Severity),
			key:      f.Path + "|" + rule,
		})
	}
	if m.MaxImports > 0 && len(res.Imports) > m.MaxImports {
		rule := fmt.Sprintf("max-imports: %d", m.MaxImports)
		out = append(out, Violation{
			File:     f.Path,
			Rule:     rule,
			Detail:   fmt.Sprintf("%d imports (limit %d)", len(res.Imports), m.MaxImports),
			Reason:   m.Reason,
			Severity: severityOf(m.Severity),
			key:      f.Path + "|" + rule,
		})
	}
	return out
}

// funcViolations checks the per-function structural limits.
func funcViolations(m config.MetricGroup, f scan.File, res *analyze.Result) []Violation {
	if m.MaxFunctionLines == 0 && m.MaxParams == 0 && m.MaxNestingDepth == 0 {
		return nil
	}
	var out []Violation
	for _, fn := range res.Funcs {
		for _, sm := range []struct {
			limit int
			name  string
			got   int
			unit  string
		}{
			{m.MaxFunctionLines, "max-function-lines", fn.Lines, "lines"},
			{m.MaxParams, "max-params", fn.Params, "parameters"},
			{m.MaxNestingDepth, "max-nesting-depth", fn.Depth, "nesting levels"},
		} {
			if sm.limit == 0 || sm.got <= sm.limit {
				continue
			}
			rule := fmt.Sprintf("%s: %d", sm.name, sm.limit)
			out = append(out, Violation{
				File: f.Path, Line: fn.Line,
				Rule:     rule,
				Detail:   fmt.Sprintf("%s: %d %s (limit %d)", fn.Name, sm.got, sm.unit, sm.limit),
				Reason:   m.Reason,
				Severity: severityOf(m.Severity),
				key:      f.Path + "|" + rule + "|" + fn.Name,
			})
		}
	}
	return out
}

// classViolations checks the per-class structural limits.
func classViolations(m config.MetricGroup, f scan.File, res *analyze.Result) []Violation {
	if m.MaxPublicMethods == 0 {
		return nil
	}
	var out []Violation
	for _, c := range res.Classes {
		if c.PublicMethods <= m.MaxPublicMethods {
			continue
		}
		rule := fmt.Sprintf("max-public-methods: %d", m.MaxPublicMethods)
		out = append(out, Violation{
			File: f.Path, Line: c.Line,
			Rule:     rule,
			Detail:   fmt.Sprintf("%s: %d public methods (limit %d)", c.Name, c.PublicMethods, m.MaxPublicMethods),
			Reason:   m.Reason,
			Severity: severityOf(m.Severity),
			key:      f.Path + "|" + rule + "|" + c.Name,
		})
	}
	return out
}

// hookViolations checks the React hook-count limits.
func hookViolations(m config.MetricGroup, f scan.File, res *analyze.Result) []Violation {
	var out []Violation
	for _, hm := range []struct {
		limit   int
		name    string
		pattern string
	}{
		{m.MaxUseState, "max-use-state", useStatePattern},
		{m.MaxUseEffect, "max-use-effect", useEffectPattern},
	} {
		if hm.limit == 0 {
			continue
		}
		n := 0
		for _, hit := range res.Hits {
			if hit.Pattern == hm.pattern {
				n++
			}
		}
		if n > hm.limit {
			rule := fmt.Sprintf("%s: %d", hm.name, hm.limit)
			out = append(out, Violation{
				File:     f.Path,
				Rule:     rule,
				Detail:   fmt.Sprintf("%d %s calls (limit %d)", n, strings.TrimSuffix(hm.pattern, "("), hm.limit),
				Reason:   m.Reason,
				Severity: severityOf(m.Severity),
				key:      f.Path + "|" + rule,
			})
		}
	}
	return out
}
