#!/usr/bin/env bash
set -euo pipefail

: "${YUNKA_BIN:?YUNKA_BIN is required}"
: "${YUNKA_ROOT:?YUNKA_ROOT is required}"
: "${REPO:?REPO is required}"
: "${EVIDENCE:?EVIDENCE is required}"

ROOT="$REPO/backend-yunka"
mkdir -p "$EVIDENCE"

test -f "$ROOT/.yunka/project.json"
test -f "$ROOT/contracts/proto/iot_delivery.proto"
test ! -e "$ROOT/internal/delivery/application/delivery_releases_list.go"
test ! -e "$ROOT/internal/delivery/application/delivery_sprints_list.go"
test ! -e "$ROOT/internal/delivery/application/delivery_milestones_list.go"

git -C "$REPO" config user.email "yunka-150@example.invalid"
git -C "$REPO" config user.name "Yunka 150 Qualification"

# #149 is already merged and exact-main qualified. Use the preserved original
# nested project profile so this run isolates #150 instead of inheriting the
# later repository-root workaround, which would also scan the unrelated legacy
# sibling backend module.
"$YUNKA_BIN" context --root "$ROOT" --json > "$EVIDENCE/context.json"
CONTEXT="$EVIDENCE/context.json" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['CONTEXT']).read_text())
project = value.get('project') or {}
assert project.get('profiled') is True, value
assert project.get('profile') == '.yunka/project.json', value
assert project.get('contractSource') == 'contracts/proto', value
assert project.get('generatedGoRoot') == 'internal', value
PY

# Bind the canonical nested module to the exact external framework candidate.
# This runner-only dependency binding is committed before any ChangeSet authoring
# so all Git-backed proof uses an immutable clean baseline.
go -C "$ROOT" mod edit \
  -replace="github.com/hvritual/yunka.io/framework=$YUNKA_ROOT/framework" \
  -replace="github.com/hvritual/yunka.io/gateway=$YUNKA_ROOT/gateway" \
  -replace="github.com/hvritual/yunka.io/pkg=$YUNKA_ROOT/pkg"
git -C "$REPO" add backend-yunka/go.mod
git -C "$REPO" commit -m "qualification: bind exact Yunka 150 candidate" > "$EVIDENCE/candidate-binding-commit.log"
base_sha="$(git -C "$REPO" rev-parse HEAD)"
printf '%s\n' "$base_sha" > "$EVIDENCE/base-sha.txt"
git -C "$REPO" status --porcelain > "$EVIDENCE/baseline.status"
test ! -s "$EVIDENCE/baseline.status"

PROTOC="$(command -v protoc)"
PROTOC_GEN_GO="$(command -v protoc-gen-go)"
PROTOC_GEN_GO_GRPC="$(command -v protoc-gen-go-grpc)"
export PROTOC_GEN_GO PROTOC_GEN_GO_GRPC

# The preserved YU-08 RED is a business RED, not a Yunka structural RED. The
# nested project itself must be canonical before the three missing Operations.
"$YUNKA_BIN" check \
  --root "$ROOT" \
  --full \
  --protoc "$PROTOC" \
  --proto-path "$YUNKA_ROOT/contracts/proto" \
  --format agent-json \
  > "$EVIDENCE/baseline-check.json"

CHECK="$EVIDENCE/baseline-check.json" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['CHECK']).read_text())
assert value.get('ok') is True, value
assert value.get('diagnostics') == [], value
PY

SOURCE="contracts/proto/iot_delivery.proto"
APP="delivery/management"

plan_operation() {
  local operation="$1" use_case="$2" permission="$3" rpc="$4" request="$5" response="$6" output="$7"
  "$YUNKA_BIN" add operation \
    --root "$ROOT" \
    --source "$SOURCE" \
    --plan \
    --format agent-json \
    --use-case "$use_case" \
    --rpc-name "$rpc" \
    --request-type "$request" \
    --response-type "$response" \
    --access protected \
    --permission "$permission" \
    --permission-mode all \
    --tenant required \
    --authentication api-key \
    --authentication jwt \
    --authentication service \
    --transaction read-only \
    --idempotency none \
    --composition local \
    "$APP" "$operation" > "$output"
}

apply_operation() {
  local operation="$1" use_case="$2" permission="$3" rpc="$4" request="$5" response="$6" output="$7"
  "$YUNKA_BIN" add operation \
    --root "$ROOT" \
    --source "$SOURCE" \
    --format agent-json \
    --use-case "$use_case" \
    --rpc-name "$rpc" \
    --request-type "$request" \
    --response-type "$response" \
    --access protected \
    --permission "$permission" \
    --permission-mode all \
    --tenant required \
    --authentication api-key \
    --authentication jwt \
    --authentication service \
    --transaction read-only \
    --idempotency none \
    --composition local \
    "$APP" "$operation" > "$output"
}

plan_operation delivery.releases.list list_releases delivery.releases.read ListReleases ListReleasesRequest ListReleasesResponse "$EVIDENCE/releases-plan.json"
plan_operation delivery.sprints.list list_sprints delivery.sprints.read ListSprints ListSprintsRequest ListSprintsResponse "$EVIDENCE/sprints-plan.json"
plan_operation delivery.milestones.list list_milestones delivery.milestones.read ListMilestones ListMilestonesRequest ListMilestonesResponse "$EVIDENCE/milestones-plan.json"

