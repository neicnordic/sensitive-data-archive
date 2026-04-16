package postgres

import (
	"context"
	"database/sql"
)

const getDecryptedChecksumQuery = "getDecryptedChecksum"

func init() {
	queries[getDecryptedChecksumQuery] = `
SELECT checksum 
FROM sda.checksums 
WHERE file_id = $1 
AND source = 'UNENCRYPTED';
`
}

func (db *pgDb) getDecryptedChecksum(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	stmt := db.getPreparedStmt(tx, getDecryptedChecksumQuery)

	var unencryptedChecksum string
	if err := stmt.QueryRowContext(ctx, id).Scan(&unencryptedChecksum); err != nil {
		return "", err
	}

	return unencryptedChecksum, nil
}
