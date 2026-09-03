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

	_ "modernc.org/sqlite"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS delivery_items (
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

func (repository *SQLiteRepository) Create(ctx context.Context, item WorkItem) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode delivery item: %w", err)
	}
	_, err = repository.database.ExecContext(ctx, `INSERT INTO delivery_items (id, payload, updated_at) VALUES (?, ?, ?)`, item.ID, string(payload), item.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	if err != nil {
		return fmt.Errorf("insert delivery item: %w", err)
	}
	return nil
}

func (repository *SQLiteRepository) Get(ctx context.Context, id string) (WorkItem, error) {
	var payload string
	err := repository.database.QueryRowContext(ctx, `SELECT payload FROM delivery_items WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkItem{}, ErrNotFound
	}
	if err != nil {
		return WorkItem{}, fmt.Errorf("read delivery item: %w", err)
	}
	return decodeWorkItem(payload)
}

func (repository *SQLiteRepository) List(ctx context.Context) ([]WorkItem, error) {
	rows, err := repository.database.QueryContext(ctx, `SELECT payload FROM delivery_items ORDER BY updated_at DESC, id ASC`)
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
	result, err := repository.database.ExecContext(ctx, `UPDATE delivery_items SET payload = ?, updated_at = ? WHERE id = ?`, string(payload), item.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), item.ID)
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

func decodeWorkItem(payload string) (WorkItem, error) {
	var item WorkItem
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		return WorkItem{}, fmt.Errorf("decode delivery item: %w", err)
	}
	return item, nil
}
