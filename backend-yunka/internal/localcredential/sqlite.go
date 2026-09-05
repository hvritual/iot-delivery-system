package localcredential

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hvritual/yunka.io/framework/execution"
)

const (
	maxInt64Value  int64 = 1<<63 - 1
	maxInt32Value  int64 = 1<<31 - 1
	maxUint32Value int64 = 1<<32 - 1
	maxUint8Value  int64 = 1<<8 - 1
)

var (
	ErrNotFound         = errors.New("local credential not found")
	ErrRevisionConflict = errors.New("local credential revision conflict")
	ErrUserNotFound     = errors.New("local credential user not found")
)

type Metadata struct {
	OrganizationID string
	UserID         string
	Revision       int64
	PolicyVersion  int64
	Algorithm      string
	ArgonVersion   int
	MemoryKiB      uint32
	Iterations     uint32
	Parallelism    uint8
	SaltLength     int
	HashLength     int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Verification struct {
	Match       bool
	NeedsRehash bool
	Revision    int64
}

type SQLiteRepository struct {
	database *sql.DB
	policies PolicySet
	random   io.Reader
	clock    func() time.Time
}

type Option func(*SQLiteRepository) error

func WithPolicySet(policies PolicySet) Option {
	return func(repository *SQLiteRepository) error {
		if _, err := policies.Current(); err != nil {
			return err
		}
		repository.policies = policies
		return nil
	}
}

func WithRandomSource(random io.Reader) Option {
	return func(repository *SQLiteRepository) error {
		if random == nil {
			return errors.New("local credential random source is required")
		}
		repository.random = random
		return nil
	}
}

func WithClock(clock func() time.Time) Option {
	return func(repository *SQLiteRepository) error {
		if clock == nil {
			return errors.New("local credential clock is required")
		}
		repository.clock = clock
		return nil
	}
}

func NewSQLiteRepository(database *sql.DB, options ...Option) (*SQLiteRepository, error) {
	if database == nil {
		return nil, errors.New("local credential SQLite database is required")
	}
	policies, err := DefaultPolicySet()
	if err != nil {
		return nil, err
	}
	repository := &SQLiteRepository{database: database, policies: policies, random: rand.Reader, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("local credential repository option is required")
		}
		if err := option(repository); err != nil {
			return nil, err
		}
	}
	return repository, nil
}

// SetPassword creates or replaces a user's local password using optimistic CAS.
// expectedRevision=0 means create-only. Plaintext is passed only to Argon2id;
// SQL receives the random salt and derived hash, never the password bytes.
func (repository *SQLiteRepository) SetPassword(ctx context.Context, organizationID, userID string, password []byte, expectedRevision int64) (Metadata, error) {
	if err := repository.ready(); err != nil {
		return Metadata{}, err
	}
	if !canonicalIdentifier(organizationID) || !canonicalIdentifier(userID) || expectedRevision < 0 || expectedRevision == maxInt64Value {
		return Metadata{}, errors.New("local credential write scope is invalid")
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return Metadata{}, err
	}
	if err := requireUser(ctx, executor, organizationID, userID); err != nil {
		return Metadata{}, err
	}
	material, err := repository.policies.hashPassword(password, repository.random)
	if err != nil {
		return Metadata{}, err
	}
	defer zeroBytes(material.hash)
	now := repository.clock().UTC()
	if now.IsZero() {
		return Metadata{}, errors.New("local credential clock returned zero time")
	}
	formatted := formatTime(now)
	var result sql.Result
	if expectedRevision == 0 {
		result, err = executor.ExecContext(ctx, `
INSERT INTO iotd_local_user_credentials (
    organization_id, user_id, revision, policy_version, algorithm, argon_version,
    memory_kib, iterations, parallelism, salt, password_hash, created_at, updated_at
)
SELECT ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
WHERE NOT EXISTS (
    SELECT 1 FROM iotd_local_user_credentials WHERE organization_id = ? AND user_id = ?
)`,
			organizationID, userID,
			material.policy.PolicyVersion, material.policy.Algorithm, material.policy.ArgonVersion,
			material.policy.MemoryKiB, material.policy.Iterations, material.policy.Parallelism,
			material.salt, material.hash, formatted, formatted,
			organizationID, userID,
		)
	} else {
		result, err = executor.ExecContext(ctx, `
UPDATE iotd_local_user_credentials
SET revision = revision + 1,
    policy_version = ?, algorithm = ?, argon_version = ?, memory_kib = ?, iterations = ?, parallelism = ?,
    salt = ?, password_hash = ?, updated_at = ?
WHERE organization_id = ? AND user_id = ? AND revision = ?`,
			material.policy.PolicyVersion, material.policy.Algorithm, material.policy.ArgonVersion,
			material.policy.MemoryKiB, material.policy.Iterations, material.policy.Parallelism,
			material.salt, material.hash, formatted,
			organizationID, userID, expectedRevision,
		)
	}
	if err != nil {
		return Metadata{}, errors.New("persist local credential")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Metadata{}, errors.New("read local credential write result")
	}
	if rows != 1 {
		return Metadata{}, ErrRevisionConflict
	}
	return metadataWithExecutor(ctx, executor, repository.policies, organizationID, userID)
}

