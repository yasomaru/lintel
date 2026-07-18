# lintel

[![CI](https://github.com/yasomaru/lintel/actions/workflows/ci.yml/badge.svg)](https://github.com/yasomaru/lintel/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fyasomaru%2Flintel%2Fbadges%2Fcoverage.json%3Fv%3D1)](https://github.com/yasomaru/lintel/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/yasomaru/lintel)](https://github.com/yasomaru/lintel/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/yasomaru/lintel/total?label=downloads)](https://github.com/yasomaru/lintel/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/yasomaru/lintel.svg)](https://pkg.go.dev/github.com/yasomaru/lintel)
[![Go Version](https://img.shields.io/github/go-mod/go-version/yasomaru/lintel)](go.mod)
[![Platforms](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-8250df)](https://github.com/yasomaru/lintel/releases/latest)
[![Last Commit](https://img.shields.io/github/last-commit/yasomaru/lintel)](https://github.com/yasomaru/lintel/commits/main)
[![License: MIT](https://img.shields.io/github/license/yasomaru/lintel)](LICENSE)

**Architecture lint for any language.** Declare your layers and rules in one
`arch.yaml`, and lintel enforces them across your whole repo — frontend,
backend, and everything in between. One fast binary, zero runtime
dependencies.

> A *lintel* is the beam above a door or window that carries the load of the
> structure above it. This tool does the same for your codebase: it is the
> `lint` that holds your architecture up.

![lintel check demo](docs/demo.gif)

*The demo project lives in [`examples/demo`](examples/demo) — try it yourself
with `lintel check examples/demo`.*

## Why lintel?

**AI agents write code faster than humans can review it.** Layer violations,
`@ts-ignore` escapes, fat service classes, and surprise npm dependencies used
to be caught in code review — at AI speed, they slip through. lintel turns
your architecture into mechanical rules that fail the build, with structured
JSON output that AI agents can read and fix against.

- **One config for the whole repo.** Existing tools are per-language
  (dependency-cruiser, ArchUnit, import-linter, deptrac...). lintel is one
  `arch.yaml` for Go, TypeScript/JavaScript, and Python together.
- **Rules carry their "why".** `description` and `reason` fields flow into
  error messages and JSON output — humans learn the architecture from the
  errors, and AI agents get the context they need to fix violations
  correctly.
- **Adoptable in brownfield codebases.** `lintel baseline` quarantines
  existing violations so only *new* ones fail the build. Pay the debt down
  at your own pace.
- **Real parsing, still one static binary.** Source files are parsed with
  tree-sitter compiled to WebAssembly and run via [wazero](https://wazero.io)
  — no cgo, cross-compilation intact, and comments or string literals never
  produce false imports.
- **Fast.** Parallel analysis; a 5,000-file project checks in ~1s on a
  laptop — suitable for pre-commit hooks and editor save actions.

## Install

**Prebuilt binaries** (macOS / Linux / Windows) — no Go required:

```console
# macOS (Apple Silicon)
curl -sL https://github.com/yasomaru/lintel/releases/latest/download/lintel_darwin_arm64.tar.gz | tar xz
sudo mv lintel /usr/local/bin/

# Linux (x86_64)
curl -sL https://github.com/yasomaru/lintel/releases/latest/download/lintel_linux_amd64.tar.gz | tar xz
sudo mv lintel /usr/local/bin/
```

All builds and checksums are on the [releases page](https://github.com/yasomaru/lintel/releases).

**With Go:**

```console
go install github.com/yasomaru/lintel/cmd/lintel@latest
```

## Quick start

```console
$ lintel init --scan           # infer layers from your tree, write arch.yaml
$ lintel check                 # check the current directory
$ lintel check --format json   # structured output for CI / AI agents
$ lintel baseline              # grandfather existing violations
```

`init --scan` detects your layers (conventional directory names like
`domain`, `infra`, `hooks`, or your top-level directories as a fallback),
then **analyzes the real imports** and writes every dependency direction
that exists today as an `allow` rule under `strict: true`. The generated
config always passes on the tree it came from; from then on, any *new*
dependency direction fails the check until someone adds it to the config —
your current architecture becomes the enforced one, whatever style it is.
A `domain` layer that is already dependency-free additionally gets locked
down with `deny: domain -> "*"`. Plain `lintel init` writes a minimal
template instead.

## Configuration

Everything lives in one `arch.yaml` at the repo root:

```yaml
layers:
  domain:
    path: "src/domain/**"
    description: Business logic. Must stay free of outward dependencies.
  usecase:
    path: "src/usecase/**"
  infra:
    path: "src/infra/**"
  ui:
    path: ["apps/web/**", "src/components/**"]

rules:
  - allow: ui -> usecase
  - allow: usecase -> domain
  - allow: infra -> domain
  - deny: domain -> "*"
    reason: The domain layer must not depend on any other layer.

metrics:
  - target: "src/hooks/use*.ts"
    max-lines: 150
    max-use-state: 5    # React: too many states in one hook
    max-use-effect: 3   # React: effect soup
    reason: Fat hooks mix responsibilities. Split them.
  - target: "src/**/service/**"
    max-lines: 300
    max-imports: 15
    max-public-methods: 8   # god-class detector (per class / Go receiver)
    reason: High fan-out is a god-class smell.
  - target: "src/**"
    max-function-lines: 80
    max-params: 5
    max-nesting-depth: 4
    reason: Keep functions readable; split when they grow past a screen.

naming:
  - target: "src/hooks/**"
    file-pattern: "use[A-Z]*.ts"
  - target: "src/**/repository/**"
    symbol-pattern: "*Repository"
    reason: Naming consistency keeps the codebase greppable.

bans:
  - target: "src/domain/**"
    imports: ["axios", "@prisma/*"]
    calls: ["fetch("]
    reason: The domain layer performs no I/O. Go through a repository.

suppressions:
  deny: ["@ts-ignore", "eslint-disable", "it.skip"]
  reason: Fix the root cause. Humans may baseline; agents may not suppress.

placeholders:
  deny: ["TODO: implement", "Not implemented"]
  reason: Unfinished code must not merge.

dependencies:
  policy: allowlist
  allow: ["react", "zod", "@tanstack/*"]
  deny: ["moment", "lodash"]
  reason: New dependencies require editing this file, i.e. human review.

coverage:
  require-layer: true
  except: ["*.config.*"]
  reason: No dumping grounds — every file belongs to a declared layer.

pairing:
  - target: "src/usecase/**/*.ts"
    requires: "tests/**/{name}.test.ts"
    reason: Every use case ships with a test.
    severity: warn   # any rule can be warn: reported, but doesn't fail CI

cycles:
  deny: true
  reason: Circular dependencies make modules inseparable.

encapsulation:
  - layer: domain
    entry: "src/domain/index.ts"
    reason: Domain internals are private. Import via the public entry point.

resolve:
  aliases:
    "@/*": "src/*"   # tsconfig.json paths are auto-detected; this overrides

baseline: .lintel-baseline.json
# strict: true   # undeclared layer dependencies also fail
```

### Rule types

| Key | Checks | Typical AI failure it stops |
|---|---|---|
| `rules` | layer dependency direction | infra leaking into domain |
| `metrics` | file/function size, params, nesting, public methods, hook counts | fat hooks, god services |
| `naming` | file & exported symbol names | convention drift across files |
| `bans` | forbidden imports / calls per target | I/O sneaking into pure layers |
| `suppressions` | lint-silencing markers | `@ts-ignore`-ing its way past errors |
| `placeholders` | unfinished-code markers | "TODO: implement" shipped as done |
| `dependencies` | manifest allowlist / denylist | random npm packages appearing |
| `coverage` | every file belongs to a layer | `utils/` dumping grounds |
| `pairing` | companion file must exist | "I'll add tests later" |
| `cycles` | circular dependencies between files | import knots nobody can untangle |
| `encapsulation` | layer accessed only via entry files | reaching into another layer's internals |

### Semantics

1. `deny` rules win over `allow` rules. `"*"` matches any layer.
2. Imports within the same layer are always allowed.
3. With `strict: true`, an edge between layers that matches no `allow` rule
   is a violation.
4. If a file matches multiple layers, the longest (most specific) pattern
   wins.
5. Every rule accepts `severity: warn` — the violation is reported (and
   annotated in PRs) but does not fail the check. Useful for socializing a
   new rule before enforcing it.
6. `description` and `reason` are not comments — they are carried into error
   messages and JSON output, so humans and AI agents see *why* a rule exists.

### Visualize the architecture

`lintel graph` aggregates the real import graph into layer-level edges, in
Mermaid (rendered natively by GitHub in Markdown) or Graphviz DOT. Denied
edges are drawn dashed and red — the diagram shows not just the
architecture you declared, but where reality disagrees with it:

````console
$ lintel graph
graph LR
  domain
  infra
  ui
  ui -->|12| usecase
  usecase -->|9| domain
  infra -->|4| domain
  domain -.->|1| infra
  linkStyle 3 stroke:#e5534b,stroke-width:2px
````

Paste it into a PR description, or keep a living architecture diagram in
your docs with `lintel graph > docs/architecture.mmd` in CI.

### Editor completion

`arch.yaml` has a published [JSON Schema](docs/arch.schema.json). Generated
configs include a modeline that the VS Code YAML extension (and any
yaml-language-server editor) picks up for completion and validation:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/yasomaru/lintel/main/docs/arch.schema.json
```

`lintel schema` prints the same schema for offline use — or for handing to
an AI agent as the contract when asking it to write or edit your rules.

## Using in CI

`lintel check` exits with code 1 on violations — that's all CI needs.
There's a ready-made GitHub Action:

```yaml
# .github/workflows/arch.yml
jobs:
  lintel:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: yasomaru/lintel@v1.1.0   # runs: lintel check --format github
```

With `--format github` (the Action's default), violations appear as inline
annotations on the PR diff, each carrying the rule's `reason`;
`severity: warn` rules become warning annotations.

Or as a [pre-commit](https://pre-commit.com) hook:

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/yasomaru/lintel
    rev: v1.1.0
    hooks:
      - id: lintel
```

Adoption flow for an existing codebase:

1. `lintel baseline` and commit `.lintel-baseline.json` — existing violations
   are grandfathered.
2. CI runs `lintel check` — only **new** violations fail the build.
3. Pay down the debt over time: when baselined violations get fixed, `check`
   notes how many entries are stale so you can regenerate a smaller baseline.

## Using with AI agents

`--format json` emits every violation with its file, line, rule, and reason —
ready to feed back to a coding agent:

```json
{
  "violations": [
    {
      "file": "src/domain/user.ts",
      "line": 1,
      "rule": "bans: import axios",
      "detail": "import \"axios\" is banned here",
      "reason": "The domain layer performs no I/O. Go through a repository."
    }
  ],
  "ok": false
}
```

For example, as a [Claude Code](https://claude.com/claude-code) hook that
checks architecture after every file edit, in `.claude/settings.json`:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [{ "type": "command", "command": "lintel check --format json" }]
      }
    ]
  }
}
```

The agent sees the violation *and the reason* immediately after writing the
offending code, and fixes it before you ever review it.

Agents can also query the constraints *before* writing:

```console
$ lintel rules src/domain/user.ts
{
  "file": "src/domain/user.ts",
  "layer": "domain",
  "layer_description": "Business logic. Must stay free of outward dependencies.",
  "dependencies": [
    { "rule": "deny: domain -> \"*\"", "reason": "The domain layer must not depend on any other layer." }
  ],
  "bans": [
    { "rule": "imports: axios, @prisma/*; calls: fetch(", "reason": "The domain layer performs no I/O. Go through a repository." }
  ]
}
```

No resident server, no always-loaded tool schemas — the tokens are spent
only when the agent actually asks.

To give agents the rules up front, `lintel context` emits a compact
Markdown summary of your architecture — paste it (or generate it in CI)
into `CLAUDE.md` / `AGENTS.md`:

```console
$ lintel context >> CLAUDE.md
```

## Language support

Extraction is AST-based: bundled tree-sitter grammars for TypeScript, TSX,
JavaScript, Go, Python, and Java, parsed in WebAssembly via wazero (cgo-free).
Set `LINTEL_ENGINE=regex` to fall back to the legacy regex extractors.

| Language | Dependency extraction |
|----------|----------------------|
| Go       | `import` declarations, resolved via `go.mod` module path |
| TS / JS  | `import` / `export from` / `require()` / dynamic `import()`; relative paths and path aliases (auto-detected from `tsconfig.json` / `jsconfig.json` `paths`, or set via `resolve.aliases`) |
| Python   | `import` / `from ... import`, absolute and relative (`from . import x`) module paths |
| Java     | `import` declarations incl. `static` and wildcard, resolved by package-path suffix — works with any source root (`src/main/java`, plain `src`, ...) |

Dependency gate manifests: `package.json`, `go.mod`, `requirements.txt`,
`pom.xml`, `build.gradle` / `build.gradle.kts` (Java deps are matched as
`"group:artifact"`, e.g. `deny: ["org.projectlombok:*"]`).

### Known limitations

- Alias detection reads the root `tsconfig.json` only; per-package tsconfigs
  in a monorepo need explicit `resolve.aliases` for now.
- Java classes in the *same* package are visible without an `import`, so a
  same-package dependency crossing layer boundaries is invisible to lintel.
  In practice layers map to distinct packages, where imports are required.
- `suppressions` / `placeholders` / `calls` are substring matches — a pattern
  appearing in a doc comment also counts. (lintel's own CI once flagged
  lintel's source for mentioning a suppression marker in a comment.)
- Import resolution is heuristic (no type checker); imports lintel cannot
  resolve to a project file are treated as external and skipped.

## Roadmap

- [x] TS path aliases (`@/...`) and Python relative imports
- [x] `--format github` for PR line annotations
- [x] JSON Schema for `arch.yaml` (editor completion, AI generation)
- [x] `lintel init --scan`: infer a starter config from the existing tree
- [x] `lintel rules <path>`: let AI agents query the rules *before* writing code
- [x] React metrics (`max-use-state`, `max-use-effect`)
- [x] `lintel context`: emit a CLAUDE.md-ready summary of the architecture
- [x] tree-sitter backend (WebAssembly + wazero, cgo-free) for TS/TSX/JS/Go/Python/Java
- [x] Structural metrics on the AST backend (`max-function-lines`, `max-params`, `max-nesting-depth`, `max-public-methods`)
- [ ] More bundled grammars (Rust, Kotlin, C#, Ruby, PHP)

## Contributing

Issues and PRs are welcome. lintel checks its own architecture in CI
(`arch.yaml` at the repo root) — `go test ./...` and
`go run ./cmd/lintel check .` must both pass.

## License

[MIT](LICENSE)
