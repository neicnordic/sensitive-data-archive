package database

import "errors"

// ErrInvalidCursor is returned when a pagination cursor cannot be decoded or parsed.
var ErrInvalidCursor = errors.New("invalid cursor")
