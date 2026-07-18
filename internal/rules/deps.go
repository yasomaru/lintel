// Dependency gate: reads direct dependencies from package manifests and
// checks them against the allow/deny policy in arch.yaml.
package rules

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/yasomaru/lintel/internal/config"
)

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
					Rule:     fmt.Sprintf("dependencies: deny %s", pat),
					Detail:   fmt.Sprintf("dependency %q is banned", dep.Name),
					Reason:   d.Reason,
					Severity: severityOf(d.Severity),
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
				Rule:     "dependencies: not in allowlist",
				Detail:   fmt.Sprintf("dependency %q is not in the allowlist", dep.Name),
				Reason:   d.Reason,
				Severity: severityOf(d.Severity),
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
