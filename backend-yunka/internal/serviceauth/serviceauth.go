// Package serviceauth owns non-human service credentials. It is deliberately
// separate from the OIDC/BFF path: service credentials never create a human
// principal and are accepted only by the service transport adapter.
package serviceauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"yunka.io/framework/core/identity"
	yunkagrpc "yunka.io/gateway/rpc/transport/grpc"
)

const (
	CredentialPrefix           = "svc."
	maxServiceAccountIDBytes   = yunkagrpc.MaxServiceIdentityBytes - len("service-account/")
	maxCredentialIDBytes       = 128
	maxEncodedCredentialSecret = 256
	maxDecodedCredentialSecret = 64
)

var (
	ErrUnauthorized             = errors.New("service credential is unauthorized")
	ErrInvalidManagementRequest = errors.New("service credential management request is invalid")
	ErrServiceAccountNotFound   = errors.New("service account was not found")
	ErrCredentialNotFound       = errors.New("service credential was not found")
)

type Config struct {
	Now                                  func() time.Time
	NewID                                func() (string, error)
	NewSecret                            func() ([]byte, error)
	AllowInsecureTransportForDevelopment bool
	AuditRecorder                        *audit.SecurityRecorder
}

type Manager struct {
	database                             *sql.DB
	now                                  func() time.Time
	newID                                func() (string, error)
	newSecret                            func() ([]byte, error)
	allowInsecureTransportForDevelopment bool
	auditRecorder                        *audit.SecurityRecorder
}

type IssuedCredential struct {
	Credential       string
	CredentialID     string
	ServiceAccountID string
	ExpiresAt        time.Time
}

