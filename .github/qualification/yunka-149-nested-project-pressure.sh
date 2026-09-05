#!/usr/bin/env bash
set -euo pipefail

: "${YUNKA_BIN:?YUNKA_BIN is required}"
: "${YUNKA_ROOT:?YUNKA_ROOT is required}"
: "${REPO:?REPO is required}"
: "${ROOT:?ROOT is required}"
: "${EVIDENCE:?EVIDENCE is required}"

mkdir -p "$EVIDENCE"

test "$ROOT" = "$REPO/backend-yunka"
test -f "$ROOT/.yunka/project.json"
test ! -f "$REPO/.yunka/project.json"

git -C "$REPO" config user.email "yunka-149@example.invalid"
git -C "$REPO" config user.name "Yunka 149 Qualification"

"$YUNKA_BIN" context \
  --root "$ROOT" \
  --json > "$EVIDENCE/context.json"

CONTEXT="$EVIDENCE/context.json" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['CONTEXT']).read_text())
# Context projections have evolved, so assert only the durable nested-profile fact.
text = json.dumps(value, sort_keys=True)
assert 'backend-yunka' in text or value.get('profiled') is True, value
PY

PROTOC_GEN_GO="$(command -v protoc-gen-go)" \
PROTOC_GEN_GO_GRPC="$(command -v protoc-gen-go-grpc)" \
"$YUNKA_BIN" generate \
  --root "$ROOT" \
  --full \
  --protoc "$(command -v protoc)" \
  --proto-path "$YUNKA_ROOT/contracts/proto" \
  > "$EVIDENCE/generate.log" 2>&1

PROTOC_GEN_GO="$(command -v protoc-gen-go)" \
PROTOC_GEN_GO_GRPC="$(command -v protoc-gen-go-grpc)" \
"$YUNKA_BIN" check \
  --root "$ROOT" \
  --full \
  --protoc "$(command -v protoc)" \
  --proto-path "$YUNKA_ROOT/contracts/proto" \
  --format agent-json \
  > "$EVIDENCE/check.json" 2> "$EVIDENCE/check.stderr"

go -C "$ROOT" test ./... > "$EVIDENCE/go-test.log" 2>&1

# Candidate generation may legitimately refresh historical generated output.
# Freeze that exact canonical nested-project state as the immutable pressure base.
git -C "$REPO" add backend-yunka
if ! git -C "$REPO" diff --cached --quiet; then
  git -C "$REPO" commit -m "qualification: canonicalize nested project with issue 149 candidate" \
    > "$EVIDENCE/canonical-commit.log" 2>&1
else
  printf 'no canonical generated delta\n' > "$EVIDENCE/canonical-commit.log"
fi
base_sha="$(git -C "$REPO" rev-parse HEAD)"
printf '%s\n' "$base_sha" > "$EVIDENCE/base-sha.txt"

git -C "$REPO" status --porcelain -- backend-yunka > "$EVIDENCE/canonical-project.status"
test ! -s "$EVIDENCE/canonical-project.status"

# Audit baseline reads must reach backend-yunka/go.mod, generated Manifest, and
# historical Go source through repository-relative Git tree paths while the
# returned evidence remains project-relative.
"$YUNKA_BIN" audit \
  --root "$ROOT" \
  --base HEAD \
  --format agent-json \
  > "$EVIDENCE/audit-base.json"

AUDIT="$EVIDENCE/audit-base.json" BASE_SHA="$base_sha" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['AUDIT']).read_text())
debt = value.get('debt') or {}
assert debt.get('baseSha') == os.environ['BASE_SHA'], debt
assert debt.get('new') == [], debt
assert debt.get('fixed') == [], debt
source = value.get('source') or {}
assert source.get('sourceRoot') in ('.', 'internal'), source
for file in source.get('files') or []:
    path = file.get('path', '')
    assert not path.startswith('backend-yunka/'), file
PY

