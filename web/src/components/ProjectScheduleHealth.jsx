export function ProjectScheduleHealth({ schedule }) {
  if (!schedule) {
    return (
      <section className="project-schedule-health empty-project-schedule" aria-label="项目交付健康">
        <span className="eyebrow">项目交付健康</span>
        <p>选择项目后查看排期、依赖与成员负载。</p>
      </section>
    );
  }

  const totalItems = Number(schedule.totalItems ?? 0);
  const scheduledItems = Number(schedule.scheduledItems ?? 0);
  const risks = Array.isArray(schedule.risks) ? schedule.risks.slice(0, 3) : [];
  const capacity = Array.isArray(schedule.capacity) ? schedule.capacity.slice(0, 4) : [];

  return (
    <section className="project-schedule-health" aria-label="项目交付健康">
      <div className="project-schedule-heading">
        <div><span className="eyebrow">项目交付健康</span><h3>{scheduledItems}/{totalItems} 项已排期</h3></div>
        <small>截至 {schedule.asOfDate || "当前"}</small>
      </div>
      <div className="project-schedule-signals">
        <span>逾期 {Number(schedule.overdueItems ?? 0)}</span>
        <span>依赖阻塞 {Number(schedule.dependencyBlockedItems ?? 0)}</span>
        <span>受阻 {Number(schedule.blockedItems ?? 0)}</span>
        <span>未排期 {Number(schedule.unscheduledItems ?? 0)}</span>
      </div>
      {capacity.length ? <div className="project-capacity-list" aria-label="成员剩余负载">
        {capacity.map((owner) => <span key={owner.owner}>{owner.owner} · 剩余 {formatPoints(owner.remainingEstimatePoints)} pts{owner.blockedItems ? ` · 受阻 ${owner.blockedItems}` : ""}</span>)}
      </div> : null}
      {risks.length ? <ul className="project-schedule-risks" aria-label="排期风险">
        {risks.map((risk) => <li key={`${risk.itemId}-${risk.reason}`}><strong>{risk.title}</strong><span>{risk.reason}{risk.dueDate ? ` · 截止 ${risk.dueDate}` : ""}</span></li>)}
      </ul> : null}
    </section>
  );
}

function formatPoints(value) {
  const points = Number(value ?? 0);
  return Number.isInteger(points) ? points : points.toFixed(1);
}
