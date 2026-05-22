package postgres

import (
	"context"
	"database/sql"
	"errors"
)

const getArchiveLocationQuery = "getArchiveLocation"

func init() {
	queries[getArchiveLocationQuery] = `
SELECT archive_location 
FROM sda.files 
WHERE id = $1;
`
}

func (db *pgDb) getArchiveLocation(ctx context.Context, tx *sql.Tx, fileID string) (string, error) {
	stmt, err := db.getPreparedStmt(tx, getArchiveLocationQuery)
	if err != nil {
		return "", err
	}

	var archiveLocation sql.NullString
	if err := stmt.QueryRowContext(ctx, fileID).Scan(&archiveLocation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}

		return "", err
	}

	return archiveLocation.String, nil
}
