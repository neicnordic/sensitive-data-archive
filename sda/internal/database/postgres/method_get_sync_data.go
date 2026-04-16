package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getSyncDataQuery = "getSyncData"

func init() {
	queries[getSyncDataQuery] = `
SELECT f.submission_user, f.submission_file_path, cs.checksum
FROM sda.files AS f
INNER JOIN sda.checksums AS cs ON f.id = cs.file_id
WHERE f.stable_id = $1 AND cs.source = 'UNENCRYPTED';
`
}

func (db *pgDb) getSyncData(ctx context.Context, tx *sql.Tx, accessionID string) (*database.SyncData, error) {
	stmt := db.getPreparedStmt(tx, getSyncDataQuery)

	data := new(database.SyncData)
	if err := stmt.QueryRowContext(ctx, accessionID).Scan(&data.User, &data.FilePath, &data.Checksum); err != nil {
		return nil, err
	}

	return data, nil
}
