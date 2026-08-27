package database

import "errors"

var (
	// ErrInvalidCursor is returned when a pagination cursor cannot be decoded or parsed.
	ErrInvalidCursor = errors.New("invalid cursor")

	// ErrUniqueViolation indicates that an operation violates a uniqueness constraint.
	ErrUniqueViolation = errors.New("unique violation")

	// ErrNotNullViolation indicates that an operation attempts to store a null value where not allowed.
	// where one is not allowed.
	ErrNotNullViolation = errors.New("not-null violation")

	// ErrForeignKeyViolation indicates that an operation violates a foreign key constraint.
	ErrForeignKeyViolation = errors.New("foreign-key violation")
)
