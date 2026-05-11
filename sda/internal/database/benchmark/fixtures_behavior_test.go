package benchmark

import (
	"context"
	"testing"
)

func TestSeedAndTruncate(t *testing.T) {
	ctx := context.Background()
	truncateTables(t, testDB)

	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ids[i] = seedFile(t, testDB)
	}
	for _, id := range ids {
		if id == "" {
			t.Fatalf("seedFile returned empty ID")
		}
	}

	var count int
	if err := testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM sda.files`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 files, got %d", count)
	}

	truncateTables(t, testDB)
	if err := testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM sda.files`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 after truncate, got %d", count)
	}
}
