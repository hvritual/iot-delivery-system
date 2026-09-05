package localcredential

import "errors"

// VerifyPasswordAgainstSyntheticCredential consumes the current password-hash
// work factor without reading or writing a real credential. Authentication
// boundaries use it when a User/credential is absent so negative outcomes do
// not create an obvious cheap timing path. The result is never a match.
func (repository *SQLiteRepository) VerifyPasswordAgainstSyntheticCredential(password []byte) error {
	if err := repository.ready(); err != nil {
		return err
	}
	material, err := repository.policies.hashPassword(password, repository.random)
	if err != nil {
		if errors.Is(err, ErrInvalidPassword) {
			return ErrInvalidPassword
		}
		return err
	}
	zeroBytes(material.salt)
	zeroBytes(material.hash)
	return nil
}