contract="$RUNNER_TEMP/yunka-149-change-contract.json"
changeset="$RUNNER_TEMP/yunka-149-change-set.json"
mutation_rel="internal/delivery/application/operations.go"
mutation="$ROOT/$mutation_rel"

test -f "$mutation"

"$YUNKA_BIN" change begin \
  --root "$ROOT" \
  --operation delivery.projects.list \
  --intent implementation \
  --base HEAD \
  --path "$mutation_rel" \
  --output "$contract" \
  --format agent-json \
  > "$EVIDENCE/change-contract.json"

"$YUNKA_BIN" change set begin \
  --root "$ROOT" \
  --base HEAD \
  --contract "$contract" \
  --output "$changeset" \
  --format agent-json \
  > "$EVIDENCE/change-set.json"

# Clean nested project must reconcile before mutation. This exercises immutable
# Manifest/OperationPlan reads with the actual nested root.
"$YUNKA_BIN" change set check \
  --root "$ROOT" \
  --set "$changeset" \
  --format agent-json \
  > "$EVIDENCE/change-set-clean.json"

CLEAN="$EVIDENCE/change-set-clean.json" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['CLEAN']).read_text())
assert value.get('conformant') is True, value
assert (value.get('reconciliation') or {}).get('violations') == [], value
assert (value.get('semantic') or {}).get('violations') == [], value
PY

printf '\n// issue-149 nested-project Git path qualification\n' >> "$mutation"

"$YUNKA_BIN" change check \
  --root "$ROOT" \
  --contract "$contract" \
  --format agent-json \
  > "$EVIDENCE/change-check-mutated.json"

"$YUNKA_BIN" change set check \
  --root "$ROOT" \
  --set "$changeset" \
  --format agent-json \
  > "$EVIDENCE/change-set-mutated.json"

EVIDENCE="$EVIDENCE" MUTATION_REL="$mutation_rel" python3 - <<'PY'
import json, os
from pathlib import Path
root = Path(os.environ['EVIDENCE'])
mutation = os.environ['MUTATION_REL']
change = json.loads((root / 'change-check-mutated.json').read_text())
assert change.get('violations') == [], change
changes = change.get('changes') or []
assert len(changes) == 1, changes
assert changes[0].get('path') == mutation, changes
assert not changes[0].get('path', '').startswith('backend-yunka/'), changes
assert changes[0].get('class') == 'editable', changes

change_set = json.loads((root / 'change-set-mutated.json').read_text())
assert change_set.get('conformant') is True, change_set
reconciliation = change_set.get('reconciliation') or {}
assert reconciliation.get('violations') == [], change_set
paths = [item.get('path') for item in reconciliation.get('changes') or []]
assert paths == [mutation], paths
assert (change_set.get('semantic') or {}).get('violations') == [], change_set
PY

# Re-run Audit from the immutable base after a non-semantic source edit. The
# nested Git conversion must still work and must not invent debt from path form.
"$YUNKA_BIN" audit \
  --root "$ROOT" \
  --base "$base_sha" \
  --format agent-json \
  > "$EVIDENCE/audit-mutated.json"

AUDIT="$EVIDENCE/audit-mutated.json" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['AUDIT']).read_text())
debt = value.get('debt') or {}
assert debt.get('new') == [], debt
assert debt.get('fixed') == [], debt
for finding in value.get('findings') or []:
    for evidence in finding.get('evidence') or []:
        path = evidence.get('path', '')
        assert not path.startswith('backend-yunka/'), evidence
PY

# Restore the runner-only mutation; workflow cleanup restores the qualification
# branch itself and removes the exact candidate checkout.
git -C "$REPO" checkout -- "$mutation_rel" 2>/dev/null || \
  git -C "$REPO" checkout -- "backend-yunka/$mutation_rel"
git -C "$REPO" status --porcelain -- backend-yunka > "$EVIDENCE/final-project.status"
test ! -s "$EVIDENCE/final-project.status"
