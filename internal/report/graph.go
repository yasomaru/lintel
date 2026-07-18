package report

import (
	"fmt"
	"io"

	"github.com/yasomaru/lintel/internal/rules"
)

// Mermaid renders the layer graph as a Mermaid flowchart, which GitHub
// renders natively in Markdown. Denied edges are drawn dashed and red.
func Mermaid(w io.Writer, layers []string, edges []rules.Edge) {
	fmt.Fprintln(w, "graph LR")
	for _, l := range layers {
		fmt.Fprintf(w, "  %s\n", l)
	}
	var denied []int
	for i, e := range edges {
		arrow := "-->"
		if e.Denied {
			arrow = "-.->"
			denied = append(denied, i)
		}
		fmt.Fprintf(w, "  %s %s|%d| %s\n", e.From, arrow, e.Count, e.To)
	}
	for _, i := range denied {
		fmt.Fprintf(w, "  linkStyle %d stroke:#e5534b,stroke-width:2px\n", i)
	}
}

// Dot renders the layer graph in Graphviz DOT format.
func Dot(w io.Writer, layers []string, edges []rules.Edge) {
	fmt.Fprintln(w, "digraph lintel {")
	fmt.Fprintln(w, "  rankdir=LR;")
	for _, l := range layers {
		fmt.Fprintf(w, "  %q;\n", l)
	}
	for _, e := range edges {
		attrs := fmt.Sprintf("label=%d", e.Count)
		if e.Denied {
			attrs += ", color=red, style=dashed"
		}
		fmt.Fprintf(w, "  %q -> %q [%s];\n", e.From, e.To, attrs)
	}
	fmt.Fprintln(w, "}")
}
