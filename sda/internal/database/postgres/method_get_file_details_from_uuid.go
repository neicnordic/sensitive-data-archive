package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getFileDetailsQuery = "getFileDetails"

func init() {
	queries[getFileDetailsQuery] = `
SELECT f.submission_user, f.submission_file_path
FROM sda.files f
JOIN sda.file_event_log fel on f.id = fel.file_id
WHERE f.id = $1 and fel.event = $2;
`
}

func (db *pgDb) getFileDetails(ctx context.Context, tx *sql.Tx, fileID, event string) (*database.FileDetails, error) {
	stmt := db.getPreparedStmt(tx, getFileDetailsQuery)

	info := new(database.FileDetails)
	if err := stmt.QueryRowContext(ctx, fileID, event).Scan(&info.User, &info.Path); err != nil {
		return nil, err
	}

	return info, nil
}