func NewManager(database *sql.DB, config Config) (*Manager, error) {
	if database == nil {
		return nil, errors.New("service credential SQLite database is required")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.NewID == nil {
		config.NewID = randomID
	}
	if config.NewSecret == nil {
		config.NewSecret = randomSecret
	}
	return &Manager{database: database, now: config.Now, newID: config.NewID, newSecret: config.NewSecret, allowInsecureTransportForDevelopment: config.AllowInsecureTransportForDevelopment, auditRecorder: config.AuditRecorder}, nil
}

// Issue returns a plaintext credential exactly once. Only its digest is
// persisted, and the target service account plus its organization must be
// active at the time of issuance.
func (manager *Manager) Issue(ctx context.Context, serviceAccountID string, expiresAt time.Time) (IssuedCredential, error) {
	if err := manager.validateManagementRequest(serviceAccountID, expiresAt); err != nil {
		return IssuedCredential{}, err
	}
	transaction, err := manager.database.BeginTx(ctx, nil)
	if err != nil {
		return IssuedCredential{}, fmt.Errorf("begin service credential issuance: %w", err)
	}
	defer transaction.Rollback()
	if err := requireActiveServiceAccount(ctx, transaction, serviceAccountID); err != nil {
		return IssuedCredential{}, err
	}
	issued, digest, err := manager.newIssuedCredential(serviceAccountID, expiresAt)
	if err != nil {
		return IssuedCredential{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO service_account_credentials (id, service_account_id, credential_hash, expires_at, created_at)
VALUES (?, ?, ?, ?, ?)`, issued.CredentialID, serviceAccountID, digest[:], formatTime(expiresAt), formatTime(manager.now())); err != nil {
		return IssuedCredential{}, fmt.Errorf("persist service credential digest: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return IssuedCredential{}, fmt.Errorf("commit service credential issuance: %w", err)
	}
	return issued, nil
}

// Rotate atomically persists a new credential and revokes the old one. The old
// credential is therefore invalid before the new plaintext credential returns.
func (manager *Manager) Rotate(ctx context.Context, serviceAccountID, currentCredential string, expiresAt time.Time) (IssuedCredential, error) {
	if err := manager.validateManagementRequest(serviceAccountID, expiresAt); err != nil {
		return IssuedCredential{}, err
	}
	credentialID, secret, ok := parseCredential(currentCredential)
	if !ok {
		return IssuedCredential{}, ErrUnauthorized
	}
	transaction, err := manager.database.BeginTx(ctx, nil)
	if err != nil {
		return IssuedCredential{}, fmt.Errorf("begin service credential rotation: %w", err)
	}
	defer transaction.Rollback()
	if err := manager.requireCurrentCredential(ctx, transaction, credentialID, secret, serviceAccountID); err != nil {
		return IssuedCredential{}, err
	}
	issued, digest, err := manager.newIssuedCredential(serviceAccountID, expiresAt)
	if err != nil {
		return IssuedCredential{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO service_account_credentials (id, service_account_id, credential_hash, expires_at, created_at)
VALUES (?, ?, ?, ?, ?)`, issued.CredentialID, serviceAccountID, digest[:], formatTime(expiresAt), formatTime(manager.now())); err != nil {
		return IssuedCredential{}, fmt.Errorf("persist rotated service credential digest: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `UPDATE service_account_credentials SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, formatTime(manager.now()), credentialID)
	if err != nil {
		return IssuedCredential{}, fmt.Errorf("revoke rotated service credential: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return IssuedCredential{}, fmt.Errorf("check rotated service credential revocation: %w", err)
	}
	if changed != 1 {
		return IssuedCredential{}, ErrUnauthorized
	}
	if err := transaction.Commit(); err != nil {
		return IssuedCredential{}, fmt.Errorf("commit service credential rotation: %w", err)
	}
	return issued, nil
}

// Revoke immediately invalidates one durable credential. It is idempotent for
// an existing credential and never deletes audit-relevant credential metadata.
func (manager *Manager) Revoke(ctx context.Context, credentialID string) error {
	if manager == nil || manager.database == nil || !validCredentialID(credentialID) {
		return ErrInvalidManagementRequest
	}
	if manager.auditRecorder != nil {
		return manager.revokeWithAudit(ctx, credentialID)
	}
	result, err := manager.database.ExecContext(ctx, `UPDATE service_account_credentials SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, formatTime(manager.now()), credentialID)
	if err != nil {
		return fmt.Errorf("revoke service credential: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check service credential revocation: %w", err)
	}
	if changed != 0 {
		return nil
	}
	var found int
	err = manager.database.QueryRowContext(ctx, `SELECT 1 FROM service_account_credentials WHERE id = ?`, credentialID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCredentialNotFound
	}
	if err != nil {
		return fmt.Errorf("read service credential for revocation: %w", err)
	}
	return nil
}

func (manager *Manager) revokeWithAudit(ctx context.Context, credentialID string) error {
	transaction, err := manager.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin service credential revocation: %w", err)
	}
	defer transaction.Rollback()
	var targetCredentialID, targetServiceAccountID, targetOrganizationID string
	if err := transaction.QueryRowContext(ctx, `
SELECT credentials.id, credentials.service_account_id, accounts.organization_id
FROM service_account_credentials credentials
JOIN service_accounts accounts ON accounts.id = credentials.service_account_id
WHERE credentials.id = ?`, credentialID).Scan(&targetCredentialID, &targetServiceAccountID, &targetOrganizationID); errors.Is(err, sql.ErrNoRows) {
		return ErrCredentialNotFound
	} else if err != nil {
		return fmt.Errorf("read service credential for revocation: %w", err)
	}
	if !cleanIdentifier(targetCredentialID) || !cleanIdentifier(targetServiceAccountID) || !cleanIdentifier(targetOrganizationID) {
		return ErrUnauthorized
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE service_account_credentials SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, formatTime(manager.now()), credentialID); err != nil {
		return fmt.Errorf("revoke service credential: %w", err)
	}
	if err := manager.auditRecorder.RecordRevocationInTransaction(ctx, transaction, "configuration.service_credential.revoke", "service.credential", targetCredentialID, targetOrganizationID); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit service credential revocation: %w", err)
	}
	return nil
}

// DisableServiceAccount immediately invalidates every credential belonging to
// the service account because Authenticate always checks account status.
func (manager *Manager) DisableServiceAccount(ctx context.Context, serviceAccountID string) error {
	if manager == nil || manager.database == nil || !cleanIdentifier(serviceAccountID) {
		return ErrInvalidManagementRequest
	}
	result, err := manager.database.ExecContext(ctx, `UPDATE service_accounts SET status = ?, updated_at = ? WHERE id = ? AND status = 'active'`, "disabled", formatTime(manager.now()), serviceAccountID)
	if err != nil {
		return fmt.Errorf("disable service account: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check disabled service account: %w", err)
	}
	if changed != 0 {
		return nil
	}
	var found int
	err = manager.database.QueryRowContext(ctx, `SELECT 1 FROM service_accounts WHERE id = ?`, serviceAccountID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrServiceAccountNotFound
	}
	if err != nil {
		return fmt.Errorf("read service account for disable: %w", err)
	}
	return nil
}

// Authenticate resolves only the standalone service credential format. All
// invalid credential states intentionally return the same error.
func (manager *Manager) Authenticate(ctx context.Context, candidate string) (identity.Principal, error) {
	if manager == nil || manager.database == nil {
		return identity.Principal{}, ErrUnauthorized
	}
	return manager.authenticate(ctx, candidate)
}

func (manager *Manager) authenticate(ctx context.Context, candidate string) (identity.Principal, error) {
	credentialID, secret, ok := parseCredential(candidate)
	if !ok {
		return identity.Principal{}, ErrUnauthorized
	}
	digest := sha256.Sum256(secret)
	credential, err := manager.findCredential(ctx, credentialID)
	if err != nil {
		return identity.Principal{}, ErrUnauthorized
	}
	if subtle.ConstantTimeCompare(digest[:], credential.hash) != 1 || credential.serviceAccountID == "" || credential.organizationID == "" || credential.serviceAccountStatus != "active" || credential.organizationStatus != "active" || credential.revokedAt.Valid || !credential.expiresAt.After(manager.now().UTC()) {
		return identity.Principal{}, ErrUnauthorized
	}
	subject := "service-account/" + credential.serviceAccountID
	if !validPrincipalValue(subject) || !validPrincipalValue(credential.organizationID) {
		return identity.Principal{}, ErrUnauthorized
	}
	return identity.Principal{
		Subject:       subject,
		TenantID:      credential.organizationID,
		AuthMethod:    identity.AuthMethodServiceToken,
		Authenticated: true,
	}, nil
}

type storedCredential struct {
	hash                 []byte
	serviceAccountID     string
	organizationID       string
	expiresAt            time.Time
	revokedAt            sql.NullString
	serviceAccountStatus string
	organizationStatus   string
}

func (manager *Manager) findCredential(ctx context.Context, credentialID string) (storedCredential, error) {
	var credential storedCredential
	var expiresAt string
	err := manager.database.QueryRowContext(ctx, `
SELECT c.credential_hash, c.service_account_id, sa.organization_id, c.expires_at, c.revoked_at, sa.status, o.status
FROM service_account_credentials c
JOIN service_accounts sa ON sa.id = c.service_account_id
JOIN organizations o ON o.id = sa.organization_id
WHERE c.id = ?`, credentialID).Scan(
		&credential.hash, &credential.serviceAccountID, &credential.organizationID, &expiresAt, &credential.revokedAt, &credential.serviceAccountStatus, &credential.organizationStatus)
	if err != nil {
		return storedCredential{}, err
	}
	credential.expiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return storedCredential{}, err
	}
	return credential, nil
}

func (manager *Manager) requireCurrentCredential(ctx context.Context, transaction *sql.Tx, credentialID string, secret []byte, serviceAccountID string) error {
	var hash []byte
	var persistedServiceAccountID, expiresAt string
	var revokedAt sql.NullString
	var serviceAccountStatus, organizationStatus string
	err := transaction.QueryRowContext(ctx, `
SELECT c.credential_hash, c.service_account_id, c.expires_at, c.revoked_at, sa.status, o.status
FROM service_account_credentials c
JOIN service_accounts sa ON sa.id = c.service_account_id
JOIN organizations o ON o.id = sa.organization_id
WHERE c.id = ?`, credentialID).Scan(&hash, &persistedServiceAccountID, &expiresAt, &revokedAt, &serviceAccountStatus, &organizationStatus)
	if err != nil {
		return ErrUnauthorized
	}
	digest := sha256.Sum256(secret)
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || subtle.ConstantTimeCompare(digest[:], hash) != 1 || persistedServiceAccountID != serviceAccountID || revokedAt.Valid || !expires.After(manager.now().UTC()) || serviceAccountStatus != "active" || organizationStatus != "active" {
		return ErrUnauthorized
	}
	return nil
}

func requireActiveServiceAccount(ctx context.Context, transaction *sql.Tx, serviceAccountID string) error {
	var serviceAccountStatus, organizationStatus string
	err := transaction.QueryRowContext(ctx, `
SELECT sa.status, o.status
FROM service_accounts sa
JOIN organizations o ON o.id = sa.organization_id
WHERE sa.id = ?`, serviceAccountID).Scan(&serviceAccountStatus, &organizationStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrServiceAccountNotFound
	}
	if err != nil {
		return fmt.Errorf("read service account for credential issuance: %w", err)
	}
	if serviceAccountStatus != "active" || organizationStatus != "active" {
		return ErrInvalidManagementRequest
	}
	return nil
}

func (manager *Manager) newIssuedCredential(serviceAccountID string, expiresAt time.Time) (IssuedCredential, [sha256.Size]byte, error) {
	credentialID, err := manager.newID()
	if err != nil || !validCredentialID(credentialID) {
		return IssuedCredential{}, [sha256.Size]byte{}, errors.New("generate service credential identifier")
	}
	secret, err := manager.newSecret()
	if err != nil || len(secret) < yunkagrpc.MinServiceTokenBytes || len(secret) > maxDecodedCredentialSecret {
		return IssuedCredential{}, [sha256.Size]byte{}, errors.New("generate service credential secret")
	}
	digest := sha256.Sum256(secret)
	return IssuedCredential{
		Credential:       CredentialPrefix + credentialID + "." + base64.RawURLEncoding.EncodeToString(secret),
		CredentialID:     credentialID,
		ServiceAccountID: serviceAccountID,
		ExpiresAt:        expiresAt.UTC(),
	}, digest, nil
}

func (manager *Manager) validateManagementRequest(serviceAccountID string, expiresAt time.Time) error {
	if manager == nil || manager.database == nil || manager.now == nil || manager.newID == nil || manager.newSecret == nil || !validServiceAccountID(serviceAccountID) || !expiresAt.After(manager.now().UTC()) {
		return ErrInvalidManagementRequest
	}
	return nil
}

func parseCredential(candidate string) (string, []byte, bool) {
	if !validServiceToken(candidate) || !strings.HasPrefix(candidate, CredentialPrefix) {
		return "", nil, false
	}
	parts := strings.Split(strings.TrimPrefix(candidate, CredentialPrefix), ".")
	if len(parts) != 2 || !validCredentialID(parts[0]) || len(parts[1]) == 0 || len(parts[1]) > maxEncodedCredentialSecret {
		return "", nil, false
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(secret) < yunkagrpc.MinServiceTokenBytes || len(secret) > maxDecodedCredentialSecret {
		return "", nil, false
	}
	return parts[0], secret, true
}

func validServiceToken(value string) bool {
	if len(value) < yunkagrpc.MinServiceTokenBytes || len(value) > yunkagrpc.MaxServiceTokenBytes || strings.TrimSpace(value) != value {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

func cleanIdentifier(value string) bool {
	return value != "" && len(value) <= yunkagrpc.MaxServiceIdentityBytes && strings.TrimSpace(value) == value && strings.IndexFunc(value, func(current rune) bool { return current < 0x20 || current == 0x7f }) < 0
}

func validServiceAccountID(value string) bool {
	return cleanIdentifier(value) && len(value) <= maxServiceAccountIDBytes
}

func validCredentialID(value string) bool {
	if len(value) == 0 || len(value) > maxCredentialIDBytes {
		return false
	}
	for index := range len(value) {
		current := value[index]
		if (current < 'a' || current > 'z') && (current < 'A' || current > 'Z') && (current < '0' || current > '9') && current != '_' && current != '-' {
			return false
		}
	}
	return true
}

func validPrincipalValue(value string) bool {
	return cleanIdentifier(value)
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func randomSecret() ([]byte, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	return value, nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
