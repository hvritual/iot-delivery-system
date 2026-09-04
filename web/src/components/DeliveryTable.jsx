import { gateLabel, priorityLabel, statusLabel } from "../lib/presentation.mjs";

export function DeliveryTable({ items, selectedId, onSelectItem }) {
  if (items.length === 0) {
    return <div className="empty-state">此板块暂时没有交付事项。点击右上角“新建交付事项”开始记录。</div>;
  }
  return (
    <div className="table-shell">
      <table className="delivery-table">
        <thead>
          <tr>
            <th>交付事项</th>
            <th>类型 / 项目</th>
            <th>负责人</th>
            <th>优先级</th>
            <th>进度</th>
            <th>当前关卡</th>
            <th>状态</th>
            <th>到期日</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr
              className={selectedId === item.id ? "selected" : ""}
              key={item.id}
              onClick={() => onSelectItem(item.id)}
              tabIndex="0"
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") onSelectItem(item.id);
              }}
            >
              <td>
                <div className="item-title-cell">
                  <strong>{item.title}</strong>
                  <span>{item.id}{item.isSample ? " · 示例" : ""}</span>
                </div>
              </td>
              <td><div className="item-title-cell"><strong>{item.kind || "task"}</strong><span>{item.projectId || "未归属"}</span></div></td>
              <td>{item.owner}</td>
              <td><span className={`priority ${String(item.priority).toLowerCase()}`}>{priorityLabel(item.priority)}</span></td>
              <td><span className="progress-cell">{Math.round(item.progressPercent ?? (item.status === "released" || item.status === "closed" ? 100 : 0))}%</span></td>
              <td>{gateLabel(item.gate)}</td>
              <td><span className={`status-badge ${item.status}`}>{statusLabel(item.status)}</span></td>
              <td>{item.dueDate || "未设定"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
