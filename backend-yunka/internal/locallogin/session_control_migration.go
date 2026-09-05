package locallogin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const SessionControlMigrationID = "YU-22_local_session_controls_v1"

const (
	sessionRevocationCASAbort       = "local session revocation requires revision CAS"
	sessionResurrectionAbort       = "revoked local session cannot be reactivated"
	sessionRevisionMutationAbort   = "local session revision may change only on revocation"
	sessionCredentialStaleAbort    = "local session credential revision is stale"
	sessionIdentityMutationAbort   = "local session identity is immutable"
	sessionRevokedAtMutationAbort  = "local session revocation timestamp is immutable"
)

const sessionRevocationCASTrigger = `
CREATE TRIGGER IF NOT EXISTS iotd_local_sessions_require_revocation_revision
BEFORE UPDATE ON iotd_local_sessions
WHEN OLD.status = 'active'
 AND NEW.status = 'revoked'
 AND NEW.revision <> OLD.revision + 1
BEGIN
    SELECT RAISE(ABORT, 'local session revocation requires revision CAS');
END;`

const sessionResurrectionTrigger = `
CREATE TRIGGER IF NOT EXISTS iotd_local_sessions_prevent_reactivation
BEFORE UPDATE OF status ON iotd_local_sessions
WHEN OLD.status = 'revoked' AND NEW.status <> 'revoked'
BEGIN
    SELECT RAISE(ABORT, 'revoked local session cannot be reactivated');
END;`

const sessionRevisionMutationTrigger = `
CREATE TRIGGER IF NOT EXISTS iotd_local_sessions_revision_only_on_revocation
BEFORE UPDATE OF revision ON iotd_local_sessions
WHEN OLD.status = NEW.status AND NEW.revision <> OLD.revision
BEGIN
    SELECT RAISE(ABORT, 'local session revision may change only on revocation');
END;`

const sessionCredentialRevisionTrigger = `
CREATE TRIGGER IF NOT EXISTS iotd_local_sessions_require_current_credential
BEFORE INSERT ON iotd_local_sessions
WHEN NOT EXISTS (
    SELECT 1
    FROM iotd_local_user_credentials credential
    WHERE credential.organization_id = NEW.organization_id
      AND credential.user_id = NEW.user_id
      AND credential.revision = NEW.credential_revision
)
BEGIN
    SELECT RAISE(ABORT, 'local session credential revision is stale');
END;`

const sessionIdentityMutationTrigger = `
CREATE TRIGGER IF NOT EXISTS iotd_local_sessions_identity_immutable
BEFORE UPDATE OF id, organization_id, user_id, secret_digest, credential_revision, created_at, expires_at ON iotd_local_sessions
WHEN NEW.id <> OLD.id
  OR NEW.organization_id <> OLD.organization_id
  OR NEW.user_id <> OLD.user_id
  OR NEW.secret_digest <> OLD.secret_digest
  OR NEW.credential_revision <> OLD.credential_revision
  OR NEW.created_at <> OLD.created_at
  OR NEW.expires_at <> OLD.expires_at
BEGIN
    SELECT RAISE(ABORT, 'local session identity is immutable');
END;`

const sessionRevokedAtMutationTrigger = `
CREATE TRIGGER IF NOT EXISTS iotd_local_sessions_revoked_at_immutable
BEFORE UPDATE OF revoked_at ON iotd_local_sessions
WHEN OLD.status = 'revoked' AND NEW.revoked_at <> OLD.revoked_at
BEGIN
    SELECT RAISE(ABORT, 'local session revocation timestamp is immutable');
END;`

func applySessionControlMigration(ctx context.Context, tx *sql.Tx) error {
	if err := ensureSessionRevision(ctx, tx); err != nil {
		return err
	}
	for name, statement := range map[string]string{
		"revocation CAS":       sessionRevocationCASTrigger,
		"reactivation":         sessionResurrectionTrigger,
		"revision mutation":    sessionRevisionMutationTrigger,
		"credential revision":  sessionCredentialRevisionTrigger,
		"identity mutation":    sessionIdentityMutationTrigger,
		"revoked-at mutation":  sessionRevokedAtMutationTrigger,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("install local session %s trigger: %w", name, err)
		}
	}
	if err := verifySessionControlTriggers(ctx, tx); err != nil {
		return err
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, SessionControlMigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read local session control migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, SessionControlMigrationID); err != nil {
			return fmt.Errorf("record local session control migration: %w", err)
		}
	} else if applied != 1 {
		return errors.New("local session control migration ledger is invalid")
	}
	return nil
}

func ensureSessionRevision(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info('iotd_local_sessions')`)
	if err != nil {
		return fmt.Errorf("inspect local session revision: %w", err)
	}
	hasRevision := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan local session revision: %w", err)
		}
		if name == "revision" {
			hasRevision = true
			if strings.ToUpper(typeName) != "INTEGER" || notNull != 1 || fmt.Sprint(defaultValue) != "1" {
				_ = rows.Close()
				return errors.New("local session revision column has an invalid contract")
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate local session revision: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close local session revision inspection: %w", err)
	}
	if !hasRevision {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE iotd_local_sessions ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)`); err != nil {
			return fmt.Errorf("add local session revision: %w", err)
		}
	}
	var tableSQL string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'iotd_local_sessions'`).Scan(&tableSQL); err != nil {
		return errors.New("local session table is missing after revision migration")
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(tableSQL), " "))
	if !strings.Contains(normalized, "revision integer not null default 1 check (revision >= 1)") {
		return errors.New("local session revision physical constraint is missing")
	}
	return nil
}

func verifySessionControlTriggers(ctx context.Context, tx *sql.Tx) error {
	required := map[string][]string{
		"iotd_local_sessions_require_revocation_revision": {
			"before update on iotd_local_sessions",
			"old.status = 'active'",
			"new.status = 'revoked'",
			"new.revision <> old.revision + 1",
			"raise(abort, 'local session revocation requires revision cas')",
		},
		"iotd_local_sessions_prevent_reactivation": {
			"before update of status on iotd_local_sessions",
			"old.status = 'revoked'",
			"new.status <> 'revoked'",
			"raise(abort, 'revoked local session cannot be reactivated')",
		},
		"iotd_local_sessions_revision_only_on_revocation": {
			"before update of revision on iotd_local_sessions",
			"old.status = new.status",
			"new.revision <> old.revision",
			"raise(abort, 'local session revision may change only on revocation')",
		},
		"iotd_local_sessions_require_current_credential": {
			"before insert on iotd_local_sessions",
			"credential.revision = new.credential_revision",
			"raise(abort, 'local session credential revision is stale')",
		},
		"iotd_local_sessions_identity_immutable": {
			"before update of id, organization_id, user_id, secret_digest, credential_revision, created_at, expires_at on iotd_local_sessions",
			"new.expires_at <> old.expires_at",
			"raise(abort, 'local session identity is immutable')",
		},
		"iotd_local_sessions_revoked_at_immutable": {
			"before update of revoked_at on iotd_local_sessions",
			"old.status = 'revoked'",
			"new.revoked_at <> old.revoked_at",
			"raise(abort, 'local session revocation timestamp is immutable')",
		},
	}
	for name, fragments := range required {
		var definition string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&definition); err != nil {
			return fmt.Errorf("local session control trigger %s is missing", name)
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
		for _, fragment := range fragments {
			if !strings.Contains(normalized, fragment) {
				return fmt.Errorf("local session control trigger %s is invalid", name)
			}
		}
	}
	return nil
}
