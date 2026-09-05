#!/usr/bin/env bash
set -euo pipefail

EXPECTED_BASE="f66019e605affe1eedbf20801fab4f22a0903621"
EXPECTED_YUNKA="057ebcf88a87303eb633eb6e604d306f633dfac0"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
REPOSITORY_ROOT="$(CDPATH= cd -- "$BACKEND_ROOT/.." && pwd)"
WEB_ROOT="$REPOSITORY_ROOT/web"

GO_COMMAND="${GO:-go}"
if [[ "$GO_COMMAND" == */* ]]; then
  GO_BIN="$GO_COMMAND"
else
  GO_BIN="$(command -v "$GO_COMMAND" || true)"
fi
if [[ -z "$GO_BIN" || ! -x "$GO_BIN" ]]; then
  echo "YU-30 requires an executable Go toolchain (set GO if needed)" >&2
  exit 2
fi
GO_BIN="$(CDPATH= cd -- "$(dirname -- "$GO_BIN")" && pwd)/$(basename -- "$GO_BIN")"
TOOLS_DIR="${TOOLS_DIR:-$BACKEND_ROOT/.tools}"
export PATH="$(dirname -- "$GO_BIN"):$PATH"

log() {
  printf '\n==> %s\n' "$*"
}

require_clean_tree() {
  local state
  state="$(git -C "$REPOSITORY_ROOT" status --porcelain --untracked-files=all)"
  if [[ -n "$state" ]]; then
    echo "YU-30 requires a clean repository worktree; observed:" >&2
    printf '%s\n' "$state" >&2
    exit 1
  fi
}

require_canonical_identity() {
  local yunka
  git -C "$REPOSITORY_ROOT" merge-base --is-ancestor "$EXPECTED_BASE" HEAD || {
    echo "YU-30 HEAD must descend from fixed parent $EXPECTED_BASE" >&2
    exit 1
  }
  yunka="$(git -C "$REPOSITORY_ROOT" ls-tree HEAD -- third_party/yunka | awk '{print $3}')"
  [[ "$yunka" == "$EXPECTED_YUNKA" ]] || {
    echo "YU-30 requires third_party/yunka@$EXPECTED_YUNKA; observed $yunka" >&2
    exit 1
  }
}

make_yunka() {
  make -C "$BACKEND_ROOT" GO="$GO_BIN" TOOLS_DIR="$TOOLS_DIR" "$@"
}

go_backend() {
  (cd "$BACKEND_ROOT" && env GOWORK=off "$GO_BIN" "$@")
}

log "identity and clean-tree preflight"
require_canonical_identity
require_clean_tree
make_yunka yunka-toolchain-check

log "canonical generate/check pass 1"
make_yunka yunka-generate
make_yunka yunka-check
require_clean_tree

log "canonical generate/check pass 2"
make_yunka yunka-generate
make_yunka yunka-check
require_clean_tree

log "ownership, audit, ChangeSet-equivalence and no-bypass focused gates"
go_backend test . -count=1
go_backend test ./internal/delivery -run 'Ownership|GeneratedRPCOperationPlansRemainCanonical|Transactional|Segregation' -count=1
go_backend test ./internal/audit ./internal/bffhttp -count=1
go_backend test ./internal/httpapi ./internal/localbffhttp -count=1

log "Go module and full regression"
go_backend mod tidy -diff
go_backend test ./... -count=1
go_backend vet ./...
go_backend test -race ./... -count=1

log "frontend install/test/typecheck/build"
(
  cd "$WEB_ROOT"
  npm ci
  npm test
  npm run typecheck
  npm run build
)

log "YU-29 real browser E2E"
(
  cd "$WEB_ROOT"
  npm run e2e:yu29
)

log "final drift and whitespace gates"
git -C "$REPOSITORY_ROOT" diff --check
require_clean_tree

printf '\nYU-30 REGRESSION PASS\n'
