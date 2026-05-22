package postgres

import (
	"context"
	"database/sql"
)

const isFileInDatasetQuery = "isFileInDataset"

func init() {
	queries[isFileInDatasetQuery] = `
SELECT EXISTS(SELECT 1 FROM sda.file_dataset WHERE file_id = $1);
`
}

func (db *pgDb) isFileInDataset(ctx context.Context, tx *sql.Tx, fileID string) (bool, error) {
	stmt, err := db.getPreparedStmt(tx, isFileInDatasetQuery)
	if err != nil {
		return false, err
	}

	var inDataset bool
	if err := stmt.QueryRowContext(ctx, fileID).Scan(&inDataset); err != nil {
		return false, err
	}

	return inDataset, nil
}
