from __future__ import annotations

import json
import re
import shutil
import sys
from pathlib import Path

root = Path(sys.argv[1]).resolve()
proto_root = root / "backend-yunka/contracts/proto"
source = proto_root / "iot_delivery.proto"
bootstrap = proto_root / "yunka_bootstrap.proto"
out_dir = proto_root / "delivery/v1"
manifest_path = root / "backend-yunka/contracts/generated/manifest.json"
baseline_manifest = root / ".proto-mod-baseline-manifest.json"

if not source.is_file():
    raise SystemExit(f"missing source proto: {source}")
if not manifest_path.is_file():
    raise SystemExit(f"missing generated manifest: {manifest_path}")

shutil.copyfile(manifest_path, baseline_manifest)
text = source.read_text(encoding="utf-8")

SERVICE_MARKER = "service DeliveryService {"
service_start = text.index(SERVICE_MARKER)

# Balanced-brace extraction, respecting quoted strings and line comments sufficiently
# for the reviewed proto syntax.
def block_end(data: str, start: int) -> int:
    open_at = data.index("{", start)
    depth = 0
    quote = None
    escaped = False
    line_comment = False
    i = open_at
    while i < len(data):
        ch = data[i]
        nxt = data[i + 1] if i + 1 < len(data) else ""
        if line_comment:
            if ch == "\n":
                line_comment = False
            i += 1
            continue
        if quote is not None:
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == quote:
                quote = None
            i += 1
            continue
        if ch == "/" and nxt == "/":
            line_comment = True
            i += 2
            continue
        if ch in {'"', "'"}:
            quote = ch
            i += 1
            continue
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return i + 1
        i += 1
    raise ValueError(f"unclosed block at {start}")

service_end = block_end(text, service_start)
service_block = text[service_start:service_end].strip() + "\n"
rest = text[service_end:]

message_matches = list(re.finditer(r"(?m)^message\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{", rest))
blocks: dict[str, str] = {}
for index, match in enumerate(message_matches):
    name = match.group(1)
    end = block_end(rest, match.start())
    # Preserve contiguous comments directly above a declaration.
    start = match.start()
    prefix = rest[:start]
    lines = prefix.splitlines(keepends=True)
    comment_start = start
    cursor = start
    while lines:
        line = lines[-1]
        if line.strip().startswith("//") or not line.strip():
            cursor -= len(line)
            lines.pop()
            if line.strip().startswith("//"):
                comment_start = cursor
            continue
        break
    start = comment_start if comment_start < match.start() else match.start()
    blocks[name] = rest[start:end].strip() + "\n"

expected_names = set(blocks)
if not expected_names:
    raise SystemExit("no message declarations parsed")

files: dict[str, set[str]] = {
    "common.proto": {
        "Evidence", "Decision", "WorkItemDependency", "IoTBinding", "TraceLink", "Comment", "Activity",
    },
    "work_item.proto": {
        "ListItemsRequest", "ListItemsResponse", "WorkItemResponse", "CommentResponse", "WorkItem",
        "CreateItemRequest", "UpdateItemRequest", "CreateItemCommentRequest", "UpdateItemContextRequest",
        "AdvanceGateRequest", "CloseItemRequest", "WorkItemFilter", "GetItemRequest", "SearchItemsRequest",
        "SearchItemsResponse", "FindSimilarItemsRequest", "FindSimilarItemsResponse", "SimilarityCandidate",
    },
    "project.proto": {
        "ProjectResponse", "Project", "CreateProjectRequest", "ListProjectsRequest", "ListProjectsResponse",
        "GetProjectProgressRequest", "ProjectProgressResponse", "ProjectProgress", "GetProjectScheduleRequest",
        "ProjectScheduleResponse", "ProjectSchedule", "OwnerCapacity", "ScheduleRisk",
    },
    "planning.proto": {
        "ReleaseResponse", "SprintResponse", "MilestoneResponse", "Release", "Sprint", "Milestone",
        "CreateReleaseRequest", "CreateSprintRequest", "CreateMilestoneRequest", "ListReleasesRequest",
        "ListReleasesResponse", "ListSprintsRequest", "ListSprintsResponse", "ListMilestonesRequest",
        "ListMilestonesResponse",
    },
    "dashboard.proto": {"GetDashboardRequest", "GetDashboardResponse", "Dashboard", "BoardSummary"},
    "saved_view.proto": {
        "SavedView", "MemberWeek", "SaveViewRequest", "ListSavedViewsRequest", "GetMemberWeekRequest",
        "SavedViewResponse", "ListSavedViewsResponse", "MemberWeekResponse",
    },
    "notification.proto": {"ListNotificationsRequest", "ListNotificationsResponse", "Notification"},
}

