// Package report renders check results for humans (text) and machines (JSON).
package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/yasomaru/lintel/internal/rules"
)

// Summary is the machine-readable result of a check run.
type Summary struct {
	Violations []rules.Violation `json:"violations"`
	Baselined  int               `json:"baselined"`
	Files      int               `json:"files"`
	OK         bool              `json:"ok"`
}

// JSON writes the summary as indented JSON.
func JSON(w io.Writer, s Summary) error {
	if s.Violations == nil {
		s.Violations = []rules.Violation{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// Human writes a human-readable report.
func Human(w io.Writer, s Summary) {
	for _, v := range s.Violations {
		loc := v.File
		if v.Line > 0 {
			loc = fmt.Sprintf("%s:%d", v.File, v.Line)
		}
		fmt.Fprintf(w, "✗ %s\n    rule: %s\n    %s\n", loc, v.Rule, v.Detail)
		if v.Reason != "" {
			fmt.Fprintf(w, "    why:  %s\n", v.Reason)
		}
	}
	if len(s.Violations) > 0 {
		fmt.Fprintln(w)
	}
	status := "ok"
	if !s.OK {
		status = "failed"
	}
	fmt.Fprintf(w, "%s: %d file(s) checked, %d violation(s)", status, s.Files, len(s.Violations))
	if s.Baselined > 0 {
		fmt.Fprintf(w, ", %d baselined", s.Baselined)
	}
	fmt.Fprintln(w)
}
