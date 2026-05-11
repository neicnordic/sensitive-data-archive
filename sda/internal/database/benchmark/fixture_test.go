package benchmark

import (
	"context"
	"testing"
)

func TestFixtureBootsUp(t *testing.T) {
	if testDB == nil {
		t.Fatal("testDB not initialized by TestMain")
	}
	var schemaVersion int
	err := testDB.QueryRowContext(context.Background(),
		`SELECT MAX(version) FROM sda.dbschema_version`).Scan(&schemaVersion)
	if err != nil {
		t.Fatalf("schema query failed: %v", err)
	}
	if schemaVersion < 1 {
		t.Fatalf("unexpected schema version: %d", schemaVersion)
	}
}
