package report

import (
	"strings"
	"testing"

	"github.com/yasomaru/lintel/internal/rules"
)

func TestMermaid(t *testing.T) {
	var b strings.Builder
	Mermaid(&b, []string{"domain", "infra", "ui"}, []rules.Edge{
		{From: "infra", To: "domain", Count: 4},
		{From: "domain", To: "infra", Count: 1, Denied: true},
	})
	out := b.String()
	for _, want := range []string{
		"graph LR",
		"  ui\n", // isolated layer still appears as a node
		"infra -->|4| domain",
		"domain -.->|1| infra",
		"linkStyle 1 stroke:#e5534b",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDot(t *testing.T) {
	var b strings.Builder
	Dot(&b, []string{"a", "b"}, []rules.Edge{{From: "a", To: "b", Count: 2, Denied: true}})
	out := b.String()
	if !strings.Contains(out, `"a" -> "b" [label=2, color=red, style=dashed];`) {
		t.Errorf("dot output wrong:\n%s", out)
	}
}
