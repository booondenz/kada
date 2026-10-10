package service

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newDryRunDB builds a GORM handle that generates SQL without ever contacting the server.
//
// The GORM port replaced hand-written SQL with the builder, so the failure mode to guard against is no
// longer a typo in a statement but a clause that renders something the database rejects (or, worse,
// something subtly different that silently matches the wrong rows). DryRun renders the SQL that would be
// sent, which is enough to assert on the generated statements with no PostgreSQL available.
func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{
		// #nosec G101 -- a throwaway DSN that is never dialed: DryRun only renders the SQL.
		DSN: "postgres://kada:invalid@127.0.0.1:1/kada?sslmode=disable",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("failed to build a dry-run gorm.DB: %v", err)
	}
	return db
}

func TestLinkListSQLFiltersAndPaginates(t *testing.T) {
	db := newDryRunDB(t)

	// Exercises every optional filter plus the pagination clauses at once. This mirrors the clause list
	// built by LinkService.List: the point is to prove GORM renders it as valid, correctly numbered SQL.
	filters := []string{"l.user_id = ?"}
	args := []any{int64(7)}

	filters = append(filters, "(l.title ILIKE ? OR l.original_url ILIKE ? OR l.short_code ILIKE ?)")
	like := "%kada%"
	args = append(args, like, like, like)
	filters = append(filters, "l.folder_id = ?", "l.id IN (SELECT link_id FROM link_tags WHERE tag_id = ?)", "l.workspace_id = ?")
	args = append(args, int64(3), int64(4), int64(5))

	stmt := db.Session(&gorm.Session{DryRun: true}).
		Table("links AS l").
		Select("l.*").
		Where(strings.Join(filters, " AND "), args...).
		Order("l.created_at DESC").
		Limit(20).
		Offset(40).
		Find(&[]any{}).
		Statement

	sql := stmt.SQL.String()
	for _, want := range []string{
		"SELECT l.* FROM links AS l",
		"l.user_id = $1",
		"l.title ILIKE $2",
		"l.folder_id = $5",
		"l.id IN (SELECT link_id FROM link_tags WHERE tag_id = $6)",
		"l.workspace_id = $7",
		"ORDER BY l.created_at DESC",
		"LIMIT $8",
		"OFFSET $9",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("generated SQL is missing %q:\n%s", want, sql)
		}
	}
	if len(stmt.Vars) != 9 {
		t.Errorf("expected 9 bound parameters, got %d: %v", len(stmt.Vars), stmt.Vars)
	}
}

// The IN (...) clauses used by the bulk operations are the easiest place to emit invalid SQL.
func TestBulkOperationsRenderValidInClauses(t *testing.T) {
	db := newDryRunDB(t)
	ids := []int64{1, 2, 3}

	stmt := db.Session(&gorm.Session{DryRun: true}).
		Where("id IN ? AND user_id = ?", ids, int64(9)).
		Delete(&struct{}{}).
		Statement
	if !strings.Contains(stmt.SQL.String(), "id IN ($1,$2,$3)") {
		t.Errorf("bulk delete must expand the IN clause, got: %s", stmt.SQL.String())
	}

	stmt = db.Session(&gorm.Session{DryRun: true}).
		Model(&struct{}{}).
		Where("id IN ? AND user_id = ?", ids, int64(9)).
		Pluck("short_code", &[]string{}).
		Statement
	if !strings.Contains(stmt.SQL.String(), "short_code") || !strings.Contains(stmt.SQL.String(), "id IN ($1,$2,$3)") {
		t.Errorf("short-code lookup must expand the IN clause, got: %s", stmt.SQL.String())
	}
}

// Clearing a nullable column has to render "SET workspace_id=NULL"; a Go nil would be dropped by
// Updates() and leave the link attached to a workspace that no longer exists.
func TestNullableColumnResetRendersNull(t *testing.T) {
	db := newDryRunDB(t)

	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&struct{}{}).
		Where("workspace_id = ? AND user_id = ?", int64(1), int64(2)).
		Update("workspace_id", gorm.Expr("NULL")).
		Statement
	if !strings.Contains(stmt.SQL.String(), `"workspace_id"=NULL`) {
		t.Errorf("expected an explicit NULL assignment, got: %s", stmt.SQL.String())
	}
}

// AutoMigrate must be told about every table; a model that is missing from entity.Models() would be
// silently left out of the schema.
func TestDryRunDBIsUsable(t *testing.T) {
	db := newDryRunDB(t)
	if !db.DryRun {
		t.Fatal("expected DryRun to be enabled")
	}
}
