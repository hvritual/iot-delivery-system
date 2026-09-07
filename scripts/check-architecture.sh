#!/usr/bin/env bash
set -euo pipefail
ROOT="$(git -C "$(dirname -- "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
export GOWORK=off GOTOOLCHAIN=local PYTHONDONTWRITEBYTECODE=1
BASE="${1:-${IOT_GATE_BASE:-}}"
if [[ -z "$BASE" ]]; then
  BASE="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["base"])' "$ROOT/.architecture/change.json")"
fi
OUT="${IOT_ARCH_REPORT_DIR:-}"
if [[ -z "$OUT" ]]; then
  OUT="$(mktemp -d)"
  trap 'rm -rf -- "$OUT"' EXIT
fi
mkdir -p "$OUT"
python3 "$ROOT/scripts/delivery_gates.py" task --root "$ROOT" --base "$BASE" > "$OUT/task.json"
(cd "$ROOT/tools/archgate" && go run . --root "$ROOT") > "$OUT/architecture.json"
python3 - "$OUT/architecture.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
assert not r['blocking']
print('ARCHITECTURE PASS head=%s packages=%d retained_frozen_debt=%d platform=%s' %
      (r['head'], len(r['inventory']), len(r['retained_frozen_debt']), r['typed_platform']))
PY
