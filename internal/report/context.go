package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/yasomaru/lintel/internal/config"
)

// Context writes a compact Markdown summary of the architecture rules,
// meant to be pasted into CLAUDE.md / AGENTS.md so coding agents know the
// constraints up front without loading the full config.
func Context(w io.Writer, cfg *config.Config) {
	fmt.Fprintln(w, "## Architecture rules (enforced by lintel)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This repo's architecture is declared in `arch.yaml` and enforced by")
	fmt.Fprintln(w, "`lintel check` (violations fail CI and include the reason). Before")
	fmt.Fprintln(w, "creating a file, `lintel rules <path>` lists the rules that apply to it.")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Layers:")
	for _, name := range cfg.LayerNames() {
		l := cfg.Layers[name]
		fmt.Fprintf(w, "- **%s** (`%s`)", name, strings.Join(l.Path, "`, `"))
		if l.Description != "" {
			fmt.Fprintf(w, ": %s", l.Description)
		}
		fmt.Fprintln(w)
	}

	if len(cfg.Rules) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Dependency rules (deny wins; `*` = any layer):")
		for _, r := range cfg.Rules {
			kind := "allow"
			if r.Kind == config.KindDeny {
				kind = "deny"
			}
			fmt.Fprintf(w, "- `%s: %s`", kind, r.Expr())
			if r.Reason != "" {
				fmt.Fprintf(w, " — %s", r.Reason)
			}
			fmt.Fprintln(w)
		}
	}

	var extra []string
	if cfg.Strict {
		extra = append(extra, "strict mode: cross-layer dependencies not covered by an allow rule are violations")
	}
	if cfg.Cycles != nil && cfg.Cycles.Deny {
		extra = append(extra, "circular dependencies between files are forbidden")
	}
	for _, e := range cfg.Encapsulation {
		extra = append(extra, fmt.Sprintf("layer `%s` may only be imported via `%s`", e.Layer, strings.Join(e.Entry, "`, `")))
	}
	if cfg.Coverage != nil && cfg.Coverage.RequireLayer {
		extra = append(extra, "every source file must belong to a declared layer")
	}
	if cfg.Suppressions != nil {
		extra = append(extra, "suppression markers are forbidden: "+strings.Join(cfg.Suppressions.Deny, ", "))
	}
	if cfg.Placeholders != nil {
		extra = append(extra, "unfinished-code markers are forbidden: "+strings.Join(cfg.Placeholders.Deny, ", "))
	}
	if d := cfg.Dependencies; d != nil {
		if d.Policy == "allowlist" {
			extra = append(extra, "new external dependencies must be added to the allowlist in arch.yaml first")
		} else if len(d.Deny) > 0 {
			extra = append(extra, "banned external dependencies: "+strings.Join(d.Deny, ", "))
		}
	}
	if n := len(cfg.Bans); n > 0 {
		extra = append(extra, fmt.Sprintf("%d ban rule(s) restrict imports/calls per layer (see arch.yaml)", n))
	}
	if n := len(cfg.Metrics); n > 0 {
		extra = append(extra, fmt.Sprintf("%d size-metric rule(s) cap file size / imports / hook counts", n))
	}
	if n := len(cfg.Naming); n > 0 {
		extra = append(extra, fmt.Sprintf("%d naming rule(s) constrain file and symbol names", n))
	}
	if n := len(cfg.Pairing); n > 0 {
		extra = append(extra, fmt.Sprintf("%d pairing rule(s) require companion files (e.g. tests)", n))
	}
	if len(extra) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Also enforced:")
		for _, e := range extra {
			fmt.Fprintf(w, "- %s\n", e)
		}
	}
}
