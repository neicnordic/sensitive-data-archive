package postgres

import (
	"context"
	"database/sql"
)

const getUploadedSubmissionFilePathAndLocationQuery = "getUploadedSubmissionFilePathAndLocation"

func init() {
	queries[getUploadedSubmissionFilePathAndLocationQuery] = `
SELECT submission_file_path, submission_location
FROM sda.files
WHERE
  submission_user = $1
  AND id = $2
  AND EXISTS (
    SELECT 1
	  FROM (
	    SELECT event
	    FROM sda.file_event_log
	    WHERE file_id = $2
	    ORDER BY started_at DESC limit 1
	  ) AS subquery
	  WHERE event = 'uploaded' OR event = 'disabled'
    );
`
}

func (db *pgDb) getUploadedSubmissionFilePathAndLocation(ctx context.Context, tx *sql.Tx, submissionUser, fileID string) (string, string, error) {
	stmt := db.getPreparedStmt(tx, getUploadedSubmissionFilePathAndLocationQuery)

	var filePath string
	var location sql.NullString
	err := stmt.QueryRowContext(ctx, submissionUser, fileID).Scan(&filePath, &location)
	if err != nil {
		return "", "", err
	}
	// dont really care if location is valid, we just want null -> ""
	return filePath, location.String, nil
}
