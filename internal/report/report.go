// Package report renders check results for humans (text) and machines (JSON).
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

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

// ANSI styles, emptied when the writer is not a terminal or NO_COLOR is set.
type palette struct {
	red, green, yellow, dim, bold, reset string
}

func styles(w io.Writer) palette {
	if os.Getenv("NO_COLOR") != "" {
		return palette{}
	}
	f, ok := w.(*os.File)
	if !ok {
		return palette{}
	}
	if fi, err := f.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return palette{}
	}
	return palette{
		red: "\x1b[31m", green: "\x1b[32m", yellow: "\x1b[33m",
		dim: "\x1b[2m", bold: "\x1b[1m", reset: "\x1b[0m",
	}
}

// GitHub writes violations as GitHub Actions workflow commands, which
// GitHub renders as inline annotations on the PR diff.
func GitHub(w io.Writer, s Summary) {
	for _, v := range s.Violations {
		line := v.Line
		if line == 0 {
			line = 1
		}
		msg := v.Detail
		if v.Reason != "" {
			msg += " — " + v.Reason
		}
		fmt.Fprintf(w, "::error file=%s,line=%d,title=%s::%s\n",
			v.File, line, escapeProperty("lintel: "+v.Rule), escapeData(msg))
	}
	fmt.Fprintf(w, "%d file(s) checked, %d violation(s)\n", s.Files, len(s.Violations))
}

var dataEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
var propEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")

func escapeData(s string) string     { return dataEscaper.Replace(s) }
func escapeProperty(s string) string { return propEscaper.Replace(s) }

// Human writes a human-readable report.
func Human(w io.Writer, s Summary) {
	p := styles(w)
	for _, v := range s.Violations {
		loc := v.File
		if v.Line > 0 {
			loc = fmt.Sprintf("%s:%d", v.File, v.Line)
		}
		fmt.Fprintf(w, "%s✗%s %s%s%s\n", p.red, p.reset, p.bold, loc, p.reset)
		fmt.Fprintf(w, "    rule: %s%s%s\n", p.yellow, v.Rule, p.reset)
		fmt.Fprintf(w, "    %s\n", v.Detail)
		if v.Reason != "" {
			fmt.Fprintf(w, "    %swhy:  %s%s\n", p.dim, v.Reason, p.reset)
		}
	}
	if len(s.Violations) > 0 {
		fmt.Fprintln(w)
	}
	status := p.green + "ok" + p.reset
	if !s.OK {
		status = p.red + "failed" + p.reset
	}
	fmt.Fprintf(w, "%s: %d file(s) checked, %d violation(s)", status, s.Files, len(s.Violations))
	if s.Baselined > 0 {
		fmt.Fprintf(w, ", %d baselined", s.Baselined)
	}
	fmt.Fprintln(w)
}
