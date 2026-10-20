package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	getObjectCountOfInboxLocationQuery   = "getObjectCountOfInboxLocation"
	getObjectCountOfArchiveLocationQuery = "getObjectCountOfArchiveLocation"
	getObjectCountOfBackupLocationQuery  = "getObjectCountOfBackupLocation"
)

// The queries assume that a location is used in only one role, so a bucket or directory is never
// both an inbox and an archive. They then count the same files as getSizeAndObjectCountOfLocation,
// and the archive and backup counts can be index-only scans.
func init() {
	queries[getObjectCountOfInboxLocationQuery] = `SELECT COUNT(*) FROM sda.files AS f
WHERE f.submission_location = $1 AND NOT EXISTS (SELECT 1 FROM sda.file_dataset fd WHERE fd.file_id = f.id);`
	queries[getObjectCountOfArchiveLocationQuery] = `SELECT COUNT(*) FROM sda.files WHERE archive_location = $1;`
	queries[getObjectCountOfBackupLocationQuery] = `SELECT COUNT(*) FROM sda.files WHERE backup_location = $1;`
}

func (db *pgDb) getObjectCountOfLocation(ctx context.Context, tx *sql.Tx, backend, location string) (uint64, error) {
	var queryName string
	switch backend {
	case "inbox":
		queryName = getObjectCountOfInboxLocationQuery
	case "archive":
		queryName = getObjectCountOfArchiveLocationQuery
	case "backup":
		queryName = getObjectCountOfBackupLocationQuery
	default:
		return 0, fmt.Errorf("no object count query for backend: %s", backend)
	}

	stmt, err := db.getPreparedStmt(tx, queryName)
	if err != nil {
		return 0, err
	}

	var count uint64
	if err := stmt.QueryRowContext(ctx, location).Scan(&count); err != nil {
		return 0, parsePQError(err)
	}

	return count, nil
}
