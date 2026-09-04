package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"yunka.io/framework/execution"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS iotd_notification_deliveries (
    delivery_id TEXT NOT NULL,
    channel TEXT NOT NULL,
    payload TEXT NOT NULL,
    delivered_at TEXT NOT NULL,
    PRIMARY KEY (delivery_id, channel)
);
CREATE INDEX IF NOT EXISTS idx_iotd_notification_deliveries_list
    ON iotd_notification_deliveries (delivered_at DESC, delivery_id DESC);`

// SQLiteStore persists local channel deliveries independently of the outbox.
// The composite primary key makes at-least-once delivery safe per event and
// channel without suppressing a future delivery to a different channel.
type SQLiteStore struct {
	database *sql.DB
}

var _ InboxStore = (*SQLiteStore)(nil)

func NewSQLiteStore(database *sql.DB) (*SQLiteStore, error) {
	if database == nil {
		return nil, errors.New("SQLite notification database is required")
	}
	if _, err := database.Exec(sqliteSchema); err != nil {
		return nil, fmt.Errorf("initialize SQLite notification schema: %w", err)
	}
	return &SQLiteStore{database: database}, nil
}

func (store *SQLiteStore) Save(ctx context.Context, value Notification) error {
	if store == nil || store.database == nil {
		return errors.New("SQLite notification store is not configured")
	}
	value, err := normalize(value)
	if err != nil {
		return err
	}
	value.Channel = normalizeChannelName(value.Channel)
	if value.Channel == "" {
		return errors.New("notification channel is required")
	}
	if value.DeliveredAt.IsZero() {
		value.DeliveredAt = time.Now().UTC()
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode notification: %w", err)
	}
	executor, err := store.executor(ctx)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `
INSERT OR IGNORE INTO iotd_notification_deliveries (delivery_id, channel, payload, delivered_at)
VALUES (?, ?, ?, ?)`, value.DeliveryID, value.Channel, string(payload), value.DeliveredAt.UTC().Format(timeFormat))
	if err != nil {
		return fmt.Errorf("save notification: %w", err)
	}
	return nil
}

func (store *SQLiteStore) List(ctx context.Context, limit int) ([]Notification, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("SQLite notification store is not configured")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	executor, err := store.executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := executor.QueryContext(ctx, `
SELECT payload
FROM iotd_notification_deliveries
ORDER BY delivered_at DESC, delivery_id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	values := make([]Notification, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		var value Notification
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, fmt.Errorf("decode notification: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}
	return values, nil
}

type sqliteExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (store *SQLiteStore) executor(ctx context.Context) (sqliteExecutor, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("SQLite notification store is not configured")
	}
	if _, active := execution.Current(ctx); !active {
		return store.database, nil
	}
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("get SQLite transaction handle: %w", err)
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, errors.New("notification execution uses a non-SQLite transaction handle")
	}
	return transaction, nil
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

// LocalInboxChannel is the non-network MVP channel. It is durable and makes
// notification delivery observable before external adapters are enabled.
type LocalInboxChannel struct {
	store InboxStore
}

func NewLocalInboxChannel(store InboxStore) *LocalInboxChannel {
	return &LocalInboxChannel{store: store}
}

func (*LocalInboxChannel) Name() string {
	return LocalInboxChannelName
}

func (channel *LocalInboxChannel) Deliver(ctx context.Context, value Notification) error {
	if channel == nil || channel.store == nil {
		return errors.New("local notification inbox is not configured")
	}
	return channel.store.Save(ctx, value)
}