func (repository *SQLiteRepository) VerifyPassword(ctx context.Context, organizationID, userID string, password []byte) (Verification, error) {
	if err := repository.ready(); err != nil {
		return Verification{}, err
	}
	if !canonicalIdentifier(organizationID) || !canonicalIdentifier(userID) {
		return Verification{}, errors.New("local credential read scope is invalid")
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return Verification{}, err
	}
	row, err := readCredential(ctx, executor, repository.policies, organizationID, userID)
	if err != nil {
		return Verification{}, err
	}
	match, err := repository.policies.verifyPassword(password, row.material)
	if err != nil {
		return Verification{}, err
	}
	return Verification{Match: match, NeedsRehash: match && repository.policies.needsRehash(row.metadata.PolicyVersion), Revision: row.metadata.Revision}, nil
}

func (repository *SQLiteRepository) Metadata(ctx context.Context, organizationID, userID string) (Metadata, error) {
	if err := repository.ready(); err != nil {
		return Metadata{}, err
	}
	if !canonicalIdentifier(organizationID) || !canonicalIdentifier(userID) {
		return Metadata{}, errors.New("local credential read scope is invalid")
	}
	executor, err := repository.executor(ctx)
	if err != nil {
		return Metadata{}, err
	}
	return metadataWithExecutor(ctx, executor, repository.policies, organizationID, userID)
}

func (repository *SQLiteRepository) ready() error {
	if repository == nil || repository.database == nil || repository.random == nil || repository.clock == nil {
		return errors.New("local credential repository is not configured")
	}
	if _, err := repository.policies.Current(); err != nil {
		return err
	}
	return nil
}

type sqliteExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (repository *SQLiteRepository) executor(ctx context.Context) (sqliteExecutor, error) {
	if _, active := execution.Current(ctx); !active {
		return repository.database, nil
	}
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, errors.New("get local credential transaction handle")
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, errors.New("local credential execution uses a non-SQLite transaction handle")
	}
	return transaction, nil
}

func requireUser(ctx context.Context, executor sqliteExecutor, organizationID, userID string) error {
	var count int
	if err := executor.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE organization_id = ? AND id = ?`, organizationID, userID).Scan(&count); err != nil {
		return errors.New("read local credential user")
	}
	if count != 1 {
		return ErrUserNotFound
	}
	return nil
}

type credentialRow struct {
	metadata Metadata
	material hashMaterial
}

const credentialSelect = `SELECT
organization_id, user_id, revision, policy_version, algorithm, argon_version,
memory_kib, iterations, parallelism, salt, password_hash, created_at, updated_at
FROM iotd_local_user_credentials WHERE organization_id = ? AND user_id = ?`

func readCredential(ctx context.Context, executor sqliteExecutor, policies PolicySet, organizationID, userID string) (credentialRow, error) {
	var (
		row                                   credentialRow
		argonVersion, parallelism            int64
		memoryKiB, iterations, policyVersion int64
		createdAt, updatedAt                 string
	)
	err := executor.QueryRowContext(ctx, credentialSelect, organizationID, userID).Scan(
		&row.metadata.OrganizationID, &row.metadata.UserID, &row.metadata.Revision, &policyVersion,
		&row.metadata.Algorithm, &argonVersion, &memoryKiB, &iterations, &parallelism,
		&row.material.salt, &row.material.hash, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return credentialRow{}, ErrNotFound
	}
	if err != nil {
		return credentialRow{}, errors.New("read local credential")
	}
	if policyVersion < 1 || argonVersion < 0 || argonVersion > maxInt32Value || memoryKiB < 0 || memoryKiB > maxUint32Value || iterations < 0 || iterations > maxUint32Value || parallelism < 0 || parallelism > maxUint8Value {
		return credentialRow{}, ErrCredentialCorrupt
	}
	policy, err := policies.policy(policyVersion)
	if err != nil {
		return credentialRow{}, err
	}
	row.metadata.PolicyVersion = policyVersion
	row.metadata.ArgonVersion = int(argonVersion)
	row.metadata.MemoryKiB = uint32(memoryKiB)
	row.metadata.Iterations = uint32(iterations)
	row.metadata.Parallelism = uint8(parallelism)
	row.metadata.SaltLength = len(row.material.salt)
	row.metadata.HashLength = len(row.material.hash)
	row.material.policy = Policy{
		PolicyVersion: policyVersion,
		Algorithm:     row.metadata.Algorithm,
		ArgonVersion:  int(argonVersion),
		MemoryKiB:     uint32(memoryKiB),
		Iterations:    uint32(iterations),
		Parallelism:   uint8(parallelism),
		SaltLength:    uint32(len(row.material.salt)),
		HashLength:    uint32(len(row.material.hash)),
	}
	if row.material.policy != policy || !canonicalIdentifier(row.metadata.OrganizationID) || !canonicalIdentifier(row.metadata.UserID) || row.metadata.Revision < 1 {
		return credentialRow{}, ErrCredentialCorrupt
	}
	row.metadata.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return credentialRow{}, ErrCredentialCorrupt
	}
	row.metadata.UpdatedAt, err = parseTime(updatedAt)
	if err != nil || row.metadata.UpdatedAt.Before(row.metadata.CreatedAt) {
		return credentialRow{}, ErrCredentialCorrupt
	}
	return row, nil
}

func metadataWithExecutor(ctx context.Context, executor sqliteExecutor, policies PolicySet, organizationID, userID string) (Metadata, error) {
	row, err := readCredential(ctx, executor, policies, organizationID, userID)
	if err != nil {
		return Metadata{}, err
	}
	return row.metadata, nil
}

func canonicalIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 255
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02T15:04:05.000000000Z", value)
	if err != nil || parsed.Location() != time.UTC || formatTime(parsed) != value {
		return time.Time{}, fmt.Errorf("invalid local credential time")
	}
	return parsed, nil
}
