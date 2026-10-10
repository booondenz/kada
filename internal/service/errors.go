package service

import (
	"errors"

	"gorm.io/gorm"
)

// isDuplicateKey reports whether the error is a unique-constraint violation.
//
// gorm.Config{TranslateError: true} wraps the PostgreSQL 23505 SQLSTATE into gorm.ErrDuplicatedKey while
// keeping the driver error in the chain, so errors.Is works whether or not translation happened.
func isDuplicateKey(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey)
}
