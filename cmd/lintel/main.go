// Command lintel checks architecture layer dependencies and size metrics
// against the rules declared in arch.yaml.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/yasomaru/lintel/internal/analyze"
	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/report"
	"github.com/yasomaru/lintel/internal/rules"
	"github.com/yasomaru/lintel/internal/scaffold"
	"github.com/yasomaru/lintel/internal/scan"
)

// version is set by goreleaser at release time.
var version = "dev"

// errViolations signals check failure; main maps it to exit code 1.
var errViolations = errors.New("check failed: violations found")

const usage = `lintel — architecture lint for any language

Usage:
  lintel check [path]      check the project against arch.yaml
  lintel baseline [path]   record current violations as the baseline
  lintel graph [path]      print the layer dependency graph (--format mermaid | dot)
  lintel init [--scan]     write a starter arch.yaml (--scan infers layers)
  lintel rules <path>      show the rules that apply to a file, as JSON
  lintel context [path]    print a Markdown rule summary for CLAUDE.md / AGENTS.md
  lintel schema            print the JSON Schema for arch.yaml
  lintel version           print the version

Flags for check:
  --config <file>   config file (default: arch.yaml under the target path)
  --format <fmt>    output format: text | json | github (default: text)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	err := run(os.Stdout, os.Args[1], os.Args[2:])
	if errors.Is(err, errViolations) {
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lintel:", err)
		os.Exit(2)
	}
}

// run dispatches a subcommand, writing user output to w.
func run(w io.Writer, command string, args []string) error {
	switch command {
	case "check":
		return runCheck(w, args, false)
	case "baseline":
		return runCheck(w, args, true)
	case "graph":
		return runGraph(w, args)
	case "init":
		return runInit(w, args)
	case "rules":
		return runRules(w, args)
	case "context":
		return runContext(w, args)
	case "schema":
		_, err := w.Write(config.SchemaJSON)
		return err
	case "version", "--version", "-v":
		_, err := fmt.Fprintln(w, "lintel", version)
		return err
	case "help", "--help", "-h":
		_, err := fmt.Fprint(w, usage)
		return err
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}
}

func runCheck(w io.Writer, args []string, writeBaseline bool) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	format := fs.String("format", "text", "output format: text | json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	if *cfgPath == "" {
		*cfgPath = filepath.Join(root, "arch.yaml")
	}

	cfg, files, results, err := loadAndAnalyze(root, *cfgPath)
	if err != nil {
		return err
	}

	violations := rules.Check(cfg, root, files, results)

	baselinePath := cfg.Baseline
	if baselinePath != "" {
		baselinePath = filepath.Join(root, baselinePath)
	}
	if writeBaseline {
		if baselinePath == "" {
			baselinePath = filepath.Join(root, ".lintel-baseline.json")
			fmt.Fprintf(os.Stderr, "note: no baseline path in config; writing %s (add `baseline:` to arch.yaml)\n", baselinePath)
		}
		if err := rules.WriteBaseline(baselinePath, violations); err != nil {
			return err
		}
		fmt.Fprintf(w, "baseline written: %s (%d violation(s))\n", baselinePath, len(violations))
		return nil
	}

	var baselined []rules.Violation
	stale := 0
	if baselinePath != "" {
		b, err := rules.LoadBaseline(baselinePath)
		if err != nil {
			return err
		}
		violations, baselined, stale = b.Filter(violations)
	}

	sum := report.Summary{
		Violations: violations,
		Baselined:  len(baselined),
		Stale:      stale,
		Files:      len(files),
		// Warn-severity violations are reported but don't fail the check.
		OK: rules.CountErrors(violations) == 0,
	}
	switch *format {
	case "json":
		if err := report.JSON(w, sum); err != nil {
			return err
		}
	case "github":
		report.GitHub(w, sum)
	case "text":
		report.Human(w, sum)
	default:
		return fmt.Errorf("unknown format %q (want text, json, or github)", *format)
	}
	if !sum.OK {
		return errViolations
	}
	return nil
}

// loadAndAnalyze runs the shared pipeline: config, file walk, analysis.
func loadAndAnalyze(root, cfgPath string) (*config.Config, []scan.File, map[string]*analyze.Result, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, nil, err
	}
	files, err := scan.Walk(root, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	relPaths := make([]string, len(files))
	for i, f := range files {
		relPaths[i] = f.Path
	}
	proj := analyze.NewProject(root, relPaths, analyze.Options{
		Patterns: rules.TextPatterns(cfg),
		Aliases:  cfg.AliasMap(),
	})
	return cfg, files, proj.All(relPaths), nil
}

// runGraph prints the aggregated layer dependency graph.
func runGraph(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	format := fs.String("format", "mermaid", "output format: mermaid | dot")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	if *cfgPath == "" {
		*cfgPath = filepath.Join(root, "arch.yaml")
	}
	cfg, files, results, err := loadAndAnalyze(root, *cfgPath)
	if err != nil {
		return err
	}
	edges := rules.LayerEdges(cfg, files, results)
	switch *format {
	case "mermaid":
		report.Mermaid(w, cfg.LayerNames(), edges)
	case "dot":
		report.Dot(w, cfg.LayerNames(), edges)
	default:
		return fmt.Errorf("unknown format %q (want mermaid or dot)", *format)
	}
	return nil
}

// runRules prints every rule applicable to a file, for querying the
// architecture before writing code (the primary consumer is AI agents).
func runRules(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("rules", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path (default: nearest arch.yaml above the file)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: lintel rules [--config arch.yaml] <path>")
	}
	target := fs.Arg(0)
	if *cfgPath == "" {
		found, err := findConfig(filepath.Dir(target))
		if err != nil {
			return err
		}
		*cfgPath = found
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	rel := filepath.ToSlash(filepath.Clean(target))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rules.Explain(cfg, rel))
}

// findConfig walks upward from dir looking for arch.yaml, so `lintel rules
// src/domain/user.ts` works from anywhere inside the project.
func findConfig(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(abs, "arch.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no arch.yaml found in %s or any parent directory", dir)
		}
		abs = parent
	}
}

// runContext prints a Markdown summary of the rules for agent instructions.
func runContext(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("context", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	if *cfgPath == "" {
		*cfgPath = filepath.Join(root, "arch.yaml")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	report.Context(w, cfg)
	return nil
}

const starterConfig = `# yaml-language-server: $schema=https://raw.githubusercontent.com/yasomaru/lintel/main/docs/arch.schema.json
# arch.yaml — architecture rules checked by lintel
# Layers are named groups of files; rules constrain dependencies between them.

layers:
  domain:
    path: "src/domain/**"
    description: Business logic. Must stay free of outward dependencies.
  infra:
    path: "src/infra/**"
    description: Adapters to databases and external services.

rules:
  - deny: domain -> "*"
    reason: The domain layer must not depend on any other layer.
  - allow: infra -> domain

# metrics:
#   - target: "src/**/service/**"
#     max-lines: 300
#     max-imports: 15
#     reason: Large services accumulate mixed responsibilities. Split them.

# baseline: .lintel-baseline.json
`

func runInit(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	scanTree := fs.Bool("scan", false, "infer layers from the existing tree")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat("arch.yaml"); err == nil {
		return fmt.Errorf("arch.yaml already exists")
	}
	content := starterConfig
	if *scanTree {
		generated, err := scaffold.Generate(".")
		if err != nil {
			return err
		}
		content = generated
	}
	if err := os.WriteFile("arch.yaml", []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(w, "wrote arch.yaml — edit the layers to match your project, then run: lintel check")
	return nil
}
