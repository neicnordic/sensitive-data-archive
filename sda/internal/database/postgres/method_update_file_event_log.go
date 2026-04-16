package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

const updateFileEventLogQuery = "updateFileEventLog"

func init() {
	queries[updateFileEventLogQuery] = `
INSERT INTO sda.file_event_log(file_id, event, user_id, details, message)
VALUES($1, $2, $3, $4, $5);
`
}
func (db *pgDb) updateFileEventLog(ctx context.Context, tx *sql.Tx, fileUUID, event, user, details, message string) error {
	stmt := db.getPreparedStmt(tx, updateFileEventLogQuery)

	result, err := stmt.ExecContext(ctx, fileUUID, event, user, details, message)
	if err != nil {
		// 23503 error code == foreign_key_violation, meaning the files row does not exist
		// http://www.postgresql.org/docs/9.3/static/errcodes-appendix.html
		var pqErr *pq.Error
		if ok := errors.As(err, &pqErr); ok && pqErr.Code == "23503" {
			return sql.ErrNoRows
		}

		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}
