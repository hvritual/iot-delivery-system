// Package localoutbox provides the native database/sql SQLite part of the
// transactional-outbox seam. Delivery and dispatch policy remain outside this
// adapter.
package localoutbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"yunka.io/framework/event"
	"yunka.io/framework/event/outbox"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS iotd_outbox (
    id TEXT PRIMARY KEY,
    envelope_json TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    next_attempt_at TEXT NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    published_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_iotd_outbox_claim
    ON iotd_outbox (status, next_attempt_at, lease_until, created_at);`

type SQLiteStore struct {
	database *sql.DB
}

var _ outbox.Store = (*SQLiteStore)(nil)

func NewSQLiteStore(database *sql.DB) (*SQLiteStore, error) {
	if database == nil {
		return nil, errors.New("SQLite outbox database is required")
	}
	if _, err := database.Exec(sqliteSchema); err != nil {
		return nil, fmt.Errorf("initialize SQLite outbox schema: %w", err)
	}
	return &SQLiteStore{database: database}, nil
}

func (store *SQLiteStore) Enqueue(ctx context.Context, envelope event.Envelope) error {
	if store == nil || store.database == nil {
		return errors.New("SQLite outbox store is not configured")
	}
	return store.insert(ctx, store.database, envelope)
}

func (store *SQLiteStore) EnqueueTx(ctx context.Context, transaction any, envelope event.Envelope) error {
	if store == nil || store.database == nil {
		return errors.New("SQLite outbox store is not configured")
	}
	tx, ok := transaction.(*sql.Tx)
	if !ok || tx == nil {
		return outbox.ErrInvalidTx
	}
	return store.insert(ctx, tx, envelope)
}

func (store *SQLiteStore) Snapshot(ctx context.Context) (outbox.Snapshot, error) {
	if store == nil || store.database == nil {
		return outbox.Snapshot{}, errors.New("SQLite outbox store is not configured")
	}
	rows, err := store.database.QueryContext(ctx, `SELECT status, COUNT(*) FROM iotd_outbox GROUP BY status`)
	if err != nil {
		return outbox.Snapshot{}, fmt.Errorf("count SQLite outbox records: %w", err)
	}
	defer rows.Close()
	var snapshot outbox.Snapshot
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return outbox.Snapshot{}, fmt.Errorf("scan SQLite outbox status: %w", err)
		}
		switch outbox.Status(status) {
		case outbox.StatusPending:
			snapshot.Pending = count
		case outbox.StatusInFlight:
			snapshot.InFlight = count
		case outbox.StatusPublished:
			snapshot.Published = count
		case outbox.StatusDeadLetter:
			snapshot.DeadLetter = count
		}
	}
	if err := rows.Err(); err != nil {
		return outbox.Snapshot{}, fmt.Errorf("iterate SQLite outbox status: %w", err)
	}
	var oldest string
	err = store.database.QueryRowContext(ctx, `SELECT created_at FROM iotd_outbox WHERE status = ? ORDER BY created_at ASC, id ASC LIMIT 1`, outbox.StatusPending).Scan(&oldest)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return outbox.Snapshot{}, fmt.Errorf("read oldest SQLite outbox record: %w", err)
	}
	snapshot.OldestPendingAt, err = parseTime(oldest)
	if err != nil {
		return outbox.Snapshot{}, fmt.Errorf("decode oldest SQLite outbox record: %w", err)
	}
	return snapshot, nil
}

func (store *SQLiteStore) Claim(ctx context.Context, options outbox.ClaimOptions) ([]outbox.Record, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("SQLite outbox store is not configured")
	}
	owner, limit, lease, now, err := normalizeClaimOptions(options)
	if err != nil {
		return nil, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin SQLite outbox claim: %w", err)
	}
	rollback := func(cause error) ([]outbox.Record, error) {
		return nil, errors.Join(cause, transaction.Rollback())
	}

	rows, err := transaction.QueryContext(ctx, `
SELECT id, envelope_json, status, attempts, next_attempt_at, lease_owner,
       lease_until, last_error, created_at, updated_at, published_at
FROM iotd_outbox
WHERE (status = ? AND next_attempt_at <= ?)
   OR (status = ? AND lease_until <= ?)
ORDER BY created_at ASC, id ASC
LIMIT ?`,
		string(outbox.StatusPending), formatTime(now), string(outbox.StatusInFlight), formatTime(now), limit)
	if err != nil {
		return rollback(fmt.Errorf("select SQLite outbox records to claim: %w", err))
	}
	candidates := make([]outbox.Record, 0, limit)
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			_ = rows.Close()
			return rollback(scanErr)
		}
		candidates = append(candidates, record)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return rollback(fmt.Errorf("iterate SQLite outbox records to claim: %w", err))
	}
	if err := rows.Close(); err != nil {
		return rollback(fmt.Errorf("close SQLite outbox claim rows: %w", err))
	}

	claimed := make([]outbox.Record, 0, len(candidates))
	leaseUntil := now.Add(lease)
	for _, record := range candidates {
		result, updateErr := transaction.ExecContext(ctx, `
UPDATE iotd_outbox
SET status = ?, attempts = attempts + 1, lease_owner = ?, lease_until = ?, updated_at = ?
WHERE id = ? AND (
    (status = ? AND next_attempt_at <= ?)
    OR (status = ? AND lease_until <= ?)
)`,
			string(outbox.StatusInFlight), owner, formatTime(leaseUntil), formatTime(now), record.ID,
			string(outbox.StatusPending), formatTime(now), string(outbox.StatusInFlight), formatTime(now))
		if updateErr != nil {
			return rollback(fmt.Errorf("claim SQLite outbox record %s: %w", record.ID, updateErr))
		}
		changed, updateErr := result.RowsAffected()
		if updateErr != nil {
			return rollback(fmt.Errorf("check SQLite outbox claim %s: %w", record.ID, updateErr))
		}
		if changed == 0 {
			continue
		}
		record.Status = outbox.StatusInFlight
		record.Attempts++
		record.LeaseOwner = owner
		record.LeaseUntil = leaseUntil
		record.UpdatedAt = now
		claimed = append(claimed, record)
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit SQLite outbox claim: %w", err)
	}
	return claimed, nil
}

func (store *SQLiteStore) MarkPublished(ctx context.Context, id, owner string, at time.Time) error {
	return store.finishLease(ctx, id, owner, `
UPDATE iotd_outbox
SET status = ?, lease_owner = '', lease_until = '', last_error = '', published_at = ?, updated_at = ?
WHERE id = ? AND status = ? AND lease_owner = ?`,
		string(outbox.StatusPublished), formatTime(nonZeroUTC(at)), formatTime(nonZeroUTC(at)), id, string(outbox.StatusInFlight), owner)
}

func (store *SQLiteStore) Retry(ctx context.Context, id, owner string, next time.Time, cause error) error {
	if cause == nil {
		cause = errors.New("outbox publish failed")
	}
	next = nonZeroUTC(next)
	return store.finishLease(ctx, id, owner, `
UPDATE iotd_outbox
SET status = ?, lease_owner = '', lease_until = '', last_error = ?, next_attempt_at = ?, updated_at = ?
WHERE id = ? AND status = ? AND lease_owner = ?`,
		string(outbox.StatusPending), cause.Error(), formatTime(next), formatTime(time.Now().UTC()), id, string(outbox.StatusInFlight), owner)
}

func (store *SQLiteStore) DeadLetter(ctx context.Context, id, owner string, cause error) error {
	if cause == nil {
		cause = errors.New("outbox publish failed")
	}
	return store.finishLease(ctx, id, owner, `
UPDATE iotd_outbox
SET status = ?, lease_owner = '', lease_until = '', last_error = ?, updated_at = ?
WHERE id = ? AND status = ? AND lease_owner = ?`,
		string(outbox.StatusDeadLetter), cause.Error(), formatTime(time.Now().UTC()), id, string(outbox.StatusInFlight), owner)
}

func (store *SQLiteStore) finishLease(ctx context.Context, id, owner, statement string, values ...any) error {
	if store == nil || store.database == nil {
		return errors.New("SQLite outbox store is not configured")
	}
	id = strings.TrimSpace(id)
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return outbox.ErrInvalidOwner
	}
	if id == "" {
		return outbox.ErrNotFound
	}
	result, err := store.database.ExecContext(ctx, statement, values...)
	if err != nil {
		return fmt.Errorf("finalize SQLite outbox record %s: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check SQLite outbox finalization %s: %w", id, err)
	}
	if changed == 0 {
		return fmt.Errorf("%w: %s", outbox.ErrLeaseLost, id)
	}
	return nil
}

func normalizeClaimOptions(options outbox.ClaimOptions) (string, int, time.Duration, time.Time, error) {
	owner := strings.TrimSpace(options.Owner)
	if owner == "" {
		return "", 0, 0, time.Time{}, outbox.ErrInvalidOwner
	}
	if options.Limit <= 0 {
		options.Limit = 50
	}
	if options.Lease <= 0 {
		options.Lease = 30 * time.Second
	}
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	} else {
		options.Now = options.Now.UTC()
	}
	return owner, options.Limit, options.Lease, options.Now, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanRecord(scanner rowScanner) (outbox.Record, error) {
	var record outbox.Record
	var envelopeJSON string
	var status string
	var nextAttemptAt, leaseUntil, createdAt, updatedAt, publishedAt string
	if err := scanner.Scan(
		&record.ID,
		&envelopeJSON,
		&status,
		&record.Attempts,
		&nextAttemptAt,
		&record.LeaseOwner,
		&leaseUntil,
		&record.LastError,
		&createdAt,
		&updatedAt,
		&publishedAt,
	); err != nil {
		return outbox.Record{}, fmt.Errorf("scan SQLite outbox record: %w", err)
	}
	if err := json.Unmarshal([]byte(envelopeJSON), &record.Envelope); err != nil {
		return outbox.Record{}, fmt.Errorf("decode SQLite outbox envelope: %w", err)
	}
	var err error
	if record.Envelope, err = record.Envelope.Normalize(); err != nil {
		return outbox.Record{}, fmt.Errorf("validate SQLite outbox envelope: %w", err)
	}
	record.Status = outbox.Status(status)
	if record.NextAttemptAt, err = parseTime(nextAttemptAt); err != nil {
		return outbox.Record{}, fmt.Errorf("decode SQLite outbox next attempt: %w", err)
	}
	if record.LeaseUntil, err = parseOptionalTime(leaseUntil); err != nil {
		return outbox.Record{}, fmt.Errorf("decode SQLite outbox lease: %w", err)
	}
	if record.CreatedAt, err = parseTime(createdAt); err != nil {
		return outbox.Record{}, fmt.Errorf("decode SQLite outbox created time: %w", err)
	}
	if record.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return outbox.Record{}, fmt.Errorf("decode SQLite outbox updated time: %w", err)
	}
	if record.PublishedAt, err = parseOptionalTime(publishedAt); err != nil {
		return outbox.Record{}, fmt.Errorf("decode SQLite outbox published time: %w", err)
	}
	return record, nil
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (store *SQLiteStore) insert(ctx context.Context, executor sqlExecutor, envelope event.Envelope) error {
	normalized, err := envelope.Normalize()
	if err != nil {
		return fmt.Errorf("normalize outbox event: %w", err)
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("encode outbox event: %w", err)
	}
	now := normalized.OccurredAt.UTC()
	_, err = executor.ExecContext(ctx, `
INSERT INTO iotd_outbox (
    id, envelope_json, status, attempts, next_attempt_at, lease_owner,
    lease_until, last_error, created_at, updated_at, published_at
) VALUES (?, ?, ?, ?, ?, '', '', '', ?, ?, '')`,
		normalized.ID,
		string(payload),
		outbox.StatusPending,
		0,
		formatTime(now),
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("%w: %s", outbox.ErrDuplicate, normalized.ID)
		}
		return fmt.Errorf("insert SQLite outbox event: %w", err)
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func parseOptionalTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return parseTime(value)
}

func nonZeroUTC(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
