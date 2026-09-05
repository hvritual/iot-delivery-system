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
project = value.get('project') or {}
assert project.get('profiled') is True, value
profile = project.get('profile') or ''
assert profile.endswith('.yunka/project.json'), value
for location in value.get('locations') or []:
    path = location.get('path', '')
    assert not path.startswith('backend-yunka/'), location
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

CHECK="$EVIDENCE/check.json" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['CHECK']).read_text())
assert value.get('ok') is True, value
assert value.get('diagnostics') == [], value
PY

# The preserved RED parent predates later consumer ListProjects completion, so
# its own business test suite is not a valid #149 gate. #149 qualifies the Git
# path-domain closure against the canonical source/contract that actually
# existed at this commit; framework CI/production independently prove Yunka's
# complete behavior suite.

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
report = value.get('report') or value
# Keep assertions schema-tolerant but exact on the debt/path invariants.
debt = report.get('debt') or {}
assert debt.get('baseSha') == os.environ['BASE_SHA'], debt
assert debt.get('new') == [], debt
assert debt.get('fixed') == [], debt
for finding in report.get('findings') or []:
    for evidence in finding.get('evidence') or []:
        path = evidence.get('path', '')
        assert not path.startswith('backend-yunka/'), evidence
PY

contract="$RUNNER_TEMP/yunka-149-change-contract.json"
changeset="$RUNNER_TEMP/yunka-149-change-set.json"
mutation_rel="internal/delivery/application/operations.go"
mutation="$ROOT/$mutation_rel"

test -f "$mutation"

# Use a canonical Operation that existed at the preserved RED parent. Later
# ListProjects work is deliberately outside this issue's historical surface.
"$YUNKA_BIN" change begin \
  --root "$ROOT" \
  --operation delivery.projects.create \
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
report = value.get('report') or value
debt = report.get('debt') or {}
assert debt.get('new') == [], debt
assert debt.get('fixed') == [], debt
for finding in report.get('findings') or []:
    for evidence in finding.get('evidence') or []:
        path = evidence.get('path', '')
        assert not path.startswith('backend-yunka/'), evidence
PY

# Restore the runner-only mutation and prove the nested project itself returns
# to the immutable canonical pressure base.
git -C "$REPO" checkout -- "backend-yunka/$mutation_rel"
git -C "$REPO" status --porcelain -- backend-yunka > "$EVIDENCE/final-project.status"
test ! -s "$EVIDENCE/final-project.status"
