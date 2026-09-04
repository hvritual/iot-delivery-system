package configrevision

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hvritual/yunka.io/framework/execution"
)

const (
	DefaultHistoryLimit = 50
	MaxHistoryLimit     = 100
)

type Store interface {
	Append(context.Context, AppendInput) (ConfigRevision, error)
	ByRevision(context.Context, string, Kind, string, int64) (ConfigRevision, error)
	Latest(context.Context, string, Kind, string) (ConfigRevision, error)
	History(context.Context, string, Kind, string, int) ([]ConfigRevision, error)
}

type SQLiteStore struct {
	database *sql.DB
	clock    func() time.Time
}

var _ Store = (*SQLiteStore)(nil)
var ErrStoreBusy = errors.New("config revision store busy")

type SQLiteStoreOption func(*SQLiteStore) error

func WithClock(clock func() time.Time) SQLiteStoreOption {
	return func(store *SQLiteStore) error {
		if clock == nil {
			return errors.New("config revision clock is required")
		}
		store.clock = clock
		return nil
	}
}

func NewSQLiteStore(database *sql.DB, options ...SQLiteStoreOption) (*SQLiteStore, error) {
	if database == nil {
		return nil, errors.New("config revision SQLite database is required")
	}
	store := &SQLiteStore{database: database, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("config revision SQLite store option is required")
		}
		if err := option(store); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (store *SQLiteStore) Append(ctx context.Context, input AppendInput) (ConfigRevision, error) {
	if store == nil || store.clock == nil {
		return ConfigRevision{}, errors.New("config revision SQLite store is not configured")
	}
	if err := validateAppendInput(input); err != nil {
		return ConfigRevision{}, err
	}
	payload, payloadHash, err := normalizePayload(input.Payload)
	if err != nil {
		return ConfigRevision{}, err
	}
	createdAt := store.clock().UTC()
	if createdAt.IsZero() {
		return ConfigRevision{}, errors.New("config revision clock returned zero time")
	}
	executor, transactional, err := store.executor(ctx)
	if err != nil {
		return ConfigRevision{}, err
	}
	revision := input.ExpectedParentRevision + 1
	result, err := appendConditionally(ctx, executor, !transactional, `
INSERT INTO iotd_config_revisions (
    id, organization_id, kind, config_key, revision, parent_revision, payload, payload_hash, created_by_type, created_by_id, created_at
)
SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
WHERE
    (? = 0 AND NOT EXISTS (
        SELECT 1 FROM iotd_config_revisions WHERE organization_id = ? AND kind = ? AND config_key = ?
    ))
    OR
    (? > 0 AND EXISTS (
        SELECT 1 FROM iotd_config_revisions WHERE organization_id = ? AND kind = ? AND config_key = ? AND revision = ?
    ) AND NOT EXISTS (
        SELECT 1 FROM iotd_config_revisions WHERE organization_id = ? AND kind = ? AND config_key = ? AND revision > ?
    ))`,
		input.ID, input.OrganizationID, input.Kind, input.ConfigKey, revision, input.ExpectedParentRevision, payload, payloadHash[:], input.CreatedByType, input.CreatedByID, formatUTCTime(createdAt),
		input.ExpectedParentRevision, input.OrganizationID, input.Kind, input.ConfigKey,
		input.ExpectedParentRevision, input.OrganizationID, input.Kind, input.ConfigKey, input.ExpectedParentRevision,
		input.OrganizationID, input.Kind, input.ConfigKey, input.ExpectedParentRevision,
	)
	if err != nil {
		return ConfigRevision{}, fmt.Errorf("append config revision: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ConfigRevision{}, fmt.Errorf("read config revision append result: %w", err)
	}
	if rows != 1 {
		return ConfigRevision{}, ErrRevisionConflict
	}
	return ConfigRevision{
		ID: input.ID, OrganizationID: input.OrganizationID, Kind: input.Kind, ConfigKey: input.ConfigKey,
		Revision: revision, ParentRevision: input.ExpectedParentRevision, Payload: payload, PayloadHash: payloadHash,
		CreatedByType: input.CreatedByType, CreatedByID: input.CreatedByID, CreatedAt: createdAt,
	}, nil
}

func appendConditionally(ctx context.Context, executor sqliteExecutor, retryBusy bool, query string, arguments ...any) (sql.Result, error) {
	for range 64 {
		result, err := executor.ExecContext(ctx, query, arguments...)
		if err == nil || !isSQLiteBusy(err) {
			return result, err
		}
		if !retryBusy {
			return nil, ErrStoreBusy
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
	result, err := executor.ExecContext(ctx, query, arguments...)
	if err == nil {
		return result, nil
	}
	if isSQLiteBusy(err) {
		return nil, ErrStoreBusy
	}
	return nil, err
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database is locked")
}

func (store *SQLiteStore) ByRevision(ctx context.Context, organizationID string, kind Kind, configKey string, revision int64) (ConfigRevision, error) {
	if err := validateReadScope(organizationID, kind, configKey); err != nil {
		return ConfigRevision{}, err
	}
	if revision < 1 {
		return ConfigRevision{}, errors.New("config revision number is invalid")
	}
	executor, _, err := store.executor(ctx)
	if err != nil {
		return ConfigRevision{}, err
	}
	entry, err := scanRevision(executor.QueryRowContext(ctx, revisionSelect+` WHERE organization_id = ? AND kind = ? AND config_key = ? AND revision = ?`, organizationID, kind, configKey, revision))
	if errors.Is(err, sql.ErrNoRows) {
		return ConfigRevision{}, ErrNotFound
	}
	if err != nil {
		return ConfigRevision{}, fmt.Errorf("read config revision: %w", err)
	}
	if err := validateChain(ctx, executor, organizationID, kind, configKey, entry.Revision); err != nil {
		return ConfigRevision{}, err
	}
	return entry, nil
}

func (store *SQLiteStore) Latest(ctx context.Context, organizationID string, kind Kind, configKey string) (ConfigRevision, error) {
	if err := validateReadScope(organizationID, kind, configKey); err != nil {
		return ConfigRevision{}, err
	}
	executor, _, err := store.executor(ctx)
	if err != nil {
		return ConfigRevision{}, err
	}
	entry, err := scanRevision(executor.QueryRowContext(ctx, revisionSelect+` WHERE organization_id = ? AND kind = ? AND config_key = ? ORDER BY revision DESC LIMIT 1`, organizationID, kind, configKey))
	if errors.Is(err, sql.ErrNoRows) {
		return ConfigRevision{}, ErrNotFound
	}
	if err != nil {
		return ConfigRevision{}, fmt.Errorf("read latest config revision: %w", err)
	}
	if err := validateChain(ctx, executor, organizationID, kind, configKey, entry.Revision); err != nil {
		return ConfigRevision{}, err
	}
	return entry, nil
}

func (store *SQLiteStore) History(ctx context.Context, organizationID string, kind Kind, configKey string, limit int) ([]ConfigRevision, error) {
	if err := validateReadScope(organizationID, kind, configKey); err != nil {
		return nil, err
	}
	if limit < 0 {
		return nil, errors.New("config revision history limit is invalid")
	}
	if limit == 0 {
		limit = DefaultHistoryLimit
	}
	if limit > MaxHistoryLimit {
		return nil, errors.New("config revision history limit is too large")
	}
	executor, _, err := store.executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := executor.QueryContext(ctx, revisionSelect+` WHERE organization_id = ? AND kind = ? AND config_key = ? ORDER BY revision ASC LIMIT ?`, organizationID, kind, configKey, limit)
	if err != nil {
		return nil, fmt.Errorf("query config revision history: %w", err)
	}
	defer rows.Close()
	history := make([]ConfigRevision, 0, limit)
	for rows.Next() {
		entry, err := scanRevision(rows)
		if err != nil {
			return nil, fmt.Errorf("scan config revision history: %w", err)
		}
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config revision history: %w", err)
	}
	if len(history) > 0 {
		if err := validateChain(ctx, executor, organizationID, kind, configKey, history[len(history)-1].Revision); err != nil {
			return nil, err
		}
	}
	return history, nil
}

func validateChain(ctx context.Context, executor sqliteExecutor, organizationID string, kind Kind, configKey string, through int64) error {
	rows, err := executor.QueryContext(ctx, revisionSelect+` WHERE organization_id = ? AND kind = ? AND config_key = ? AND revision <= ? ORDER BY revision ASC`, organizationID, kind, configKey, through)
	if err != nil {
		return errors.New("config revision chain validation failed")
	}
	defer rows.Close()
	var previous ConfigRevision
	count := 0
	for rows.Next() {
		entry, err := scanRevision(rows)
		if err != nil {
			return errors.New("config revision chain validation failed")
		}
		if (count == 0 && (entry.Revision != 1 || entry.ParentRevision != 0)) || (count > 0 && (entry.Revision != previous.Revision+1 || entry.ParentRevision != previous.Revision)) {
			return errors.New("config revision chain is incomplete")
		}
		previous = entry
		count++
	}
	if rows.Err() != nil || count == 0 || previous.Revision != through {
		return errors.New("config revision chain is incomplete")
	}
	return nil
}

const revisionSelect = `SELECT id, organization_id, kind, config_key, revision, parent_revision, payload, payload_hash, created_by_type, created_by_id, created_at FROM iotd_config_revisions`

type sqliteExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (store *SQLiteStore) executor(ctx context.Context) (sqliteExecutor, bool, error) {
	if store == nil || store.database == nil || store.clock == nil {
		return nil, false, errors.New("config revision SQLite store is not configured")
	}
	if _, active := execution.Current(ctx); !active {
		return store.database, false, nil
	}
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("get config revision SQLite transaction handle: %w", err)
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, false, errors.New("config revision execution uses a non-SQLite transaction handle")
	}
	return transaction, true, nil
}

type rowScanner interface{ Scan(...any) error }

func scanRevision(row rowScanner) (ConfigRevision, error) {
	var entry ConfigRevision
	var payloadHash []byte
	var createdAt string
	if err := row.Scan(&entry.ID, &entry.OrganizationID, &entry.Kind, &entry.ConfigKey, &entry.Revision, &entry.ParentRevision, &entry.Payload, &payloadHash, &entry.CreatedByType, &entry.CreatedByID, &createdAt); err != nil {
		return ConfigRevision{}, err
	}
	if len(payloadHash) != len(entry.PayloadHash) {
		return ConfigRevision{}, errors.New("stored config revision hash has invalid length")
	}
	copy(entry.PayloadHash[:], payloadHash)
	parsed, err := time.Parse("2006-01-02T15:04:05.000000000Z", createdAt)
	if err != nil {
		return ConfigRevision{}, fmt.Errorf("parse stored config revision time: %w", err)
	}
	entry.CreatedAt = parsed.UTC()
	if !validIdentifier(entry.ID) || !validIdentifier(entry.OrganizationID) || !validIdentifier(entry.ConfigKey) || !validIdentifier(entry.CreatedByID) || !validKind(entry.Kind) || !validCreatedByType(entry.CreatedByType) || entry.Revision < 1 || entry.ParentRevision < 0 || entry.Revision != entry.ParentRevision+1 || formatUTCTime(entry.CreatedAt) != createdAt {
		return ConfigRevision{}, errors.New("stored config revision violates invariants")
	}
	canonicalPayload, canonicalHash, err := normalizePayload(entry.Payload)
	if err != nil || canonicalPayload != entry.Payload || canonicalHash != entry.PayloadHash {
		return ConfigRevision{}, errors.New("stored config revision payload is invalid")
	}
	return entry, nil
}

func validateAppendInput(input AppendInput) error {
	if !validIdentifier(input.ID) || !validIdentifier(input.OrganizationID) || !validIdentifier(input.ConfigKey) || !validIdentifier(input.CreatedByID) {
		return errors.New("config revision identifier is invalid")
	}
	if !validKind(input.Kind) || !validCreatedByType(input.CreatedByType) {
		return errors.New("config revision enum is invalid")
	}
	if input.ExpectedParentRevision < 0 || input.ExpectedParentRevision == math.MaxInt64 {
		return errors.New("config revision expected parent is invalid")
	}
	if !input.CreatedAt.IsZero() {
		return errors.New("config revision created at is assigned by the store")
	}
	return nil
}

func validateReadScope(organizationID string, kind Kind, configKey string) error {
	if !validIdentifier(organizationID) || !validIdentifier(configKey) || !validKind(kind) {
		return errors.New("config revision read scope is invalid")
	}
	return nil
}

func formatUTCTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
