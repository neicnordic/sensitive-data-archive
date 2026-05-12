package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getFileStatusHistoryQuery = "getFileStatus"

func init() {
	queries[getFileStatusHistoryQuery] = `
SELECT event, user_id, details, message, started_at
FROM sda.file_event_log 
WHERE file_id = $1 
`
}

func (db *pgDb) getFileStatusHistory(ctx context.Context, tx *sql.Tx, fileID string) ([]database.FileStatus, error) {
	stmt, err := db.getPreparedStmt(tx, getFileStatusHistoryQuery)
	if err != nil {
		return nil, err
	}

	rows, err := stmt.QueryContext(ctx, fileID)
	if err != nil {
		return nil, err
	}

	var fileInfo []database.FileStatus
	for rows.Next() {
		var f database.FileStatus
		if err := rows.Scan(&f.Event, &f.User, &f.Details, &f.Message, &f.CreatedAt); err != nil {
			return nil, err
		}
		fileInfo = append(fileInfo, f)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return fileInfo, nil
}
