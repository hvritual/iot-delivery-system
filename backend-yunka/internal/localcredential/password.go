package localcredential

import "unicode/utf8"

// MinimumPasswordCharacters applies to newly enrolled or replaced passwords.
// Existing credential verification stays byte-exact and backward compatible.
const MinimumPasswordCharacters = 15
const MaxPasswordBytes = maxPasswordBytes

// ValidateNewPassword counts Unicode code points, not bytes or UTF-16 units.
// It never trims, truncates, normalizes or logs the submitted secret. Long
// passphrases and password-manager generated values need no composition rules.
func ValidateNewPassword(password []byte) error {
	if len(password) > MaxPasswordBytes || !utf8.Valid(password) || utf8.RuneCount(password) < MinimumPasswordCharacters {
		return ErrInvalidPassword
	}
	return nil
}
