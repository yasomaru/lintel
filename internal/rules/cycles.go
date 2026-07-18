package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yasomaru/lintel/internal/analyze"
	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/scan"
)

// checkCycles finds circular dependencies between project files using
// Tarjan's strongly-connected-components algorithm (iterative).
func checkCycles(cfg *config.Config, files []scan.File, results map[string]*analyze.Result) []Violation {
	c := cfg.Cycles
	if c == nil || !c.Deny {
		return nil
	}
	adj := map[string][]string{}
	for _, f := range files {
		res := results[f.Path]
		if res == nil || scan.Match(c.Except, f.Path) {
			continue
		}
		seen := map[string]bool{}
		for _, imp := range res.Imports {
			to := imp.Resolved
			if to == "" || to == f.Path || seen[to] || scan.Match(c.Except, to) {
				continue
			}
			seen[to] = true
			adj[f.Path] = append(adj[f.Path], to)
		}
	}

	var out []Violation
	for _, scc := range tarjanSCCs(adj) {
		if len(scc) < 2 {
			continue
		}
		sort.Strings(scc)
		out = append(out, Violation{
			File:     scc[0],
			Rule:     "cycles: deny",
			Detail:   fmt.Sprintf("circular dependency between %d files: %s", len(scc), strings.Join(scc, " <-> ")),
			Reason:   c.Reason,
			Severity: severityOf(c.Severity),
		})
	}
	return out
}

// tarjanSCCs returns the strongly connected components of the graph.
func tarjanSCCs(adj map[string][]string) [][]string {
	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes) // deterministic traversal order

	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	next := 0
	var sccs [][]string

	type frame struct {
		node string
		edge int
	}
	for _, start := range nodes {
		if _, ok := index[start]; ok {
			continue
		}
		call := []frame{{node: start}}
		for len(call) > 0 {
			f := &call[len(call)-1]
			n := f.node
			if f.edge == 0 {
				index[n] = next
				low[n] = next
				next++
				stack = append(stack, n)
				onStack[n] = true
			}
			advanced := false
			for f.edge < len(adj[n]) {
				m := adj[n][f.edge]
				f.edge++
				if _, ok := index[m]; !ok {
					call = append(call, frame{node: m})
					advanced = true
					break
				}
				if onStack[m] && index[m] < low[n] {
					low[n] = index[m]
				}
			}
			if advanced {
				continue
			}
			if low[n] == index[n] {
				var scc []string
				scc, stack = popSCC(stack, onStack, n)
				sccs = append(sccs, scc)
			}
			call = call[:len(call)-1]
			if len(call) > 0 {
				parent := call[len(call)-1].node
				if low[n] < low[parent] {
					low[parent] = low[n]
				}
			}
		}
	}
	return sccs
}

// popSCC pops one strongly connected component off the Tarjan stack.
func popSCC(stack []string, onStack map[string]bool, root string) (scc, rest []string) {
	for {
		m := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		onStack[m] = false
		scc = append(scc, m)
		if m == root {
			return scc, stack
		}
	}
}

// checkEncapsulation flags imports that reach into a layer's internals
// instead of going through its declared entry files.
func checkEncapsulation(cfg *config.Config, f scan.File, res *analyze.Result, layerOf map[string]string) []Violation {
	var out []Violation
	for _, e := range cfg.Encapsulation {
		if f.Layer == e.Layer {
			continue // the layer may use its own internals freely
		}
		for _, imp := range res.Imports {
			if imp.Resolved == "" || layerOf[imp.Resolved] != e.Layer {
				continue
			}
			if scan.Match(e.Entry, imp.Resolved) {
				continue
			}
			out = append(out, Violation{
				File: f.Path, Line: imp.Line,
				Rule:     fmt.Sprintf("encapsulation: %s", e.Layer),
				Detail:   fmt.Sprintf("%s reaches into %s internals (%s); import via %s", f.Path, e.Layer, imp.Resolved, strings.Join(e.Entry, " or ")),
				Reason:   e.Reason,
				Severity: severityOf(e.Severity),
			})
		}
	}
	return out
}
