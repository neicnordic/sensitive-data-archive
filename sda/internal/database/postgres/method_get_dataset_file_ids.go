package postgres

import (
	"context"
	"database/sql"
)

const getDatasetFileIDsQuery = "getDatasetFileIDs"

func init() {
	queries[getDatasetFileIDsQuery] = `
SELECT fd.file_id 
FROM sda.datasets AS d 
INNER JOIN sda.file_dataset AS fd ON d.id = fd.dataset_id 
WHERE d.stable_id = $1;
`
}

func (db *pgDb) getDatasetFileIDs(ctx context.Context, tx *sql.Tx, datasetID string) ([]string, error) {
	stmt := db.getPreparedStmt(tx, getDatasetFileIDsQuery)

	var fileIDs []string
	rows, err := stmt.QueryContext(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var fileID string
		err := rows.Scan(&fileID)
		if err != nil {
			return nil, err
		}

		fileIDs = append(fileIDs, fileID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return fileIDs, nil
}
