package rules

import (
	"sort"

	"github.com/yasomaru/lintel/internal/analyze"
	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/scan"
)

// Edge is an aggregated dependency between two layers.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
	// Denied marks edges the rules forbid (or, in strict mode, don't allow).
	Denied bool `json:"denied"`
}

// LayerEdges aggregates cross-layer imports into per-layer-pair edges.
func LayerEdges(cfg *config.Config, files []scan.File, results map[string]*analyze.Result) []Edge {
	layerOf := make(map[string]string, len(files))
	for _, f := range files {
		layerOf[f.Path] = f.Layer
	}
	byPair := map[[2]string]*Edge{}
	for _, f := range files {
		res := results[f.Path]
		if res == nil || f.Layer == "" {
			continue
		}
		for _, imp := range res.Imports {
			to := layerOf[imp.Resolved]
			if imp.Resolved == "" || to == "" || to == f.Layer {
				continue
			}
			key := [2]string{f.Layer, to}
			e := byPair[key]
			if e == nil {
				verdict, _ := judge(cfg, f.Layer, to)
				e = &Edge{
					From: f.Layer, To: to,
					Denied: verdict == verdictDenied || (cfg.Strict && verdict == verdictUndeclared),
				}
				byPair[key] = e
			}
			e.Count++
		}
	}
	out := make([]Edge, 0, len(byPair))
	for _, e := range byPair {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}
