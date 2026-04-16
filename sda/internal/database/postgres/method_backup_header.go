package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

const backupHeaderQuery = "backupHeader"

func init() {
	queries[backupHeaderQuery] = `
INSERT INTO sda.file_headers_backup (file_id, header, key_hash, backup_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (file_id) DO UPDATE
SET header = EXCLUDED.header,
	key_hash = EXCLUDED.key_hash,
	backup_at = EXCLUDED.backup_at;
`
}

func (db *pgDb) backupHeader(ctx context.Context, tx *sql.Tx, fileID string, header []byte, keyHash string) error {
	stmt := db.getPreparedStmt(tx, backupHeaderQuery)

	result, err := stmt.ExecContext(ctx, fileID, hex.EncodeToString(header), keyHash)
	if err != nil {
		return fmt.Errorf("backupHeader error: %w", err)
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("failed to backup header: zero rows were inserted")
	}

	return nil
}
