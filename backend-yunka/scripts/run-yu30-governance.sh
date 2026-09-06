#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE=f66019e605affe1eedbf20801fab4f22a0903621
GO_BIN="${GO:-go}"
OUT="${YU30_EVIDENCE_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/yu30-evidence}"
mkdir -p "$OUT"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/yu30-governance.XXXXXX")"
cleanup() {
  git -C "$ROOT" worktree remove --force "$WORK/checkout" >/dev/null 2>&1 || true
  rm -rf -- "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Use the pinned framework workspace, not the consumer or a public pkg tag.
GOWORK="$ROOT/third_party/yunka/go.work" "$GO_BIN" -C "$ROOT/third_party/yunka/app" build -o "$WORK/yunka" ./cmd

# Ownership is checked for actual changed handwritten Go paths. Framework and
# legacy source are forbidden. Frontend/CI are outside Yunka's Go classifier;
# their own test/build gates remain required rather than calling them editable.
test -z "$(git -C "$ROOT" diff --name-only "$BASE" HEAD -- third_party backend)"
args=()
while IFS= read -r file; do
  [[ -n "$file" ]] && args+=(--path "$file")
done < <(git -C "$ROOT" diff --name-only "$BASE" HEAD -- backend-yunka/internal)
test "${#args[@]}" -gt 0
"$WORK/yunka" ownership check --root "$ROOT" "${args[@]}" --format json > "$OUT/ownership-changed.json"

args=()
while IFS= read -r file; do
  case "$file" in
    backend-yunka/contracts/generated/*|backend-yunka/contracts/delivery/*/*.pb.go|backend-yunka/*/zz_yunka_*_gen.go) args+=(--path "$file");;
  esac
done < <(git -C "$ROOT" ls-files backend-yunka)
test "${#args[@]}" -gt 0
"$WORK/yunka" ownership inspect --root "$ROOT" "${args[@]}" --format json > "$OUT/ownership-generated.json"
node --input-type=module - "$OUT/ownership-generated.json" <<'JS'
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const report = JSON.parse(readFileSync(process.argv[2], 'utf8'));
assert(report.decisions.length > 0, 'generated ownership inventory must not be empty');
for (const item of report.decisions) {
  assert.equal(item.mutation, 'generated-only', item.path);
  assert.equal(item.safeAutoEdit, false, item.path);
}
console.log(`Generated ownership: ${report.decisions.length} protected files`);
JS

# CLI exit zero alone is not an audit pass: inspect the actual debt delta.
"$WORK/yunka" audit --root "$ROOT" --base "$BASE" --format json > "$OUT/yunka-audit.json"
node --input-type=module - "$OUT/yunka-audit.json" "$BASE" <<'JS'
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const report = JSON.parse(readFileSync(process.argv[2], 'utf8'));
assert.equal(report.debt?.baseSha, process.argv[3]);
assert(Array.isArray(report.source?.files) && report.source.files.length > 0, 'audit must inspect real source');
assert(Array.isArray(report.debt.new), 'audit must include a classified debt delta');
console.log(JSON.stringify({ existing: report.debt.existing.map(x => x.id), new: report.debt.new.map(x => x.id), fixed: report.debt.fixed.map(x => x.id) }));
assert.equal(report.debt.existing.length, 0, 'current proven architecture debt is forbidden after YU-32 closure');
assert.equal(report.debt.new.length, 0, 'new proven architecture debt is forbidden');
JS

# Exercise the real ChangeSet CLI against ALL current canonical operations.
# This certifies current-head reconciliation + a negative scope control, NOT a
# claim that operation contracts cover unrelated CI/frontend changes since BASE.
# Those are independently checked by the exact commit diff and their own gates.
git -C "$ROOT" worktree add --detach "$WORK/checkout" HEAD >/dev/null
node --input-type=module - "$ROOT/backend-yunka/contracts/generated/operation-plans.json" > "$WORK/operations.txt" <<'JS'
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const ids = JSON.parse(readFileSync(process.argv[2], 'utf8')).operations.map(x => x.operationId);
assert(ids.length > 0 && new Set(ids).size === ids.length, 'canonical operation IDs must be nonempty and unique');
for (const id of ids) { assert(/^[a-z0-9.-]+$/.test(id)); console.log(id); }
JS
contracts=()
while IFS= read -r operation; do
  contract="$WORK/$operation.json"
  "$WORK/yunka" change begin --root "$WORK/checkout" --base HEAD --operation "$operation" --intent implementation --output "$contract" --format json > /dev/null
  contracts+=(--contract "$contract")
done < "$WORK/operations.txt"
"$WORK/yunka" change set begin --root "$WORK/checkout" --base HEAD "${contracts[@]}" --output "$OUT/change-set.json" --format json > "$OUT/change-set-begin.json"
"$WORK/yunka" change set check --root "$WORK/checkout" --set "$OUT/change-set.json" --format json > "$OUT/change-set-positive.json"
printf 'YU-30 controlled out-of-scope test file\n' > "$WORK/checkout/yu30-out-of-scope-probe.txt"
set +e
"$WORK/yunka" change set check --root "$WORK/checkout" --set "$OUT/change-set.json" --format json > "$OUT/change-set-negative.json"
code=$?
set -e
test "$code" -ne 0
node --input-type=module - "$OUT/change-set-positive.json" "$OUT/change-set-negative.json" <<'JS'
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const positive = JSON.parse(readFileSync(process.argv[2], 'utf8'));
const negative = JSON.parse(readFileSync(process.argv[3], 'utf8'));
assert.equal(positive.conformant, true);
assert.equal(negative.conformant, false);
assert(negative.reconciliation.violations.some(x => x.kind === 'scope' && x.path === 'yu30-out-of-scope-probe.txt'), 'negative control must fail for the exact out-of-scope file');
JS
rm "$WORK/checkout/yu30-out-of-scope-probe.txt"
"$WORK/yunka" change set check --root "$WORK/checkout" --set "$OUT/change-set.json" --format json > "$OUT/change-set-restored.json"
cp "$WORK/operations.txt" "$OUT/change-set-operations.txt"
printf 'YU-30 GOVERNANCE PASS (audit: zero current proven debt; fixed debt retained in report)\n'
