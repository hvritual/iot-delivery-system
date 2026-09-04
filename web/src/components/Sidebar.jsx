const boardIcons = {
  "设备质量与连接": "◌",
  "产品与平台能力": "◇",
  "研发交付效能": "▣",
  "运营保障与安全": "◇",
  "客户与业务价值": "◒",
};

export function Sidebar({ boards, activeBoard, onSelectBoard, sampleVisible }) {
  return (
    <aside className="sidebar">
      <div className="brand">
        <div className="brand-mark">I</div>
        <div>
          <strong>IoT Delivery</strong>
          <span>研发交付系统</span>
        </div>
      </div>

      <nav className="nav-list" aria-label="交付板块导航">
        <button
          className={`nav-item ${activeBoard === null ? "active" : ""}`}
          onClick={() => onSelectBoard(null)}
          type="button"
        >
          <span className="nav-icon">▦</span>
          <span>交付驾驶舱</span>
        </button>
        <div className="nav-caption">五个经营板块</div>
        {boards.map((board) => (
          <button
            className={`nav-item board-nav ${activeBoard === board.board ? "active" : ""}`}
            key={board.board}
            onClick={() => onSelectBoard(board.board)}
            type="button"
          >
            <span className="nav-icon">{boardIcons[board.board]}</span>
            <span>{board.board}</span>
            <span className="nav-count">{board.total}</span>
          </button>
        ))}
      </nav>

      <div className="sidebar-footer">
        <div className="projection-state">
          <span className="projection-dot" />
          <div>
            <strong>Obsidian 单向沉淀</strong>
            <span>任务变更会自动生成可追溯笔记</span>
          </div>
        </div>
        {sampleVisible ? <p className="sample-note">当前显示本地示例数据</p> : null}
      </div>
    </aside>
  );
}
