export const workItemKinds = [
  { value: "epic", label: "Epic" },
  { value: "task", label: "任务" },
  { value: "subtask", label: "子任务" },
  { value: "defect", label: "缺陷" },
];

export function filterDeliveryItems(items, filter = {}) {
  const query = normalize(filter.query);
  return items.filter((item) => {
    if (filter.projectId && item.projectId !== filter.projectId) return false;
    if (filter.owner && item.owner !== filter.owner) return false;
    if (filter.status && item.status !== filter.status) return false;
    if (filter.kind && item.kind !== filter.kind) return false;
    if (filter.releaseId && item.releaseId !== filter.releaseId) return false;
    if (filter.sprintId && item.sprintId !== filter.sprintId) return false;
    if (filter.milestoneId && item.milestoneId !== filter.milestoneId) return false;
    if (!query) return true;
    return [item.title, item.owner, item.plan, item.solution, item.blocker, ...(item.traceLinks ?? []).flatMap((link) => [link.reference, link.title]), ...(item.iotBindings ?? []).flatMap((binding) => [binding.reference, binding.label])]
      .some((value) => normalize(value).includes(query));
  });
}

export function parseIoTBindings(value) {
  return parseLines(value, 3).map(([kind, reference, label]) => ({ kind, reference, ...(label ? { label } : {}) }));
}

export function parseTraceLinks(value) {
  return parseLines(value, 5).map(([kind, reference, title, url, status]) => ({
    kind,
    reference,
    ...(title ? { title } : {}),
    ...(url ? { url } : {}),
    ...(status ? { status } : {}),
  }));
}

export function stringifyIoTBindings(bindings = []) {
  return bindings.map((binding) => [binding.kind, binding.reference, binding.label ?? ""].join(" | ")).join("\n");
}

export function stringifyTraceLinks(links = []) {
  return links.map((link) => [link.kind, link.reference, link.title ?? "", link.url ?? "", link.status ?? ""].join(" | ")).join("\n");
}

export function projectProgressLabel(progress) {
  if (!progress || !Number.isFinite(Number(progress.progressPercent))) return "暂无项目进度";
  const percentage = Math.round(Number(progress.progressPercent));
  return `${percentage}% · ${progress.completedItems ?? 0}/${progress.totalItems ?? 0} 项完成`;
}

function parseLines(value, maxParts) {
  return String(value ?? "")
    .split("\n")
    .map((line) => line.split("|").slice(0, maxParts).map((part) => part.trim()))
    .filter((parts) => parts[0] && parts[1]);
}

function normalize(value) {
  return String(value ?? "").trim().toLocaleLowerCase();
}
