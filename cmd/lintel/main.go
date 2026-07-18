// Command lintel checks architecture layer dependencies and size metrics
// against the rules declared in arch.yaml.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yasomaru/lintel/internal/analyze"
	"github.com/yasomaru/lintel/internal/config"
	"github.com/yasomaru/lintel/internal/report"
	"github.com/yasomaru/lintel/internal/rules"
	"github.com/yasomaru/lintel/internal/scan"
)

const usage = `lintel — architecture lint for any language

Usage:
  lintel check [path]      check the project against arch.yaml
  lintel baseline [path]   record current violations as the baseline
  lintel init              write a starter arch.yaml

Flags for check:
  --config <file>   config file (default: arch.yaml under the target path)
  --format <fmt>    output format: text | json (default: text)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "check":
		err = runCheck(os.Args[2:], false)
	case "baseline":
		err = runCheck(os.Args[2:], true)
	case "init":
		err = runInit()
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lintel:", err)
		os.Exit(2)
	}
}

func runCheck(args []string, writeBaseline bool) error {
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

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	files, err := scan.Walk(root, cfg)
	if err != nil {
		return err
	}
	relPaths := make([]string, len(files))
	for i, f := range files {
		relPaths[i] = f.Path
	}
	proj := analyze.NewProject(root, relPaths, rules.TextPatterns(cfg))
	results := make(map[string]*analyze.Result, len(files))
	for _, f := range files {
		res, err := proj.File(f.Path)
		if err != nil {
			continue // unreadable or unsupported files are skipped, not fatal
		}
		results[f.Path] = res
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
		fmt.Printf("baseline written: %s (%d violation(s))\n", baselinePath, len(violations))
		return nil
	}

	var baselined []rules.Violation
	if baselinePath != "" {
		b, err := rules.LoadBaseline(baselinePath)
		if err != nil {
			return err
		}
		violations, baselined = b.Filter(violations)
	}

	sum := report.Summary{
		Violations: violations,
		Baselined:  len(baselined),
		Files:      len(files),
		OK:         len(violations) == 0,
	}
	if *format == "json" {
		if err := report.JSON(os.Stdout, sum); err != nil {
			return err
		}
	} else {
		report.Human(os.Stdout, sum)
	}
	if !sum.OK {
		os.Exit(1)
	}
	return nil
}

const starterConfig = `# arch.yaml — architecture rules checked by lintel
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

func runInit() error {
	if _, err := os.Stat("arch.yaml"); err == nil {
		return fmt.Errorf("arch.yaml already exists")
	}
	if err := os.WriteFile("arch.yaml", []byte(starterConfig), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote arch.yaml — edit the layers to match your project, then run: lintel check")
	return nil
}
