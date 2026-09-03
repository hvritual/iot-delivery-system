package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"yunka.io/framework/execution"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS iotd_delivery_items (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS iotd_delivery_projects (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS iotd_delivery_releases (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS iotd_delivery_sprints (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS iotd_delivery_milestones (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS iotd_delivery_saved_views (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`

type SQLiteRepository struct {
	database *sql.DB
}

func NewSQLiteRepository(databasePath string) (*SQLiteRepository, error) {
	databasePath = strings.TrimSpace(databasePath)
	if databasePath == "" {
		return nil, errors.New("SQLite database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(databasePath)), 0o755); err != nil {
		return nil, fmt.Errorf("create SQLite directory: %w", err)
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	// The local runtime uses one SQLite file for command transactions, the
	// asynchronous outbox dispatcher, and the durable local inbox. Restricting
	// this local pool to one connection serializes in-process writers; WAL and a
	// bounded busy wait keep reads responsive and guard the transition between
	// transactions instead of surfacing SQLITE_BUSY to an API caller.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	for _, statement := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := database.Exec(statement); err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("configure SQLite connection: %w", err)
		}
	}
	if _, err := database.Exec(sqliteSchema); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize SQLite schema: %w", err)
	}
	return &SQLiteRepository{database: database}, nil
}

func (repository *SQLiteRepository) Close() error {
	if repository == nil || repository.database == nil {
		return nil
	}
	return repository.database.Close()
}

func (repository *SQLiteRepository) Database() *sql.DB {
	if repository == nil {
		return nil
	}
	return repository.database
}

func (repository *SQLiteRepository) Create(ctx context.Context, item WorkItem) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode delivery item: %w", err)
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO iotd_delivery_items (id, payload, updated_at) VALUES (?, ?, ?)`, item.ID, string(payload), item.UpdatedAt.UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert delivery item: %w", err)
	}
	return nil
}

func (repository *SQLiteRepository) Get(ctx context.Context, id string) (WorkItem, error) {
	executor, err := repository.executor(ctx)
	if err != nil {
		return WorkItem{}, err
	}
	var payload string
	err = executor.QueryRowContext(ctx, `SELECT payload FROM iotd_delivery_items WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkItem{}, ErrNotFound
	}
	if err != nil {
		return WorkItem{}, fmt.Errorf("read delivery item: %w", err)
	}
	return decodeWorkItem(payload)
}

func (repository *SQLiteRepository) List(ctx context.Context) ([]WorkItem, error) {
	executor, err := repository.executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := executor.QueryContext(ctx, `SELECT payload FROM iotd_delivery_items ORDER BY updated_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list delivery items: %w", err)
	}
	defer rows.Close()
	items := make([]WorkItem, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan delivery item: %w", err)
		}
		item, err := decodeWorkItem(payload)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery items: %w", err)
	}
	return items, nil
}

func (repository *SQLiteRepository) Save(ctx context.Context, item WorkItem) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode delivery item: %w", err)
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return err
	}
	result, err := executor.ExecContext(ctx, `UPDATE iotd_delivery_items SET payload = ?, updated_at = ? WHERE id = ?`, string(payload), item.UpdatedAt.UTC().Format(timeLayout), item.ID)
	if err != nil {
		return fmt.Errorf("update delivery item: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check updated delivery item: %w", err)
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func (repository *SQLiteRepository) CreateProject(ctx context.Context, project Project) error {
	payload, err := json.Marshal(project)
	if err != nil {
		return fmt.Errorf("encode delivery project: %w", err)
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO iotd_delivery_projects (id, payload, updated_at) VALUES (?, ?, ?)`, project.ID, string(payload), project.UpdatedAt.UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert delivery project: %w", err)
	}
	return nil
}

func (repository *SQLiteRepository) GetProject(ctx context.Context, id string) (Project, error) {
	executor, err := repository.executor(ctx)
	if err != nil {
		return Project{}, err
	}
	var payload string
	err = executor.QueryRowContext(ctx, `SELECT payload FROM iotd_delivery_projects WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("read delivery project: %w", err)
	}
	return decodeProject(payload)
}

func (repository *SQLiteRepository) ListProjects(ctx context.Context) ([]Project, error) {
	executor, err := repository.executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := executor.QueryContext(ctx, `SELECT payload FROM iotd_delivery_projects ORDER BY updated_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list delivery projects: %w", err)
	}
	defer rows.Close()
	projects := make([]Project, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan delivery project: %w", err)
		}
		project, err := decodeProject(payload)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery projects: %w", err)
	}
	return projects, nil
}

func (repository *SQLiteRepository) SaveProject(ctx context.Context, project Project) error {
	payload, err := json.Marshal(project)
	if err != nil {
		return fmt.Errorf("encode delivery project: %w", err)
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return err
	}
	result, err := executor.ExecContext(ctx, `UPDATE iotd_delivery_projects SET payload = ?, updated_at = ? WHERE id = ?`, string(payload), project.UpdatedAt.UTC().Format(timeLayout), project.ID)
	if err != nil {
		return fmt.Errorf("update delivery project: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check updated delivery project: %w", err)
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func (repository *SQLiteRepository) CreateRelease(ctx context.Context, release Release) error {
	return repository.createPayload(ctx, "iotd_delivery_releases", release.ID, release.UpdatedAt, release, "release")
}

func (repository *SQLiteRepository) GetRelease(ctx context.Context, id string) (Release, error) {
	payload, err := repository.getPayload(ctx, "iotd_delivery_releases", id, "release")
	if err != nil {
		return Release{}, err
	}
	return decodeRelease(payload)
}

func (repository *SQLiteRepository) ListReleases(ctx context.Context) ([]Release, error) {
	payloads, err := repository.listPayloads(ctx, "iotd_delivery_releases", "releases")
	if err != nil {
		return nil, err
	}
	releases := make([]Release, 0, len(payloads))
	for _, payload := range payloads {
		release, err := decodeRelease(payload)
		if err != nil {
			return nil, err
		}
		releases = append(releases, release)
	}
	return releases, nil
}

func (repository *SQLiteRepository) SaveRelease(ctx context.Context, release Release) error {
	return repository.savePayload(ctx, "iotd_delivery_releases", release.ID, release.UpdatedAt, release, "release")
}

func (repository *SQLiteRepository) CreateSprint(ctx context.Context, sprint Sprint) error {
	return repository.createPayload(ctx, "iotd_delivery_sprints", sprint.ID, sprint.UpdatedAt, sprint, "sprint")
}

func (repository *SQLiteRepository) GetSprint(ctx context.Context, id string) (Sprint, error) {
	payload, err := repository.getPayload(ctx, "iotd_delivery_sprints", id, "sprint")
	if err != nil {
		return Sprint{}, err
	}
	return decodeSprint(payload)
}

func (repository *SQLiteRepository) ListSprints(ctx context.Context) ([]Sprint, error) {
	payloads, err := repository.listPayloads(ctx, "iotd_delivery_sprints", "sprints")
	if err != nil {
		return nil, err
	}
	sprints := make([]Sprint, 0, len(payloads))
	for _, payload := range payloads {
		sprint, err := decodeSprint(payload)
		if err != nil {
			return nil, err
		}
		sprints = append(sprints, sprint)
	}
	return sprints, nil
}

func (repository *SQLiteRepository) SaveSprint(ctx context.Context, sprint Sprint) error {
	return repository.savePayload(ctx, "iotd_delivery_sprints", sprint.ID, sprint.UpdatedAt, sprint, "sprint")
}

func (repository *SQLiteRepository) CreateMilestone(ctx context.Context, milestone Milestone) error {
	return repository.createPayload(ctx, "iotd_delivery_milestones", milestone.ID, milestone.UpdatedAt, milestone, "milestone")
}

func (repository *SQLiteRepository) GetMilestone(ctx context.Context, id string) (Milestone, error) {
	payload, err := repository.getPayload(ctx, "iotd_delivery_milestones", id, "milestone")
	if err != nil {
		return Milestone{}, err
	}
	return decodeMilestone(payload)
}

func (repository *SQLiteRepository) ListMilestones(ctx context.Context) ([]Milestone, error) {
	payloads, err := repository.listPayloads(ctx, "iotd_delivery_milestones", "milestones")
	if err != nil {
		return nil, err
	}
	milestones := make([]Milestone, 0, len(payloads))
	for _, payload := range payloads {
		milestone, err := decodeMilestone(payload)
		if err != nil {
			return nil, err
		}
		milestones = append(milestones, milestone)
	}
	return milestones, nil
}

func (repository *SQLiteRepository) SaveMilestone(ctx context.Context, milestone Milestone) error {
	return repository.savePayload(ctx, "iotd_delivery_milestones", milestone.ID, milestone.UpdatedAt, milestone, "milestone")
}

func (repository *SQLiteRepository) CreateSavedView(ctx context.Context, view SavedView) error {
	return repository.createPayload(ctx, "iotd_delivery_saved_views", view.ID, view.UpdatedAt, view, "saved view")
}

func (repository *SQLiteRepository) ListSavedViews(ctx context.Context, owner string) ([]SavedView, error) {
	payloads, err := repository.listPayloads(ctx, "iotd_delivery_saved_views", "saved views")
	if err != nil {
		return nil, err
	}
	owner = strings.TrimSpace(owner)
	views := make([]SavedView, 0, len(payloads))
	for _, payload := range payloads {
		view, err := decodeSavedView(payload)
		if err != nil {
			return nil, err
		}
		if owner == "" || view.Owner == owner {
			views = append(views, view)
		}
	}
	return views, nil
}

func (repository *SQLiteRepository) createPayload(ctx context.Context, table, id string, updatedAt time.Time, value any, label string) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode delivery %s: %w", label, err)
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO `+table+` (id, payload, updated_at) VALUES (?, ?, ?)`, id, string(payload), updatedAt.UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert delivery %s: %w", label, err)
	}
	return nil
}

func (repository *SQLiteRepository) getPayload(ctx context.Context, table, id, label string) (string, error) {
	executor, err := repository.executor(ctx)
	if err != nil {
		return "", err
	}
	var payload string
	err = executor.QueryRowContext(ctx, `SELECT payload FROM `+table+` WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read delivery %s: %w", label, err)
	}
	return payload, nil
}

func (repository *SQLiteRepository) listPayloads(ctx context.Context, table, label string) ([]string, error) {
	executor, err := repository.executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := executor.QueryContext(ctx, `SELECT payload FROM `+table+` ORDER BY updated_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list delivery %s: %w", label, err)
	}
	defer rows.Close()
	payloads := make([]string, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan delivery %s: %w", label, err)
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery %s: %w", label, err)
	}
	return payloads, nil
}

func (repository *SQLiteRepository) savePayload(ctx context.Context, table, id string, updatedAt time.Time, value any, label string) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode delivery %s: %w", label, err)
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return err
	}
	result, err := executor.ExecContext(ctx, `UPDATE `+table+` SET payload = ?, updated_at = ? WHERE id = ?`, string(payload), updatedAt.UTC().Format(timeLayout), id)
	if err != nil {
		return fmt.Errorf("update delivery %s: %w", label, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check updated delivery %s: %w", label, err)
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func (repository *SQLiteRepository) Ping(ctx context.Context) error {
	if repository == nil || repository.database == nil {
		return errors.New("SQLite repository is not configured")
	}
	return repository.database.PingContext(ctx)
}

type sqliteExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (repository *SQLiteRepository) executor(ctx context.Context) (sqliteExecutor, error) {
	if repository == nil || repository.database == nil {
		return nil, errors.New("SQLite repository is not configured")
	}
	if _, active := execution.Current(ctx); !active {
		return repository.database, nil
	}
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("get SQLite transaction handle: %w", err)
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, errors.New("delivery execution uses a non-SQLite transaction handle")
	}
	return transaction, nil
}

func decodeWorkItem(payload string) (WorkItem, error) {
	var item WorkItem
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		return WorkItem{}, fmt.Errorf("decode delivery item: %w", err)
	}
	return item, nil
}

func decodeProject(payload string) (Project, error) {
	var project Project
	if err := json.Unmarshal([]byte(payload), &project); err != nil {
		return Project{}, fmt.Errorf("decode delivery project: %w", err)
	}
	return project, nil
}

func decodeRelease(payload string) (Release, error) {
	var release Release
	if err := json.Unmarshal([]byte(payload), &release); err != nil {
		return Release{}, fmt.Errorf("decode delivery release: %w", err)
	}
	return release, nil
}

func decodeSprint(payload string) (Sprint, error) {
	var sprint Sprint
	if err := json.Unmarshal([]byte(payload), &sprint); err != nil {
		return Sprint{}, fmt.Errorf("decode delivery sprint: %w", err)
	}
	return sprint, nil
}

func decodeMilestone(payload string) (Milestone, error) {
	var milestone Milestone
	if err := json.Unmarshal([]byte(payload), &milestone); err != nil {
		return Milestone{}, fmt.Errorf("decode delivery milestone: %w", err)
	}
	return milestone, nil
}

func decodeSavedView(payload string) (SavedView, error) {
	var view SavedView
	if err := json.Unmarshal([]byte(payload), &view); err != nil {
		return SavedView{}, fmt.Errorf("decode delivery saved view: %w", err)
	}
	return view, nil
}

const timeLayout = "2006-01-02T15:04:05.999999999Z07:00"
