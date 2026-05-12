package postgres

import (
	"context"
	"database/sql"
)

const getFileEventsQuery = "getFileEvents"

func init() {
	queries[getFileEventsQuery] = "SELECT title from sda.file_events"
}

func (db *pgDb) getFileEvents(ctx context.Context, tx *sql.Tx) ([]string, error) {
	stmt, err := db.getPreparedStmt(tx, getFileEventsQuery)
	if err != nil {
		return nil, err
	}

	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, err
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	var fileEvents []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		fileEvents = append(fileEvents, title)
	}

	return fileEvents, nil
}
