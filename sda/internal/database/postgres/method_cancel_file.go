package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	cancelFileUnSetArchivedQuery   = "cancelFileUnSetArchived"
	cancelFileDeleteChecksumsQuery = "cancelFileDeleteChecksums"
)

func init() {
	queries[cancelFileUnSetArchivedQuery] = `
UPDATE sda.files 
SET archive_location = NULL, archive_file_path = '', archive_file_size = NULL, decrypted_file_size = NULL, stable_id = NULL
WHERE id = $1;
`
	queries[cancelFileDeleteChecksumsQuery] = `
DELETE FROM sda.checksums 
WHERE file_id = $1
`
}

func (db *pgDb) cancelFile(ctx context.Context, tx *sql.Tx, fileID string, message string) error {
	unsetArchivedStmt := db.getPreparedStmt(tx, cancelFileUnSetArchivedQuery)

	deleteChecksumsStmt := db.getPreparedStmt(tx, cancelFileDeleteChecksumsQuery)

	logFileEventStmt := db.getPreparedStmt(tx, updateFileEventLogQuery)

	if _, err := unsetArchivedStmt.ExecContext(ctx, fileID); err != nil {
		return fmt.Errorf("failed to unset file data (file-id: %s): %v", fileID, err)
	}

	if _, err := deleteChecksumsStmt.ExecContext(ctx, fileID); err != nil {
		return fmt.Errorf("failed to delete checksums (file-id: %s): %v", fileID, err)
	}

	if _, err := logFileEventStmt.ExecContext(ctx, fileID, "disabled", "system", "{\"reason\": \"file cancelled\"}", message); err != nil {
		return fmt.Errorf("failed to log cancel file event (file-id: %s): %v", fileID, err)
	}

	return nil
}
