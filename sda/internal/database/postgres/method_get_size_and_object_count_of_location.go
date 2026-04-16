package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const getSizeAndObjectCountOfLocationQuery = "getSizeAndObjectCountOfLocation"

func init() {
	queries[getSizeAndObjectCountOfLocationQuery] = `
SELECT SUM(CASE WHEN f.submission_location = $1 AND f.file_in_dataset IS NOT TRUE THEN f.submission_file_size ELSE f.archive_file_size END ) AS size, COUNT(*)
FROM (
SELECT f.submission_file_size, f.archive_file_size, f.submission_location, f.archive_location, f.backup_location,
      (EXISTS (SELECT 1
         FROM sda.file_dataset fd
         WHERE fd.file_id = f.id)
      ) AS file_in_dataset
  FROM sda.files AS f
) as f
WHERE (f.submission_location = $1 AND f.file_in_dataset IS NOT TRUE) OR f.archive_location = $1 OR f.backup_location = $1;
`
}

func (db *pgDb) getSizeAndObjectCountOfLocation(ctx context.Context, tx *sql.Tx, location string) (uint64, uint64, error) {
	stmt := db.getPreparedStmt(tx, getSizeAndObjectCountOfLocationQuery)

	var size, count sql.Null[uint64]
	if err := stmt.QueryRowContext(ctx, location).Scan(&size, &count); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, nil
		}

		return 0, 0, err
	}

	return size.V, count.V, nil
}
