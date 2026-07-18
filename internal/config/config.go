// Package config defines the arch.yaml schema: layers, dependency rules,
// and metric rules. The schema is designed to be readable by both humans
// and AI agents — descriptions and reasons are first-class fields.
package config

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaJSON is the JSON Schema for arch.yaml, embedded so that
// `lintel schema` works offline. docs/arch.schema.json is a published
// copy kept in sync by a test.
//
//go:embed arch.schema.json
var SchemaJSON []byte

// Config is the root of arch.yaml.
type Config struct {
	// Layers maps a layer name to its definition.
	Layers map[string]Layer `yaml:"layers"`
	// Rules are dependency rules, evaluated as: deny wins over allow.
	// When Strict is true, an edge that matches no allow rule is a violation.
	Rules []Rule `yaml:"rules"`
	// Metrics are size/complexity limits applied to matched files.
	Metrics []MetricGroup `yaml:"metrics"`
	// Naming enforces file and exported-symbol naming conventions.
	Naming []NamingRule `yaml:"naming"`
	// Bans forbids specific imports or calls inside matched files.
	Bans []BanRule `yaml:"bans"`
	// Suppressions forbids lint-silencing markers (ts-ignore comments etc.).
	Suppressions *PatternRule `yaml:"suppressions"`
	// Placeholders forbids unfinished-code markers (TODO: implement, ...).
	Placeholders *PatternRule `yaml:"placeholders"`
	// Dependencies gates external packages declared in manifests.
	Dependencies *DepsPolicy `yaml:"dependencies"`
	// Coverage requires every source file to belong to a layer.
	Coverage *Coverage `yaml:"coverage"`
	// Pairing requires companion files (e.g. tests) to exist.
	Pairing []PairRule `yaml:"pairing"`
	// Cycles forbids circular dependencies between files.
	Cycles *CycleRule `yaml:"cycles"`
	// Encapsulation forces access to a layer through its entry files.
	Encapsulation []EncapsulationRule `yaml:"encapsulation"`
	// Resolve tunes import resolution (path aliases etc.).
	Resolve *Resolve `yaml:"resolve"`
	// Baseline is a path to a JSON file holding grandfathered violations.
	Baseline string `yaml:"baseline"`
	// Strict makes undeclared dependencies between layers a violation.
	Strict bool `yaml:"strict"`
}

// NamingRule constrains file names and exported symbol names.
// Patterns are globs; FilePattern matches the base name of the file.
type NamingRule struct {
	Target        StringList `yaml:"target"`
	FilePattern   string     `yaml:"file-pattern"`
	SymbolPattern string     `yaml:"symbol-pattern"`
	Reason        string     `yaml:"reason"`
	Severity      string     `yaml:"severity"`
}

// BanRule forbids import specifiers (glob on the raw specifier) and calls
// (substring match, e.g. "console.log") inside files matching Target.
type BanRule struct {
	Target   StringList `yaml:"target"`
	Imports  []string   `yaml:"imports"`
	Calls    []string   `yaml:"calls"`
	Except   StringList `yaml:"except"`
	Reason   string     `yaml:"reason"`
	Severity string     `yaml:"severity"`
}

// PatternRule forbids substrings anywhere in matched source lines.
type PatternRule struct {
	Deny     []string   `yaml:"deny"`
	Except   StringList `yaml:"except"`
	Reason   string     `yaml:"reason"`
	Severity string     `yaml:"severity"`
}

// DepsPolicy gates external dependencies declared in package.json, go.mod,
// and requirements.txt. Deny always wins; with policy "allowlist", every
// dependency must match an allow pattern.
type DepsPolicy struct {
	Policy   string   `yaml:"policy"`
	Allow    []string `yaml:"allow"`
	Deny     []string `yaml:"deny"`
	Reason   string   `yaml:"reason"`
	Severity string   `yaml:"severity"`
}

// Coverage requires each scanned file to belong to some layer, so new files
// cannot be dropped outside the declared architecture.
type Coverage struct {
	RequireLayer bool       `yaml:"require-layer"`
	Except       StringList `yaml:"except"`
	Reason       string     `yaml:"reason"`
	Severity     string     `yaml:"severity"`
}

// PairRule requires a companion file to exist for each file matching Target.
// In Requires, "{name}" expands to the file's base name without extension.
type PairRule struct {
	Target   StringList `yaml:"target"`
	Requires string     `yaml:"requires"`
	Reason   string     `yaml:"reason"`
	Severity string     `yaml:"severity"`
}

