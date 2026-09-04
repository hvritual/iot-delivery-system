package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// MigrationID is independently versioned while sharing the repository-wide
// SQLite migration ledger with identity and authorization schemas.
const MigrationID = "S0-04-01_audit_entries_v1"

const auditSchema = `
CREATE TABLE iotd_audit_entries (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE CHECK (length(trim(id)) > 0),
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    event_category TEXT NOT NULL CHECK (event_category IN ('authentication', 'authorization', 'delivery', 'configuration', 'system', 'legacy')),
    organization_id TEXT,
    project_id TEXT,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('human', 'service', 'system', 'anonymous', 'legacy')),
    actor_id TEXT,
    operation TEXT NOT NULL CHECK (length(trim(operation)) > 0),
    authorization_decision TEXT NOT NULL CHECK (authorization_decision IN ('allowed', 'denied', 'not_evaluated')),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('organization', 'project', 'object', 'system')),
    scope_id TEXT,
    target_type TEXT,
    target_id TEXT,
    result TEXT NOT NULL CHECK (result IN ('success', 'failure', 'denied')),
    reason_code TEXT NOT NULL CHECK (length(trim(reason_code)) > 0),
    trace_id TEXT,
    request_id TEXT,
    correlation_id TEXT,
    diff_summary TEXT NOT NULL CHECK (json_valid(diff_summary) AND json_type(diff_summary) = 'object'),
    metadata TEXT NOT NULL CHECK (json_valid(metadata) AND json_type(metadata) = 'object'),
    occurred_at TEXT NOT NULL CHECK (occurred_at GLOB '????-??-??T??:??:??*Z' AND strftime('%s', occurred_at) IS NOT NULL),
    recorded_at TEXT NOT NULL CHECK (recorded_at GLOB '????-??-??T??:??:??*Z' AND strftime('%s', recorded_at) IS NOT NULL),
    CHECK (project_id IS NULL OR organization_id IS NOT NULL),
    CHECK ((actor_type = 'anonymous' AND actor_id IS NULL) OR (actor_type IN ('human', 'service', 'system', 'legacy') AND actor_id IS NOT NULL AND length(trim(actor_id)) > 0)),
    CHECK ((scope_type = 'system' AND scope_id IS NULL) OR (scope_type = 'organization' AND organization_id IS NOT NULL AND scope_id = organization_id) OR (scope_type = 'project' AND organization_id IS NOT NULL AND project_id IS NOT NULL AND scope_id = project_id) OR (scope_type = 'object' AND scope_id IS NOT NULL AND length(trim(scope_id)) > 0)),
    CHECK ((target_type IS NULL AND target_id IS NULL) OR (length(trim(target_type)) > 0 AND length(trim(target_id)) > 0)),
    CHECK ((authorization_decision = 'denied' AND result = 'denied') OR (authorization_decision <> 'denied' AND result <> 'denied'))
);
CREATE INDEX idx_iotd_audit_entries_organization_time ON iotd_audit_entries (organization_id, occurred_at DESC, sequence DESC);
CREATE INDEX idx_iotd_audit_entries_project_time ON iotd_audit_entries (project_id, occurred_at DESC, sequence DESC);
CREATE INDEX idx_iotd_audit_entries_actor_time ON iotd_audit_entries (actor_type, actor_id, occurred_at DESC, sequence DESC);
CREATE INDEX idx_iotd_audit_entries_operation_time ON iotd_audit_entries (operation, occurred_at DESC, sequence DESC);
CREATE INDEX idx_iotd_audit_entries_target_time ON iotd_audit_entries (target_type, target_id, occurred_at DESC, sequence DESC);
CREATE INDEX idx_iotd_audit_entries_trace_time ON iotd_audit_entries (trace_id, occurred_at DESC, sequence DESC);
CREATE INDEX idx_iotd_audit_entries_correlation_time ON iotd_audit_entries (correlation_id, occurred_at DESC, sequence DESC);
CREATE TRIGGER iotd_audit_entries_append_only_update
BEFORE UPDATE ON iotd_audit_entries
BEGIN
    SELECT RAISE(ABORT, 'audit entries are append-only');
END;
CREATE TRIGGER iotd_audit_entries_append_only_delete
BEFORE DELETE ON iotd_audit_entries
BEGIN
    SELECT RAISE(ABORT, 'audit entries are append-only');
END;`

var requiredAuditColumns = []string{
	"id", "sequence", "schema_version", "event_category", "organization_id", "project_id",
	"actor_type", "actor_id", "operation", "authorization_decision", "scope_type", "scope_id",
	"target_type", "target_id", "result", "reason_code", "trace_id", "request_id",
	"correlation_id", "diff_summary", "metadata", "occurred_at", "recorded_at",
}

var requiredAuditIndexes = []string{
	"idx_iotd_audit_entries_organization_time",
	"idx_iotd_audit_entries_project_time",
	"idx_iotd_audit_entries_actor_time",
	"idx_iotd_audit_entries_operation_time",
	"idx_iotd_audit_entries_target_time",
	"idx_iotd_audit_entries_trace_time",
	"idx_iotd_audit_entries_correlation_time",
}

var requiredAuditTriggers = []string{
	"iotd_audit_entries_append_only_update",
	"iotd_audit_entries_append_only_delete",
}

// ApplyMigrations transactionally installs the audit schema and records it
// exactly once. It neither reads nor changes existing delivery or identity data.
func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("audit SQLite database is required")
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin audit migration transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')))`); err != nil {
		return fmt.Errorf("create audit migration ledger: %w", err)
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read audit migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, auditSchema); err != nil {
			return fmt.Errorf("apply audit schema: %w", err)
		}
		if err := verifyAuditSchema(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, MigrationID); err != nil {
			return fmt.Errorf("record audit migration: %w", err)
		}
	}
	if err := verifyAuditSchema(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audit migration: %w", err)
	}
	return nil
}

func verifyAuditSchema(ctx context.Context, tx *sql.Tx) error {
	var table string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'iotd_audit_entries'`).Scan(&table); err != nil {
		return fmt.Errorf("verify audit table: %w", err)
	}
	if err := verifyAuditSchemaNames(ctx, tx, `SELECT name FROM pragma_table_info('iotd_audit_entries')`, requiredAuditColumns, "column"); err != nil {
		return err
	}
	if err := verifyAuditSchemaNames(ctx, tx, `SELECT name FROM pragma_index_list('iotd_audit_entries')`, requiredAuditIndexes, "index"); err != nil {
		return err
	}
	return verifyAuditSchemaNames(ctx, tx, `SELECT name FROM sqlite_master WHERE type = 'trigger' AND tbl_name = 'iotd_audit_entries'`, requiredAuditTriggers, "trigger")
}

func verifyAuditSchemaNames(ctx context.Context, tx *sql.Tx, query string, required []string, kind string) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read audit %s metadata: %w", kind, err)
	}
	defer rows.Close()
	found := make(map[string]bool, len(required))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan audit %s metadata: %w", kind, err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate audit %s metadata: %w", kind, err)
	}
	for _, name := range required {
		if !found[name] {
			return fmt.Errorf("audit schema is missing required %s %q", kind, name)
		}
	}
	return nil
}
