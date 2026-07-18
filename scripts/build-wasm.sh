#!/usr/bin/env bash
# Builds internal/sitter/lib/ts.wasm: the tree-sitter runtime plus the
# grammars lintel bundles, compiled to wasm32-wasi with zig.
#
# Sources come from github.com/malivvan/tree-sitter (MIT), which vendors
# the tree-sitter core and generated grammar C sources.
#
# Requirements: zig (https://ziglang.org), git.
set -euo pipefail

UPSTREAM_REPO="https://github.com/malivvan/tree-sitter"
UPSTREAM_REF="v0.0.1"
OUT="$(cd "$(dirname "$0")/.." && pwd)/internal/sitter/lib/ts.wasm"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
git clone --quiet --depth 1 --branch "$UPSTREAM_REF" "$UPSTREAM_REPO" "$work/upstream"
SRC="$work/upstream/src"

zig cc --target=wasm32-wasi-musl -mexec-model=reactor -I "$SRC" \
    "$SRC/lib.c" \
    "$SRC/typescript/typescript/parser.c" "$SRC/typescript/typescript/scanner.c" \
    "$SRC/typescript/tsx/parser.c" "$SRC/typescript/tsx/scanner.c" \
    "$SRC/javascript/parser.c" "$SRC/javascript/scanner.c" \
    "$SRC/golang/parser.c" \
    "$SRC/python/parser.c" "$SRC/python/scanner.c" \
    "$SRC/java/parser.c" \
    -o "$OUT" -Os -fPIC -Wl,--no-entry -Wl,-z -Wl,stack-size=65536 -Wl,--strip-debug \
    -Wl,--import-symbols \
    -Wl,--export=malloc \
    -Wl,--export=free \
    -Wl,--export=strlen \
    -Wl,--export=ts_parser_new \
    -Wl,--export=ts_parser_parse_string \
    -Wl,--export=ts_parser_set_language \
    -Wl,--export=ts_parser_delete \
    -Wl,--export=ts_query_new \
    -Wl,--export=ts_query_delete \
    -Wl,--export=ts_query_cursor_new \
    -Wl,--export=ts_query_cursor_delete \
    -Wl,--export=ts_query_cursor_exec \
    -Wl,--export=ts_query_cursor_next_match \
    -Wl,--export=ts_query_capture_name_for_id \
    -Wl,--export=ts_language_name \
    -Wl,--export=ts_language_version \
    -Wl,--export=ts_tree_root_node \
    -Wl,--export=ts_tree_delete \
    -Wl,--export=ts_node_string \
    -Wl,--export=ts_node_child_count \
    -Wl,--export=ts_node_named_child_count \
    -Wl,--export=ts_node_child \
    -Wl,--export=ts_node_named_child \
    -Wl,--export=ts_node_type \
    -Wl,--export=ts_node_start_byte \
    -Wl,--export=ts_node_end_byte \
    -Wl,--export=ts_node_is_error \
    -Wl,--export=tree_sitter_typescript \
    -Wl,--export=tree_sitter_tsx \
    -Wl,--export=tree_sitter_javascript \
    -Wl,--export=tree_sitter_go \
    -Wl,--export=tree_sitter_python \
    -Wl,--export=tree_sitter_java

ls -lh "$OUT"
