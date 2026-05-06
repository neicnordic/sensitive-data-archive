package postgres

import (
	"context"
	"database/sql"
)

const getDatasetStatusQuery = "getDatasetStatus"

func init() {
	queries[getDatasetStatusQuery] = `
SELECT event 
FROM sda.dataset_event_log 
WHERE dataset_id = $1 
ORDER BY id DESC LIMIT 1;
`
}
func (db *pgDb) getDatasetStatus(ctx context.Context, tx *sql.Tx, datasetID string) (string, error) {
	stmt, err := db.getPreparedStmt(tx, getDatasetStatusQuery)
	if err != nil {
		return "", err
	}

	var status string

	if err := stmt.QueryRowContext(ctx, datasetID).Scan(&status); err != nil {
		return "", err
	}

	return status, nil
}
