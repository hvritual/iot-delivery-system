package localcredential

import "context"

// RehashPassword upgrades only an already verified, unchanged credential.
// It is not a bypass for enrolling weak passwords: the supplied secret must
// match the existing record, its CAS revision and a retired hash profile.
func (repository *SQLiteRepository) RehashPassword(ctx context.Context, organizationID, userID string, password []byte, expectedRevision int64) (Metadata, error) {
	verification, err := repository.VerifyPassword(ctx, organizationID, userID, password)
	if err != nil {
		return Metadata{}, err
	}
	if !verification.Match {
		return Metadata{}, ErrInvalidPassword
	}
	if verification.Revision != expectedRevision {
		return Metadata{}, ErrRevisionConflict
	}
	if !verification.NeedsRehash {
		return Metadata{}, ErrUnsupportedCredential
	}
	return repository.setPassword(ctx, organizationID, userID, password, expectedRevision)
}
