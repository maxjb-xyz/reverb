package auth

import "errors"

// MinPasswordLength is the minimum accepted length for a user-chosen password
// (first-run setup, signup, admin reset, and self-service change). It follows the
// NIST 800-63B guidance of an 8-character floor; deliberately no composition
// rules (mixed case / symbols), which that guidance discourages.
const MinPasswordLength = 8

// maxPasswordLength guards against bcrypt's silent 72-byte truncation: inputs
// longer than this are rejected rather than quietly having their tail ignored.
const maxPasswordLength = 72

var (
	// ErrPasswordTooShort is returned when a password is below MinPasswordLength.
	ErrPasswordTooShort = errors.New("password too short")
	// ErrPasswordTooLong is returned when a password exceeds bcrypt's usable length.
	ErrPasswordTooLong = errors.New("password too long")
)

// ValidatePassword enforces the password policy. It is the single gate every
// password-setting path funnels through, so the rule lives in exactly one place.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(pw) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}