EVIDENCE="$EVIDENCE" python3 - <<'PY'
import json, os
from pathlib import Path
root = Path(os.environ['EVIDENCE'])
for name in ('releases', 'sprints', 'milestones'):
    value = json.loads((root / f'{name}-plan.json').read_text())
    semantics = value.get('explicitSemantics') or {}
    assert semantics.get('authentication') == ['api-key', 'jwt', 'service-token'], value
    assert semantics.get('transaction') == 'read_only', value
    assert semantics.get('composition') == 'local', value
PY

CHANGESET="$RUNNER_TEMP/yunka-150-yu08-changeset.json"
"$YUNKA_BIN" change set begin \
  --root "$ROOT" \
  --base HEAD \
  --create-plan "$EVIDENCE/releases-plan.json" \
  --create-plan "$EVIDENCE/sprints-plan.json" \
  --create-plan "$EVIDENCE/milestones-plan.json" \
  --output "$CHANGESET" \
  --format agent-json \
  > "$EVIDENCE/change-set-begin.json"

# Trusted create-plan revalidation is part of the proof: canonical service-token
# must round-trip directly without translating back to public `service` or
# requiring a second existing-operation ChangeSet.
CHANGESET_PATH="$CHANGESET" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['CHANGESET_PATH']).read_text())
subjects = value.get('subjects') or []
assert len(subjects) == 3, value
for subject in subjects:
    create = subject.get('create') or {}
    semantics = create.get('explicitSemantics') or create.get('semantics') or {}
    if semantics:
        assert semantics.get('authentication') == ['api-key', 'jwt', 'service-token'], subject
PY

apply_operation delivery.releases.list list_releases delivery.releases.read ListReleases ListReleasesRequest ListReleasesResponse "$EVIDENCE/releases-apply.json"
apply_operation delivery.sprints.list list_sprints delivery.sprints.read ListSprints ListSprintsRequest ListSprintsResponse "$EVIDENCE/sprints-apply.json"
apply_operation delivery.milestones.list list_milestones delivery.milestones.read ListMilestones ListMilestonesRequest ListMilestonesResponse "$EVIDENCE/milestones-apply.json"

# The public alias must render the existing protobuf enum. All three new RPCs
# must include API-key/JWT/service authentication before generation.
for enum in AUTHENTICATION_API_KEY AUTHENTICATION_JWT AUTHENTICATION_SERVICE; do
  count="$(grep -c "$enum" "$ROOT/$SOURCE")"
  test "$count" -ge 3
  printf '%s=%s\n' "$enum" "$count" >> "$EVIDENCE/proto-auth-enum-counts.txt"
done

"$YUNKA_BIN" generate \
  --root "$ROOT" \
  --full \
  --protoc "$PROTOC" \
  --proto-path "$YUNKA_ROOT/contracts/proto" \
  > "$EVIDENCE/generate.log" 2>&1

"$YUNKA_BIN" check \
  --root "$ROOT" \
  --full \
  --protoc "$PROTOC" \
  --proto-path "$YUNKA_ROOT/contracts/proto" \
  --format agent-json \
  > "$EVIDENCE/post-generate-check.json"

"$YUNKA_BIN" change set check \
  --root "$ROOT" \
  --set "$CHANGESET" \
  --format agent-json \
  > "$EVIDENCE/change-set-check.json"

EVIDENCE="$EVIDENCE" python3 - <<'PY'
import json, os
from pathlib import Path
root = Path(os.environ['EVIDENCE'])
report = json.loads((root / 'change-set-check.json').read_text())
assert report.get('conformant') is True, report
reconciliation = report.get('reconciliation') or {}
semantic = report.get('semantic') or {}
assert reconciliation.get('violations') == [], report
assert semantic.get('violations') == [], report

check = json.loads((root / 'post-generate-check.json').read_text())
assert check.get('ok') is True, check
assert check.get('diagnostics') == [], check
PY

# Canonical generated manifest must contain all three Operations with the same
# authentication vocabulary. Generated artifacts are produced only by generate.
MANIFEST="$ROOT/contracts/generated/manifest.json"
MANIFEST="$MANIFEST" python3 - <<'PY'
import json, os
from pathlib import Path
value = json.loads(Path(os.environ['MANIFEST']).read_text())
want = {'delivery.releases.list', 'delivery.sprints.list', 'delivery.milestones.list'}
found = {}
def walk(node):
    if isinstance(node, dict):
        op = node.get('operationId') or node.get('id')
        if op in want:
            found[op] = node
        for child in node.values():
            walk(child)
    elif isinstance(node, list):
        for child in node:
            walk(child)
walk(value)
assert set(found) == want, found
for op, node in found.items():
    text = json.dumps(node, sort_keys=True)
    assert 'service-token' in text, (op, node)
    assert 'api-key' in text, (op, node)
    assert 'jwt' in text, (op, node)
PY

git -C "$REPO" status --porcelain -- backend-yunka > "$EVIDENCE/pressure.status"
test -s "$EVIDENCE/pressure.status"
