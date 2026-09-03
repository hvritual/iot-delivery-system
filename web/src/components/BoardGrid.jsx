import { statusLabel } from "../lib/presentation.mjs";

const boardTone = ["blue", "indigo", "teal", "amber", "violet"];

export function BoardGrid({ boards, activeBoard, onSelectBoard }) {
  return (
    <section className="board-grid" aria-label="五个交付板块概览">
      {boards.map((board, index) => {
        const focus = board.blocked > 0 ? `${board.blocked} 项受阻` : board.active > 0 ? `${board.active} 项推进中` : "当前无待办";
        return (
          <button
            className={`board-card ${boardTone[index]} ${activeBoard === board.board ? "selected" : ""}`}
            key={board.board}
            onClick={() => onSelectBoard(activeBoard === board.board ? null : board.board)}
            type="button"
          >
            <span className="board-card-label">{board.board}</span>
            <strong>{board.total}</strong>
            <span className="board-card-subtitle">{focus}</span>
            <span className="board-card-foot">查看事项 <span aria-hidden="true">→</span></span>
          </button>
        );
      })}
    </section>
  );
}

export function StatusLegend() {
  return (
    <div className="status-legend" aria-label="状态说明">
      {["planned", "in_progress", "blocked", "verifying", "released", "closed"].map((status) => (
        <span key={status} className={`status-dot ${status}`}>
          {statusLabel(status)}
        </span>
      ))}
    </div>
  );
}
