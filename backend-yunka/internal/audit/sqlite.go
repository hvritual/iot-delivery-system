package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"yunka.io/framework/execution"
)

var ErrNotFound = errors.New("audit entry not found")

// Store is the small append/read port for future transactionally coupled audit
// writers. It deliberately exposes no update or delete operation.
type Store interface {
	Append(context.Context, Entry) (Entry, error)
	ByID(context.Context, string) (Entry, error)
}

type SQLiteStore struct {
	database *sql.DB
	clock    func() time.Time
}

var _ Store = (*SQLiteStore)(nil)

type SQLiteStoreOption func(*SQLiteStore) error

// WithClock supplies the store-side clock used to assign recorded_at. The
// returned instant is normalized to UTC before persistence.
func WithClock(clock func() time.Time) SQLiteStoreOption {
	return func(store *SQLiteStore) error {
		if clock == nil {
			return errors.New("audit clock is required")
		}
		store.clock = clock
		return nil
	}
}

func NewSQLiteStore(database *sql.DB, options ...SQLiteStoreOption) (*SQLiteStore, error) {
	if database == nil {
		return nil, errors.New("audit SQLite database is required")
	}
	store := &SQLiteStore{database: database, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("audit SQLite store option is required")
		}
		if err := option(store); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (store *SQLiteStore) Append(ctx context.Context, entry Entry) (Entry, error) {
	if store == nil || store.clock == nil {
		return Entry{}, errors.New("audit SQLite store is not configured")
	}
	var err error
	entry.Metadata, err = normalizeAuditJSON(entry.Metadata)
	if err != nil {
		return Entry{}, err
	}
	if err := entry.validate(); err != nil {
		return Entry{}, err
	}
	recordedAt := store.clock().UTC()
	if recordedAt.IsZero() {
		return Entry{}, errors.New("audit clock returned zero time")
	}
	entry.RecordedAt = recordedAt
	executor, err := store.executor(ctx)
	if err != nil {
		return Entry{}, err
	}
	result, err := executor.ExecContext(ctx, `INSERT INTO iotd_audit_entries (
id, schema_version, event_category, organization_id, project_id, actor_type, actor_id, operation,
authorization_decision, scope_type, scope_id, target_type, target_id, result, reason_code,
trace_id, request_id, correlation_id, diff_summary, metadata, occurred_at, recorded_at
) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''),
NULLIF(?, ''), NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)`,
		entry.ID, entry.SchemaVersion, entry.EventCategory, entry.OrganizationID, entry.ProjectID, entry.ActorType, entry.ActorID, entry.Operation,
		entry.AuthorizationDecision, entry.ScopeType, entry.ScopeID, entry.TargetType, entry.TargetID, entry.Result, entry.ReasonCode,
		entry.TraceID, entry.RequestID, entry.CorrelationID, entry.DiffSummary, entry.Metadata,
		formatUTCTime(entry.OccurredAt), formatUTCTime(entry.RecordedAt))
	if err != nil {
		return Entry{}, fmt.Errorf("append audit entry: %w", err)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return Entry{}, fmt.Errorf("read appended audit sequence: %w", err)
	}
	entry.Sequence = sequence
	return entry, nil
}

// AppendInTransaction appends through the caller's SQLite transaction. It is
// used only when a security-state transition must fail closed with its audit.
func (store *SQLiteStore) AppendInTransaction(ctx context.Context, transaction *sql.Tx, entry Entry) (Entry, error) {
	if store == nil || store.clock == nil || transaction == nil {
		return Entry{}, errors.New("audit SQLite transaction store is not configured")
	}
	var err error
	entry.Metadata, err = normalizeAuditJSON(entry.Metadata)
	if err != nil {
		return Entry{}, err
	}
	if err := entry.validate(); err != nil {
		return Entry{}, err
	}
	entry.RecordedAt = store.clock().UTC()
	if entry.RecordedAt.IsZero() {
		return Entry{}, errors.New("audit clock returned zero time")
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO iotd_audit_entries (
id, schema_version, event_category, organization_id, project_id, actor_type, actor_id, operation,
authorization_decision, scope_type, scope_id, target_type, target_id, result, reason_code,
trace_id, request_id, correlation_id, diff_summary, metadata, occurred_at, recorded_at
) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''),
NULLIF(?, ''), NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)`,
		entry.ID, entry.SchemaVersion, entry.EventCategory, entry.OrganizationID, entry.ProjectID, entry.ActorType, entry.ActorID, entry.Operation,
		entry.AuthorizationDecision, entry.ScopeType, entry.ScopeID, entry.TargetType, entry.TargetID, entry.Result, entry.ReasonCode,
		entry.TraceID, entry.RequestID, entry.CorrelationID, entry.DiffSummary, entry.Metadata,
		formatUTCTime(entry.OccurredAt), formatUTCTime(entry.RecordedAt))
	if err != nil {
		return Entry{}, fmt.Errorf("append audit entry: %w", err)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return Entry{}, fmt.Errorf("read appended audit sequence: %w", err)
	}
	entry.Sequence = sequence
	return entry, nil
}

func (store *SQLiteStore) ByID(ctx context.Context, id string) (Entry, error) {
	if err := validateIdentifier("audit id", id, false); err != nil {
		return Entry{}, err
	}
	executor, err := store.executor(ctx)
	if err != nil {
		return Entry{}, err
	}
	entry, err := scanEntry(executor.QueryRowContext(ctx, `SELECT sequence, id, schema_version, event_category, COALESCE(organization_id, ''), COALESCE(project_id, ''), actor_type, COALESCE(actor_id, ''), operation, authorization_decision, scope_type, COALESCE(scope_id, ''), COALESCE(target_type, ''), COALESCE(target_id, ''), result, reason_code, COALESCE(trace_id, ''), COALESCE(request_id, ''), COALESCE(correlation_id, ''), diff_summary, metadata, occurred_at, recorded_at FROM iotd_audit_entries WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("read audit entry: %w", err)
	}
	return entry, nil
}

type sqliteExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *SQLiteStore) executor(ctx context.Context) (sqliteExecutor, error) {
	if store == nil || store.database == nil || store.clock == nil {
		return nil, errors.New("audit SQLite store is not configured")
	}
	if _, active := execution.Current(ctx); !active {
		return store.database, nil
	}
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("get audit SQLite transaction handle: %w", err)
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, errors.New("audit execution uses a non-SQLite transaction handle")
	}
	return transaction, nil
}

func formatUTCTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
