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
	stmt := db.getPreparedStmt(tx, getArchivePathAndLocationQuery)

	var archivePath string
	var archiveLocation sql.NullString
	err := stmt.QueryRowContext(ctx, accessionID).Scan(&archivePath, &archiveLocation)
	if err != nil {
		return "", "", err
	}

	return archivePath, archiveLocation.String, nil
}
