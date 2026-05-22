package postgres

import (
	"context"
	"database/sql"
)

const getArchivePathAndLocationQuery = "getArchivePathAndLocation"

func init() {
	queries[getArchivePathAndLocationQuery] = `
SELECT archive_file_path, archive_location 
FROM sda.files 
WHERE stable_id = $1;
`
}

func (db *pgDb) getArchivePathAndLocation(ctx context.Context, tx *sql.Tx, accessionID string) (string, string, error) {
	stmt, err := db.getPreparedStmt(tx, getArchivePathAndLocationQuery)
	if err != nil {
		return "", "", err
	}

	var archivePath string
	var archiveLocation sql.NullString

	if err := stmt.QueryRowContext(ctx, accessionID).Scan(&archivePath, &archiveLocation); err != nil {
		return "", "", err
	}

	return archivePath, archiveLocation.String, nil
}
