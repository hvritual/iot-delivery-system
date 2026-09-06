#!/usr/bin/env bash
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
BASE=5e57424b034ae8da0ac27d6fb920b457005ea253
OUT="${RUNNER_TEMP:-/tmp}/yu32-evidence"
mkdir -p "$OUT"
WORK="$(mktemp -d)"
cleanup() {
  git -C "$ROOT" worktree remove --force "$WORK/baseline" >/dev/null 2>&1 || true
  rm -rf -- "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
git merge-base --is-ancestor "$BASE" HEAD
git worktree add --detach "$WORK/baseline" "$BASE"
# Reuse the exact pinned read-only framework, not a moving checkout/tag.
rmdir "$WORK/baseline/third_party/yunka"
ln -s "$ROOT/third_party/yunka" "$WORK/baseline/third_party/yunka"
cp "$ROOT/backend-yunka/internal/locallogin/yu32h_red_test.go" "$WORK/baseline/backend-yunka/internal/locallogin/yu32h_red_test.go"
set +e
(cd "$WORK/baseline/backend-yunka" && GOWORK=off GOTOOLCHAIN=local go test -mod=readonly -count=1 ./internal/locallogin -run '^TestYU32H(ShortPasswordEnrollmentRejected|OnlineGuessingIsBounded)$') > "$OUT/hardening-baseline-red.log" 2>&1
status=$?
set -e
cat "$OUT/hardening-baseline-red.log"
test "$status" -eq 1
grep -q 'YU32H_RED: short password enrollment was accepted' "$OUT/hardening-baseline-red.log"
grep -q 'YU32H_RED: password guesses remain unbounded after 11 attempts' "$OUT/hardening-baseline-red.log"
grep -q -- '--- FAIL: TestYU32HShortPasswordEnrollmentRejected' "$OUT/hardening-baseline-red.log"
grep -q -- '--- FAIL: TestYU32HOnlineGuessingIsBounded' "$OUT/hardening-baseline-red.log"
# Compiler/environment failures cannot satisfy either exact behavioral marker.
(cd "$ROOT/backend-yunka" && GOWORK=off GOTOOLCHAIN=local go test -mod=readonly -race -count=1 ./internal/localcredential ./internal/locallogin ./internal/localbffhttp -run '^TestYU32H') 2>&1 | tee "$OUT/hardening-green.log"
printf 'YU32H REAL RED AND GREEN PASS\n'
