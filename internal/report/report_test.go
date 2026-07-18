package report

import (
	"strings"
	"testing"

	"github.com/yasomaru/lintel/internal/rules"
)

func TestGitHubFormat(t *testing.T) {
	var b strings.Builder
	GitHub(&b, Summary{
		Files: 3,
		Violations: []rules.Violation{
			{File: "src/a.ts", Line: 7, Rule: "bans: import axios", Detail: `import "axios" is banned here`, Reason: "no I/O in domain"},
			{File: "package.json", Rule: "dependencies: deny moment", Detail: "dependency banned"},
		},
	})
	out := b.String()
	if !strings.Contains(out, "::error file=src/a.ts,line=7,title=lintel%3A bans%3A import axios::") {
		t.Errorf("annotation properties wrong:\n%s", out)
	}
	if !strings.Contains(out, "banned here — no I/O in domain") {
		t.Errorf("reason not appended:\n%s", out)
	}
	if !strings.Contains(out, "file=package.json,line=1,") {
		t.Errorf("zero line should clamp to 1:\n%s", out)
	}
}

func TestHumanNoColorForNonTTY(t *testing.T) {
	var b strings.Builder
	Human(&b, Summary{OK: true, Files: 1})
	if strings.Contains(b.String(), "\x1b[") {
		t.Errorf("ANSI codes leaked to non-TTY writer: %q", b.String())
	}
}
