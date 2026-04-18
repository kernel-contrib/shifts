package internal

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// ── Error helpers ─────────────────────────────────────────────────────────────

// IsNotFoundErr checks whether the error chain contains gorm.ErrRecordNotFound.
func IsNotFoundErr(err error) bool {
	return err != nil && errors.Is(err, gorm.ErrRecordNotFound)
}

// IsDuplicateError detects unique-constraint violations across both
// PostgreSQL (SQLSTATE 23505) and SQLite (UNIQUE constraint failed).
func IsDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	if containsErrCode(err, "23505") {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "unique constraint")
}

func containsErrCode(err error, code string) bool {
	type pgErr interface{ SQLState() string }
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == code
	}
	return false
}
