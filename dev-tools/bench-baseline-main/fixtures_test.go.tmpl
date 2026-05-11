package benchmark

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func truncateTables(tb testing.TB, db *sql.DB) {
	tb.Helper()
	if _, err := db.ExecContext(context.Background(),
		`TRUNCATE sda.files, sda.file_event_log, sda.checksums CASCADE`); err != nil {
		tb.Fatalf("truncate failed: %v", err)
	}
}

func seedFile(tb testing.TB, db *sql.DB) string {
	tb.Helper()
	n := nextSeedID()
	var id string
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO sda.files(
			id, submission_location, submission_file_path, submission_user,
			encryption_method, stable_id, header, archive_location, archive_file_path
		)
		VALUES(
			gen_random_uuid(), 'inbox', $1, 'bench-user',
			'CRYPT4GH', $2, $3, 'archive', $4
		)
		RETURNING id
	`,
		fmt.Sprintf("path/%d.c4gh", n),
		fmt.Sprintf("accession-%d", n),
		"00000000",
		fmt.Sprintf("archive/%d.c4gh", n),
	).Scan(&id)
	if err != nil {
		tb.Fatalf("seed insert failed: %v", err)
	}
	return id
}

var seedCounter int

func nextSeedID() int {
	seedCounter++
	return seedCounter
}
