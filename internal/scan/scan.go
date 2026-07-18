// Package scan walks the project tree and assigns each source file to a layer.
package scan

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/yasomaru/lintel/internal/config"
)

// File is a source file discovered under the root.
type File struct {
	// Path is slash-separated and relative to the root.
	Path string
	// Layer is the assigned layer name, or "" if no layer matched.
	Layer string
}

// Supported source extensions for v0. Kept intentionally small;
// language packs will own this once the tree-sitter backend lands.
var sourceExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".mjs": true, ".cjs": true, ".py": true, ".java": true,
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "__pycache__": true, ".venv": true,
	"venv": true, "coverage": true, "target": true,
}

// Walk returns all supported source files under root with their layer.
func Walk(root string, cfg *config.Config) ([]File, error) {
	rels, err := List(root)
	if err != nil {
		return nil, err
	}
	files := make([]File, len(rels))
	for i, rel := range rels {
		files[i] = File{Path: rel, Layer: LayerOf(rel, cfg)}
	}
	return files, nil
}

// List returns the slash-relative paths of all supported source files
// under root, without layer assignment.
func List(root string) ([]string, error) {
	var rels []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceExts[filepath.Ext(path)] {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	return rels, err
}

// LayerOf returns the layer a path belongs to, or "" if none matches.
// If multiple layers match, the most specific (longest) pattern wins so
// that e.g. "src/domain/**" beats "src/**".
func LayerOf(rel string, cfg *config.Config) string {
	best, bestLen := "", -1
	for name, layer := range cfg.Layers {
		for _, pat := range layer.Path {
			ok, err := doublestar.Match(pat, rel)
			if err != nil || !ok {
				continue
			}
			if len(pat) > bestLen {
				best, bestLen = name, len(pat)
			}
		}
	}
	return best
}

// Match reports whether rel matches any of the given patterns.
func Match(patterns []string, rel string) bool {
	for _, pat := range patterns {
		if ok, err := doublestar.Match(pat, rel); err == nil && ok {
			return true
		}
	}
	return false
}
