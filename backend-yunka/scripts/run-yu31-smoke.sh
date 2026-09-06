#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE=0501bd4b2295c624b817e28a94eb1f62b08b0d4c
YUNKA=057ebcf88a87303eb633eb6e604d306f633dfac0
GO="${GO:-go}"
export GOWORK=off GOTOOLCHAIN=local
[[ "$($GO version | awk '{print $3}')" == go1.25.13 ]]
git -C "$ROOT" merge-base --is-ancestor "$BASE" HEAD
[[ "$(git -C "$ROOT" ls-tree HEAD -- third_party/yunka | awk '{print $3}')" == "$YUNKA" ]]
[[ "$(git -C "$ROOT/third_party/yunka" rev-parse HEAD)" == "$YUNKA" ]]
test -z "$(git -C "$ROOT" status --porcelain --untracked-files=all)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/yu31-smoke.XXXXXX")"
trap 'rm -rf -- "$WORK"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
OUT="${YU31_EVIDENCE_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/yu31-evidence}"
mkdir -p "$OUT"
git -C "$ROOT" rev-parse HEAD > "$OUT/head.txt"
printf '%s\n' "$YUNKA" > "$OUT/framework-head.txt"
"$GO" version > "$OUT/go-version.txt"
cd "$ROOT/backend-yunka"
for target in yunka-bootstrap iot-delivery-mcp yu29-fixture; do
  "$GO" build -mod=readonly -race -o "$WORK/$target" "./cmd/$target"
done
export YU31_RUNTIME_BIN="$WORK/yunka-bootstrap"
export YU31_MCP_BIN="$WORK/iot-delivery-mcp"
export YU31_FIXTURE_BIN="$WORK/yu29-fixture"
# Missing prerequisites are hard failures, never Skip. All children are owned
# process groups with bounded graceful exit and explicit Wait/reap assertions.
"$GO" test -mod=readonly -race -tags=yu31 -count=1 -timeout=4m -v ./tests/runtime 2>&1 | tee "$OUT/runtime-smoke.log"
git -C "$ROOT" diff --check
git -C "$ROOT" status --porcelain --untracked-files=all > "$OUT/worktree.txt"
git -C "$ROOT" diff --binary > "$OUT/worktree.patch"
test ! -s "$OUT/worktree.txt"
printf 'YU-31 PROCESS / TRANSPORT / CLOSURE PASS\n'
