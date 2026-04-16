package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const listUserDatasetsQuery = "listUserDatasets"

func init() {
	queries[listUserDatasetsQuery] = `
SELECT dataset_id, event, event_date 
FROM sda.dataset_event_log 
WHERE (dataset_id, event_date) IN (
	SELECT dataset_id,max(event_date) FROM sda.dataset_event_log WHERE
	dataset_id IN (
		SELECT stable_id FROM sda.datasets WHERE
		id IN (
			SELECT DISTINCT dataset_id FROM sda.file_dataset WHERE
			file_id IN (
				SELECT id FROM sda.files WHERE submission_user = $1 AND stable_id IS NOT NULL
			)
		)
	)
	GROUP BY dataset_id
);
`
}

func (db *pgDb) listUserDatasets(ctx context.Context, tx *sql.Tx, submissionUser string) ([]*database.DatasetInfo, error) {
	stmt := db.getPreparedStmt(tx, listUserDatasetsQuery)

	rows, err := stmt.QueryContext(ctx, submissionUser)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var datasets []*database.DatasetInfo
	for rows.Next() {
		di := new(database.DatasetInfo)
		if err := rows.Scan(&di.DatasetID, &di.Status, &di.Timestamp); err != nil {
			return nil, err
		}

		datasets = append(datasets, di)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return datasets, nil
}
