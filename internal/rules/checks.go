package rules

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
					File:   f.Path,
					Rule:   fmt.Sprintf("naming: file-pattern %s", n.FilePattern),
					Detail: fmt.Sprintf("file name %q does not match %q", base, n.FilePattern),
					Reason: n.Reason,
				})
			}
		}
		if n.SymbolPattern != "" {
			for _, s := range res.Exports {
				if ok, _ := doublestar.Match(n.SymbolPattern, s.Name); !ok {
					out = append(out, Violation{
						File: f.Path, Line: s.Line,
						Rule:   fmt.Sprintf("naming: symbol-pattern %s", n.SymbolPattern),
						Detail: fmt.Sprintf("exported symbol %q does not match %q", s.Name, n.SymbolPattern),
						Reason: n.Reason,
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
						Rule:   fmt.Sprintf("bans: import %s", pat),
						Detail: fmt.Sprintf("import %q is banned here", imp.Raw),
						Reason: b.Reason,
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
					Rule:   fmt.Sprintf("bans: call %s", hit.Pattern),
					Detail: fmt.Sprintf("%q is banned here", hit.Pattern),
					Reason: b.Reason,
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
				Rule:   fmt.Sprintf("%s: %s", kind, hit.Pattern),
				Detail: fmt.Sprintf("%q found", hit.Pattern),
				Reason: pr.Reason,
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
		File:   f.Path,
		Rule:   "coverage: require-layer",
		Detail: fmt.Sprintf("%s does not belong to any layer (known: %s)", f.Path, strings.Join(cfg.LayerNames(), ", ")),
		Reason: cov.Reason,
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
				File:   f.Path,
				Rule:   fmt.Sprintf("pairing: %s", p.Requires),
				Detail: fmt.Sprintf("no file matches %q", want),
				Reason: p.Reason,
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

// --- dependency gate ---

type manifestDep struct {
	Name string
	File string
	Line int
}

func checkDeps(cfg *config.Config, root string) []Violation {
	d := cfg.Dependencies
	if d == nil {
		return nil
	}
	var out []Violation
	for _, dep := range collectDeps(root) {
		denied := false
		for _, pat := range d.Deny {
			if ok, _ := doublestar.Match(pat, dep.Name); ok {
				out = append(out, Violation{
					File: dep.File, Line: dep.Line,
					Rule:   fmt.Sprintf("dependencies: deny %s", pat),
					Detail: fmt.Sprintf("dependency %q is banned", dep.Name),
					Reason: d.Reason,
				})
				denied = true
				break
			}
		}
		if denied || d.Policy != "allowlist" {
			continue
		}
		allowed := false
		for _, pat := range d.Allow {
			if ok, _ := doublestar.Match(pat, dep.Name); ok {
				allowed = true
				break
			}
		}
		if !allowed {
			out = append(out, Violation{
				File: dep.File, Line: dep.Line,
				Rule:   "dependencies: not in allowlist",
				Detail: fmt.Sprintf("dependency %q is not in the allowlist", dep.Name),
				Reason: d.Reason,
			})
		}
	}
	return out
}

// collectDeps reads direct dependencies from the manifests lintel understands.
func collectDeps(root string) []manifestDep {
	var out []manifestDep
	out = append(out, packageJSONDeps(filepath.Join(root, "package.json"))...)
	out = append(out, goModDeps(filepath.Join(root, "go.mod"))...)
	out = append(out, requirementsDeps(filepath.Join(root, "requirements.txt"))...)
	out = append(out, pomDeps(filepath.Join(root, "pom.xml"))...)
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		out = append(out, gradleDeps(filepath.Join(root, name), name)...)
	}
	return out
}

func packageJSONDeps(path string) []manifestDep {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	var out []manifestDep
	for name := range pkg.Dependencies {
		out = append(out, manifestDep{Name: name, File: "package.json"})
	}
	for name := range pkg.DevDependencies {
		out = append(out, manifestDep{Name: name, File: "package.json"})
	}
	return out
}

func goModDeps(path string) []manifestDep {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []manifestDep
	inBlock := false
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		t := strings.TrimSpace(sc.Text())
		switch {
		case t == "require (":
			inBlock = true
		case inBlock && t == ")":
			inBlock = false
		case strings.HasSuffix(t, "// indirect"):
			// AI agents add direct deps; indirect ones follow automatically.
		case inBlock:
			if fields := strings.Fields(t); len(fields) >= 2 {
				out = append(out, manifestDep{Name: fields[0], File: "go.mod", Line: line})
			}
		case strings.HasPrefix(t, "require "):
			if fields := strings.Fields(t); len(fields) >= 3 {
				out = append(out, manifestDep{Name: fields[1], File: "go.mod", Line: line})
			}
		}
	}
	return out
}

// pomDeps reads Maven dependencies as "groupId:artifactId".
func pomDeps(path string) []manifestDep {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pom struct {
		Dependencies struct {
			Dependency []struct {
				GroupID    string `xml:"groupId"`
				ArtifactID string `xml:"artifactId"`
			} `xml:"dependency"`
		} `xml:"dependencies"`
	}
	if err := xml.Unmarshal(data, &pom); err != nil {
		return nil
	}
	var out []manifestDep
	for _, d := range pom.Dependencies.Dependency {
		if d.GroupID != "" && d.ArtifactID != "" {
			out = append(out, manifestDep{Name: d.GroupID + ":" + d.ArtifactID, File: "pom.xml"})
		}
	}
	return out
}

// gradleCoord matches quoted "group:artifact:version" coordinates.
var gradleCoord = regexp.MustCompile(`["']([\w.\-]+):([\w.\-]+):[^"']+["']`)

// gradleDeps reads Gradle dependency coordinates as "group:artifact".
func gradleDeps(path, display string) []manifestDep {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []manifestDep
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		for _, m := range gradleCoord.FindAllStringSubmatch(sc.Text(), -1) {
			out = append(out, manifestDep{Name: m[1] + ":" + m[2], File: display, Line: line})
		}
	}
	return out
}

func requirementsDeps(path string) []manifestDep {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []manifestDep
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "-") {
			continue
		}
		name := t
		if i := strings.IndexAny(t, "=<>!~[; "); i >= 0 {
			name = t[:i]
		}
		if name != "" {
			out = append(out, manifestDep{Name: name, File: "requirements.txt", Line: line})
		}
	}
	return out
}