// CycleRule forbids circular dependencies between project files.
type CycleRule struct {
	Deny     bool       `yaml:"deny"`
	Except   StringList `yaml:"except"`
	Reason   string     `yaml:"reason"`
	Severity string     `yaml:"severity"`
}

// EncapsulationRule forces other layers to import Layer only through its
// Entry files, so internals stay private to the layer.
type EncapsulationRule struct {
	Layer    string     `yaml:"layer"`
	Entry    StringList `yaml:"entry"`
	Reason   string     `yaml:"reason"`
	Severity string     `yaml:"severity"`
}

// Resolve tunes import resolution.
type Resolve struct {
	// Aliases maps import prefixes to project paths, tsconfig-style:
	// "@/*": "src/*". Merged with tsconfig.json paths; these win.
	Aliases map[string]StringList `yaml:"aliases"`
}

// AliasMap returns the manual aliases as plain string slices.
func (c *Config) AliasMap() map[string][]string {
	if c.Resolve == nil || len(c.Resolve.Aliases) == 0 {
		return nil
	}
	out := make(map[string][]string, len(c.Resolve.Aliases))
	for k, v := range c.Resolve.Aliases {
		out[k] = v
	}
	return out
}

// Layer describes one architectural layer.
type Layer struct {
	// Path holds one or more glob patterns (doublestar) relative to the root.
	Path StringList `yaml:"path"`
	// Description explains the layer's responsibility. Surfaced to humans
	// in error output and to AI agents in JSON output.
	Description string `yaml:"description"`
}

// Rule is a single dependency rule. Exactly one of Allow or Deny is set,
// using arrow notation: "ui -> usecase". "*" is a wildcard for any layer.
type Rule struct {
	Allow    string `yaml:"allow"`
	Deny     string `yaml:"deny"`
	Reason   string `yaml:"reason"`
	Severity string `yaml:"severity"`

	// Parsed form, populated by Validate.
	Kind RuleKind `yaml:"-"`
	From string   `yaml:"-"`
	To   string   `yaml:"-"`
}

type RuleKind int

const (
	KindAllow RuleKind = iota
	KindDeny
)

// Expr returns the original arrow expression of the rule.
func (r Rule) Expr() string {
	if r.Kind == KindDeny {
		return r.Deny
	}
	return r.Allow
}

// MetricGroup applies metric limits to files matching Target.
type MetricGroup struct {
	Target   StringList `yaml:"target"`
	Reason   string     `yaml:"reason"`
	Severity string     `yaml:"severity"`

	// Limits. Zero means "not set".
	MaxLines   int `yaml:"max-lines"`
	MaxImports int `yaml:"max-imports"`
}

// StringList accepts either a single YAML string or a list of strings.
type StringList []string

func (s *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var v string
		if err := node.Decode(&v); err != nil {
			return err
		}
		*s = []string{v}
		return nil
	case yaml.SequenceNode:
		var v []string
		if err := node.Decode(&v); err != nil {
			return err
		}
		*s = v
		return nil
	default:
		return fmt.Errorf("expected string or list of strings at line %d", node.Line)
	}
}

