package rules

import (
	"fmt"
	"path"
	"strings"

	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/scan"
)

// RuleInfo is one rule applicable to a file, phrased for humans and agents.
type RuleInfo struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason,omitempty"`
}

// Explanation lists every rule that applies to a file, so an agent can
// query the constraints before writing code (lintel rules <path>).
type Explanation struct {
	File             string     `json:"file"`
	Layer            string     `json:"layer,omitempty"`
	LayerDescription string     `json:"layer_description,omitempty"`
	Strict           bool       `json:"strict,omitempty"`
	Dependencies     []RuleInfo `json:"dependencies,omitempty"`
	Metrics          []RuleInfo `json:"metrics,omitempty"`
	Naming           []RuleInfo `json:"naming,omitempty"`
	Bans             []RuleInfo `json:"bans,omitempty"`
	Suppressions     *RuleInfo  `json:"suppressions,omitempty"`
	Placeholders     *RuleInfo  `json:"placeholders,omitempty"`
	Pairing          []RuleInfo `json:"pairing,omitempty"`
	Cycles           *RuleInfo  `json:"cycles,omitempty"`
	Encapsulation    []RuleInfo `json:"encapsulation,omitempty"`
}

// Explain collects the rules that would apply to the given file path.
func Explain(cfg *config.Config, rel string) *Explanation {
	e := &Explanation{File: rel, Layer: scan.LayerOf(rel, cfg), Strict: cfg.Strict}
	if e.Layer != "" {
		e.LayerDescription = cfg.Layers[e.Layer].Description
		for i := range cfg.Rules {
			r := &cfg.Rules[i]
			if r.From == e.Layer || r.From == "*" {
				kind := "allow"
				if r.Kind == config.KindDeny {
					kind = "deny"
				}
				e.Dependencies = append(e.Dependencies, RuleInfo{
					Rule: fmt.Sprintf("%s: %s", kind, r.Expr()), Reason: r.Reason,
				})
			}
		}
	}
	for _, m := range cfg.Metrics {
		if !scan.Match(m.Target, rel) {
			continue
		}
		var limits []string
		for _, l := range []struct {
			v    int
			name string
		}{
			{m.MaxLines, "max-lines"}, {m.MaxImports, "max-imports"},
			{m.MaxFunctionLines, "max-function-lines"}, {m.MaxParams, "max-params"},
			{m.MaxNestingDepth, "max-nesting-depth"}, {m.MaxPublicMethods, "max-public-methods"},
			{m.MaxUseState, "max-use-state"}, {m.MaxUseEffect, "max-use-effect"},
		} {
			if l.v > 0 {
				limits = append(limits, fmt.Sprintf("%s: %d", l.name, l.v))
			}
		}
		e.Metrics = append(e.Metrics, RuleInfo{Rule: strings.Join(limits, ", "), Reason: m.Reason})
	}
	for _, n := range cfg.Naming {
		if !scan.Match(n.Target, rel) {
			continue
		}
		var parts []string
		if n.FilePattern != "" {
			parts = append(parts, fmt.Sprintf("file-pattern: %s", n.FilePattern))
		}
		if n.SymbolPattern != "" {
			parts = append(parts, fmt.Sprintf("symbol-pattern: %s", n.SymbolPattern))
		}
		e.Naming = append(e.Naming, RuleInfo{Rule: strings.Join(parts, ", "), Reason: n.Reason})
	}
	for _, b := range cfg.Bans {
		if !scan.Match(b.Target, rel) || scan.Match(b.Except, rel) {
			continue
		}
		var parts []string
		if len(b.Imports) > 0 {
			parts = append(parts, "imports: "+strings.Join(b.Imports, ", "))
		}
		if len(b.Calls) > 0 {
			parts = append(parts, "calls: "+strings.Join(b.Calls, ", "))
		}
		e.Bans = append(e.Bans, RuleInfo{Rule: strings.Join(parts, "; "), Reason: b.Reason})
	}
	if s := cfg.Suppressions; s != nil && !scan.Match(s.Except, rel) {
		e.Suppressions = &RuleInfo{Rule: "deny: " + strings.Join(s.Deny, ", "), Reason: s.Reason}
	}
	if p := cfg.Placeholders; p != nil && !scan.Match(p.Except, rel) {
		e.Placeholders = &RuleInfo{Rule: "deny: " + strings.Join(p.Deny, ", "), Reason: p.Reason}
	}
	for _, p := range cfg.Pairing {
		if !scan.Match(p.Target, rel) {
			continue
		}
		base := path.Base(rel)
		name := strings.TrimSuffix(base, path.Ext(base))
		e.Pairing = append(e.Pairing, RuleInfo{
			Rule:   "requires: " + strings.ReplaceAll(p.Requires, "{name}", name),
			Reason: p.Reason,
		})
	}
	if c := cfg.Cycles; c != nil && c.Deny && !scan.Match(c.Except, rel) {
		e.Cycles = &RuleInfo{Rule: "deny circular dependencies", Reason: c.Reason}
	}
	for _, enc := range cfg.Encapsulation {
		// Relevant both inside the layer (your internals are private) and
		// outside it (import only via the entry files).
		e.Encapsulation = append(e.Encapsulation, RuleInfo{
			Rule:   fmt.Sprintf("layer %s only via: %s", enc.Layer, strings.Join(enc.Entry, ", ")),
			Reason: enc.Reason,
		})
	}
	return e
}
