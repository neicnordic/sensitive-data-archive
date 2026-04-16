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
	stmt := db.getPreparedStmt(tx, isFileInDatasetQuery)

	var inDataset bool
	err := stmt.QueryRowContext(ctx, fileID).Scan(&inDataset)

	return inDataset, err
}