assigned: dict[str, str] = {}
for filename, names in files.items():
    overlap = set(assigned).intersection(names)
    if overlap:
        raise SystemExit(f"duplicate assignment: {sorted(overlap)}")
    for name in names:
        assigned[name] = filename

missing = expected_names - set(assigned)
extra = set(assigned) - expected_names
if missing or extra:
    raise SystemExit(f"classification mismatch missing={sorted(missing)} extra={sorted(extra)}")

common_header = '''syntax = "proto3";\n\npackage iot.delivery.v1;\n\nimport "yunka/dsl/v1/options.proto";\n{timestamp_import}{cross_imports}\noption go_package = "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1;deliveryv1";\n\n'''

# Resolve message-level file dependencies by symbol reference. This intentionally
# creates imports only for other bounded-context files and rejects cycles.
all_names = sorted(expected_names, key=len, reverse=True)
deps: dict[str, set[str]] = {filename: set() for filename in files}
for filename, names in files.items():
    body = "\n".join(blocks[name] for name in names)
    for symbol in all_names:
        owner = assigned[symbol]
        if owner == filename:
            continue
        if re.search(rf"\b{re.escape(symbol)}\b", body):
            deps[filename].add(owner)

# Verify no circular import graph.
visiting: set[str] = set()
visited: set[str] = set()
def visit(node: str) -> None:
    if node in visiting:
        raise SystemExit(f"circular proto import detected at {node}: {deps}")
    if node in visited:
        return
    visiting.add(node)
    for dep in deps[node]:
        visit(dep)
    visiting.remove(node)
    visited.add(node)
for filename in files:
    visit(filename)

out_dir.mkdir(parents=True, exist_ok=True)

for filename, names in files.items():
    ordered_names = [m.group(1) for m in message_matches if m.group(1) in names]
    body = "\n\n".join(blocks[name].strip() for name in ordered_names) + "\n"
    timestamp_import = 'import "google/protobuf/timestamp.proto";\n' if "google.protobuf.Timestamp" in body else ""
    cross = "".join(f'import "delivery/v1/{dep}";\n' for dep in sorted(deps[filename]))
    if timestamp_import or cross:
        cross = cross + "\n"
    content = common_header.format(timestamp_import=timestamp_import, cross_imports=cross) + body
    (out_dir / filename).write_text(content, encoding="utf-8")

service_imports = "".join(
    f'import "delivery/v1/{filename}";\n' for filename in [
        "common.proto", "work_item.proto", "project.proto", "planning.proto",
        "dashboard.proto", "saved_view.proto", "notification.proto",
    ]
)
service_header = f'''syntax = "proto3";\n\npackage iot.delivery.v1;\n\nimport "yunka/dsl/v1/options.proto";\n{service_imports}\noption go_package = "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1;deliveryv1";\noption (yunka.dsl.v1.domain) = {{ name: "delivery" version: "v1" }};\n\n// DeliveryService remains the single transport-neutral service identity.\n// DTO/schema ownership is split by bounded context to improve review and AI context locality.\n'''
(out_dir / "delivery_service.proto").write_text(service_header + service_block, encoding="utf-8")

source.unlink()
if bootstrap.exists():
    bootstrap.unlink()

layout = {
    "domain": "delivery",
    "version": "v1",
    "service": "delivery/v1/delivery_service.proto",
    "modules": {filename.removesuffix(".proto"): sorted(names) for filename, names in files.items()},
    "imports": {filename: sorted(values) for filename, values in deps.items()},
}
(root / ".proto-mod-layout.json").write_text(json.dumps(layout, indent=2, sort_keys=True) + "\n", encoding="utf-8")
print(json.dumps(layout, indent=2, sort_keys=True))
