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
  return parseLines(value, 4).map(([kind, reference, label, encodedAttributes]) => ({
    kind,
    reference,
    ...(label ? { label } : {}),
    ...(encodedAttributes ? { attributes: parseAttributes(encodedAttributes) } : {}),
  }));
}

export function parseTraceLinks(value) {
  return parseLines(value, 6).map(([kind, reference, title, url, status, recordedAt]) => ({
    kind,
    reference,
    ...(title ? { title } : {}),
    ...(url ? { url } : {}),
    ...(status ? { status } : {}),
    ...(recordedAt ? { recordedAt } : {}),
  }));
}

export function stringifyIoTBindings(bindings = []) {
  return bindings.map((binding) => [binding.kind, binding.reference, binding.label ?? "", stringifyAttributes(binding.attributes)].join(" | ")).join("\n");
}

export function stringifyTraceLinks(links = []) {
  return links.map((link) => [link.kind, link.reference, link.title ?? "", link.url ?? "", link.status ?? "", link.recordedAt ?? ""].join(" | ")).join("\n");
}

function stringifyAttributes(attributes) {
  if (!attributes || typeof attributes !== "object" || Array.isArray(attributes) || Object.keys(attributes).length === 0) return "";
  const ordered = Object.fromEntries(Object.entries(attributes).sort(([left], [right]) => left.localeCompare(right)));
  return JSON.stringify(ordered).replaceAll("|", "\\u007c");
}

function parseAttributes(value) {
  let attributes;
  try {
    attributes = JSON.parse(value);
  } catch {
    throw new TypeError("IoT 绑定属性必须是 JSON 对象");
  }
  if (!attributes || typeof attributes !== "object" || Array.isArray(attributes) || Object.values(attributes).some((entry) => typeof entry !== "string")) {
    throw new TypeError("IoT 绑定属性必须是字符串到字符串的 JSON 对象");
  }
  return attributes;
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
