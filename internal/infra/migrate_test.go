package infra

import (
	"os"
	"strings"
	"testing"
)

// The workspaces teardown is two statements from two helpers, and only one order works: DROP TABLE fails
// while links.workspace_id still points at the table through a foreign key. That order lives in Migrate,
// so it is pinned here - a tidy-up that moved the table drop above the column drop would pass every test
// and only fail on a deployed database.
func TestMigrateDropsTheWorkspaceTableAfterItsColumn(t *testing.T) {
	src, err := os.ReadFile("database.go")
	if err != nil {
		t.Fatalf("cannot read database.go: %v", err)
	}
	body := string(src)

	columns := strings.Index(body, "schema.ReconcileLegacyColumns(db)")
	tables := strings.Index(body, "schema.ReconcileLegacyTables(db)")
	if columns < 0 {
		t.Fatal("Migrate no longer reconciles legacy columns")
	}
	if tables < 0 {
		t.Fatal("Migrate no longer drops the tables of removed features")
	}
	if tables < columns {
		t.Error("Migrate drops the workspaces table before links.workspace_id, which the foreign key forbids")
	}
}
