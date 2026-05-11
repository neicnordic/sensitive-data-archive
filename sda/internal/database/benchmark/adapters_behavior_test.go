package benchmark

import (
	"context"
	"testing"
)

func TestAdapterWithPrep_UpdateFileEventLog(t *testing.T) {
	truncateTables(t, testDB)
	fileID := seedFile(t, testDB)

	a := newAdapter(t, testDB, "prep")
	defer a.Close()

	if err := a.exec(context.Background(), "updateFileEventLog",
		fileID, "uploaded", "bench-user", nil, nil); err != nil {
		t.Fatalf("prep adapter exec: %v", err)
	}

	var count int
	if err := testDB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM sda.file_event_log WHERE file_id = $1 AND event = 'uploaded'`,
		fileID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 uploaded event, got %d", count)
	}
}

func TestAdapterWithoutPrep_UpdateFileEventLog(t *testing.T) {
	truncateTables(t, testDB)
	fileID := seedFile(t, testDB)

	a := newAdapter(t, testDB, "noprep")
	defer a.Close()

	if err := a.exec(context.Background(), "updateFileEventLog",
		fileID, "uploaded", "bench-user", nil, nil); err != nil {
		t.Fatalf("noprep adapter exec: %v", err)
	}

	var count int
	if err := testDB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM sda.file_event_log WHERE file_id = $1`,
		fileID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}
