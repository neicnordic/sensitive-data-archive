package postgres

import (
	"context"
	"database/sql"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const listDatasetsQuery = "listDatasets"

func init() {
	queries[listDatasetsQuery] = `
SELECT dataset_id, event, event_date 
FROM sda.dataset_event_log 
WHERE (dataset_id, event_date) IN (
	SELECT dataset_id, max(event_date) 
	FROM sda.dataset_event_log 
	GROUP BY dataset_id
	);
`
}
func (db *pgDb) listDatasets(ctx context.Context, tx *sql.Tx) ([]*database.DatasetInfo, error) {
	stmt := db.getPreparedStmt(tx, listDatasetsQuery)

	var datasets []*database.DatasetInfo
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		di := new(database.DatasetInfo)
		err := rows.Scan(&di.DatasetID, &di.Status, &di.Timestamp)
		if err != nil {
			return nil, err
		}

		datasets = append(datasets, di)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return datasets, nil
}
