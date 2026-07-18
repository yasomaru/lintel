# lintel

**Architecture lint for any language.** Declare your layers and rules in one
`arch.yaml`, and lintel enforces them — dependency directions between layers,
and size metrics that catch fat modules before they grow teeth.

> A *lintel* is the beam above a door or window that carries the load of the
> structure above it. This tool does the same for your codebase: it is the
> `lint` that holds your architecture up.

## Why

- **AI-era guardrails.** AI agents write code faster than humans can review.
  Architectural rules need to be enforced mechanically — in CI, in hooks, and
  as structured output that agents themselves can read and act on.
- **One config for the whole repo.** Existing tools are per-language
  (dependency-cruiser, ArchUnit, import-linter, deptrac, ...). lintel is one
  binary and one `arch.yaml` for your frontend, backend, and everything else.
- **Adoptable in real codebases.** A `baseline` quarantines existing
  violations so only *new* ones fail the build.

## Quick start

```console
$ lintel init          # write a starter arch.yaml
$ lintel check         # check the current directory
$ lintel check --format json   # structured output for CI / AI agents
$ lintel baseline      # grandfather existing violations
```

## Configuration

```yaml
# arch.yaml
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
    reason: Fat hooks mix responsibilities. Split them.
  - target: "src/**/service/**"
    max-lines: 300
    max-imports: 15
    reason: High fan-out is a god-class smell.

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

baseline: .lintel-baseline.json
# strict: true   # undeclared layer dependencies also fail
```

## Rule types

| Key | Checks | Typical AI failure it stops |
|---|---|---|
| `rules` | layer dependency direction | infra leaking into domain |
| `metrics` | file size / import fan-out | fat hooks, god services |
| `naming` | file & exported symbol names | convention drift across files |
| `bans` | forbidden imports / calls per target | I/O sneaking into pure layers |
| `suppressions` | lint-silencing markers | `@ts-ignore`-ing its way past errors |
| `placeholders` | unfinished-code markers | "TODO: implement" shipped as done |
| `dependencies` | manifest allowlist / denylist | random npm packages appearing |
| `coverage` | every file belongs to a layer | `utils/` dumping grounds |
| `pairing` | companion file must exist | "I'll add tests later" |

Semantics, in order:

1. `deny` rules win over `allow` rules. `"*"` matches any layer.
2. Imports within the same layer are always allowed.
3. With `strict: true`, an edge between layers that matches no `allow` rule
   is a violation.
4. `description` and `reason` are not comments — they are carried into error
   messages and JSON output, so humans and AI agents see *why* a rule exists.

## Language support (v0)

| Language | Dependency extraction |
|----------|----------------------|
| Go       | `import` declarations, resolved via `go.mod` module path |
| TS / JS  | `import` / `export from` / `require()` / dynamic `import()`, relative paths |
| Python   | `import` / `from ... import`, absolute module paths |

v0 intentionally uses lightweight extraction. The roadmap replaces this with
a tree-sitter backend behind the same interface, which adds precision,
per-framework metrics (e.g. `max-use-state` for React hooks), and more
languages without touching the rule engine.

## Roadmap

- [ ] tree-sitter backend + language packs (replaces v0's regex extraction)
- [ ] TS path aliases (`@/...`) and Python relative imports
- [ ] Framework-aware metrics (`max-use-state`, `max-public-methods`, ...)
- [ ] `--format github` for PR line annotations
- [ ] JSON Schema for `arch.yaml` (editor completion, AI generation)
- [ ] `lintel init --scan`: infer a starter config from the existing tree
- [ ] MCP server mode: let AI agents query the rules *before* writing code

## License

MIT
