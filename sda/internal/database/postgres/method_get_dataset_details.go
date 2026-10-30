package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

const getDatasetDetailsQuery = "getDatasetDetails"

func init() {
	queries[getDatasetDetailsQuery] = `SELECT d.created_at, last_event.event, file_count.count
FROM sda.datasets AS d
   LEFT JOIN LATERAL (
	  SELECT COUNT(*) AS count
	  FROM sda.file_dataset
	  WHERE dataset_id = d.id
  ) AS file_count ON true
  LEFT JOIN LATERAL (
	  SELECT event
	  FROM sda.dataset_event_log
	  WHERE dataset_id = d.stable_id
	  ORDER BY id DESC
	  LIMIT 1
  ) AS last_event ON true
WHERE d.stable_id = $1;`
}

func (db *pgDb) getDatasetDetails(ctx context.Context, tx *sql.Tx, datasetID string) (*database.DatasetDetails, error) {
	stmt, err := db.getPreparedStmt(tx, getDatasetDetailsQuery)
	if err != nil {
		return nil, err
	}

	datasetDetails := &database.DatasetDetails{
		Status:        "invalid",
		CreatedAt:     "",
		NumberOfFiles: 0,
	}

	var status sql.NullString
	var nrFiles sql.NullInt64

	if err := stmt.QueryRowContext(ctx, datasetID).Scan(&datasetDetails.CreatedAt, &status, &nrFiles); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, parsePQError(err)
	}

	if status.Valid {
		datasetDetails.Status = status.String
	}
	if nrFiles.Valid {
		v := nrFiles.Int64
		if v < 0 {
			datasetDetails.NumberOfFiles = 0
		} else {
			datasetDetails.NumberOfFiles = uint64(nrFiles.Int64)
		}
	}

	return datasetDetails, nil
}
