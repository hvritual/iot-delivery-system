#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
GO_BIN="${GO:-go}"
[[ "$($GO_BIN version | awk '{print $3}')" == go1.25.13 ]] || { echo 'AG-03 INCOMPLETE: Go 1.25.13 required' >&2; exit 2; }
command -v timeout >/dev/null || { echo 'AG-03 INCOMPLETE: GNU timeout required' >&2; exit 2; }
WORK="$(mktemp -d)"
trap 'rm -rf -- "$WORK"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
export GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
SOURCE="$ROOT/cmd/yunka-bootstrap/main.go"
BEFORE="$(sha256sum "$SOURCE")"
for mode in public hidden alias; do
  python3 - "$WORK" "$SOURCE" "$mode" <<'PY'
import json,pathlib,sys
root=pathlib.Path(sys.argv[1]); mode=sys.argv[3]
base='github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application/savedview'
source='package main\nimport _ "'+base+'"\nfunc main(){}\n'
if mode=='hidden': source=source.replace(base,base+'/internal/usecase')
if mode=='alias': source='package main\nimport renamed "'+base+'/internal/usecase"\nvar _ = renamed.New\nfunc main(){}\n'
file=root/(mode+'.go');file.write_text(source)
(root/'overlay.json').write_text(json.dumps({'Replace':{sys.argv[2]:str(file)}}))
PY
  code=0
  (cd "$ROOT" && timeout --kill-after=3s 90s "$GO_BIN" build -mod=readonly -overlay="$WORK/overlay.json" -o "$WORK/probe" ./cmd/yunka-bootstrap) > "$WORK/$mode.log" 2>&1 || code=$?
  if [[ "$mode" == public ]]; then
    [[ "$code" == 0 ]] || { cat "$WORK/$mode.log"; echo 'AG-03 INCOMPLETE: legal import control failed'; exit 1; }
  else
    [[ "$code" == 1 ]] || { cat "$WORK/$mode.log"; echo "AG-03 wrong rejection exit $code"; exit 1; }
    python3 - "$WORK/$mode.log" <<'PY'
import pathlib,re,sys
lines=pathlib.Path(sys.argv[1]).read_text().splitlines()
diagnostics=[line.strip() for line in lines if re.search(r'\.go:\d+:\d+:',line)]
expected='use of internal package github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application/savedview/internal/usecase not allowed'
assert len(diagnostics)==1 and diagnostics[0].endswith(expected), lines
assert not any(x in '\n'.join(lines) for x in ('syntax error','download','timed out','undefined:')),lines
PY
  fi
  echo "AG03_IMPORT_${mode}=PASS"
done
[[ "$(sha256sum "$SOURCE")" == "$BEFORE" ]]