// Load reads and validates an arch.yaml file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// Validate checks internal consistency and parses rule expressions.
func (c *Config) Validate() error {
	if len(c.Layers) == 0 {
		return fmt.Errorf("no layers defined")
	}
	for name, l := range c.Layers {
		if len(l.Path) == 0 {
			return fmt.Errorf("layer %q has no path", name)
		}
	}
	for i := range c.Rules {
		r := &c.Rules[i]
		expr := r.Allow
		r.Kind = KindAllow
		if r.Deny != "" {
			if r.Allow != "" {
				return fmt.Errorf("rule %d: cannot set both allow and deny", i+1)
			}
			expr = r.Deny
			r.Kind = KindDeny
		}
		if expr == "" {
			return fmt.Errorf("rule %d: one of allow or deny is required", i+1)
		}
		from, to, err := parseArrow(expr)
		if err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
		for _, side := range []string{from, to} {
			if side == "*" {
				continue
			}
			if _, ok := c.Layers[side]; !ok {
				return fmt.Errorf("rule %d: unknown layer %q (known: %s)", i+1, side, strings.Join(c.LayerNames(), ", "))
			}
		}
		r.From, r.To = from, to
	}
	for i, m := range c.Metrics {
		if len(m.Target) == 0 {
			return fmt.Errorf("metrics %d: target is required", i+1)
		}
		if m.MaxLines == 0 && m.MaxImports == 0 {
			return fmt.Errorf("metrics %d: at least one limit (max-lines, max-imports) is required", i+1)
		}
	}
	for i, n := range c.Naming {
		if len(n.Target) == 0 {
			return fmt.Errorf("naming %d: target is required", i+1)
		}
		if n.FilePattern == "" && n.SymbolPattern == "" {
			return fmt.Errorf("naming %d: one of file-pattern, symbol-pattern is required", i+1)
		}
	}
	for i, b := range c.Bans {
		if len(b.Target) == 0 {
			return fmt.Errorf("bans %d: target is required", i+1)
		}
		if len(b.Imports) == 0 && len(b.Calls) == 0 {
			return fmt.Errorf("bans %d: one of imports, calls is required", i+1)
		}
	}
	for name, pr := range map[string]*PatternRule{"suppressions": c.Suppressions, "placeholders": c.Placeholders} {
		if pr != nil && len(pr.Deny) == 0 {
			return fmt.Errorf("%s: deny is required", name)
		}
	}
	if d := c.Dependencies; d != nil {
		switch d.Policy {
		case "", "denylist":
			if len(d.Deny) == 0 {
				return fmt.Errorf("dependencies: deny is required with denylist policy")
			}
		case "allowlist":
			if len(d.Allow) == 0 {
				return fmt.Errorf("dependencies: allow is required with allowlist policy")
			}
		default:
			return fmt.Errorf("dependencies: policy must be allowlist or denylist, got %q", d.Policy)
		}
	}
	for i, p := range c.Pairing {
		if len(p.Target) == 0 || p.Requires == "" {
			return fmt.Errorf("pairing %d: target and requires are required", i+1)
		}
	}
	for i, e := range c.Encapsulation {
		if e.Layer == "" || len(e.Entry) == 0 {
			return fmt.Errorf("encapsulation %d: layer and entry are required", i+1)
		}
		if _, ok := c.Layers[e.Layer]; !ok {
			return fmt.Errorf("encapsulation %d: unknown layer %q", i+1, e.Layer)
		}
	}
	if err := c.validateSeverities(); err != nil {
		return err
	}
	return nil
}

// validateSeverities checks every severity field is "", "error", or "warn".
func (c *Config) validateSeverities() error {
	check := func(where, s string) error {
		if s != "" && s != "error" && s != "warn" {
			return fmt.Errorf("%s: severity must be error or warn, got %q", where, s)
		}
		return nil
	}
	var err error
	join := func(where, s string) {
		if err == nil {
			err = check(where, s)
		}
	}
	for i, r := range c.Rules {
		join(fmt.Sprintf("rule %d", i+1), r.Severity)
	}
	for i, m := range c.Metrics {
		join(fmt.Sprintf("metrics %d", i+1), m.Severity)
	}
	for i, n := range c.Naming {
		join(fmt.Sprintf("naming %d", i+1), n.Severity)
	}
	for i, b := range c.Bans {
		join(fmt.Sprintf("bans %d", i+1), b.Severity)
	}
	if c.Suppressions != nil {
		join("suppressions", c.Suppressions.Severity)
	}
	if c.Placeholders != nil {
		join("placeholders", c.Placeholders.Severity)
	}
	if c.Dependencies != nil {
		join("dependencies", c.Dependencies.Severity)
	}
	if c.Coverage != nil {
		join("coverage", c.Coverage.Severity)
	}
	for i, p := range c.Pairing {
		join(fmt.Sprintf("pairing %d", i+1), p.Severity)
	}
	if c.Cycles != nil {
		join("cycles", c.Cycles.Severity)
	}
	for i, e := range c.Encapsulation {
		join(fmt.Sprintf("encapsulation %d", i+1), e.Severity)
	}
	return err
}

// LayerNames returns layer names sorted alphabetically.
func (c *Config) LayerNames() []string {
	names := make([]string, 0, len(c.Layers))
	for n := range c.Layers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func parseArrow(expr string) (from, to string, err error) {
	parts := strings.Split(expr, "->")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected %q notation, got %q", "from -> to", expr)
	}
	// In `deny: domain -> "*"` the whole value is a plain YAML scalar, so the
	// inner quotes around * survive as literal characters. Strip them.
	from = strings.Trim(strings.TrimSpace(parts[0]), `"'`)
	to = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	if from == "" || to == "" {
		return "", "", fmt.Errorf("empty side in rule %q", expr)
	}
	return from, to, nil
}
