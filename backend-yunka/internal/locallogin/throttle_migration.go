package locallogin

import (
	"context"
	"database/sql"
	"strings"
)

const throttleMigrationID = "YU-32H_password_attempts_v1"
const throttleSchema = `CREATE TABLE IF NOT EXISTS iotd_local_password_attempts (
    bucket TEXT PRIMARY KEY NOT NULL CHECK (length(bucket) = 64),
    attempts INTEGER NOT NULL CHECK (attempts >= 1 AND attempts <= 10000),
    reset_at INTEGER NOT NULL CHECK (reset_at > 0),
    blocked_until INTEGER NOT NULL CHECK (blocked_until >= 0),
    expires_at INTEGER NOT NULL CHECK (expires_at > 0)
)`
const throttleIndex = `CREATE INDEX IF NOT EXISTS idx_iotd_local_password_attempts_expiry ON iotd_local_password_attempts (expires_at)`

func applyThrottleMigration(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{throttleSchema, throttleIndex} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return ErrThrottleUnavailable
		}
	}
	// Verify actual schema even if the ledger already claims success.
	for name, expected := range map[string]string{
		"iotd_local_password_attempts":            throttleSchema,
		"idx_iotd_local_password_attempts_expiry": throttleIndex,
	} {
		var actual string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name = ?`, name).Scan(&actual); err != nil {
			return ErrThrottleUnavailable
		}
		normalize := func(s string) string {
			return strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(s, "IF NOT EXISTS ", ""))), " ")
		}
		if normalize(actual) != normalize(expected) {
			return ErrThrottleUnavailable
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) ON CONFLICT (migration_id) DO NOTHING`, throttleMigrationID)
	if err != nil {
		return ErrThrottleUnavailable
	}
	return nil
}
