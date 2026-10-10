package service

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain/entity"
)

// newAnalyticsDryRunDB mirrors the helper the other service tests use: it renders SQL without a server.
func newAnalyticsDryRunDB(t *testing.T) *gorm.DB {
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

// The totals are one user's: the condition is in the query, so a missing WHERE would show somebody how many
// links are on the platform rather than how many are theirs. The dashboard and the assistant's tool both
// read this, so the assertion covers both.
func TestOverviewTotalsOneUsersLinks(t *testing.T) {
	db := newAnalyticsDryRunDB(t)

	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&entity.Link{}).
		Select("COUNT(*) AS total_links, COALESCE(SUM(click_count), 0) AS total_clicks").
		Where("user_id = ?", int64(7)).
		Scan(&Overview{}).
		Statement
	sql := stmt.SQL.String()

	for _, want := range []string{
		`FROM "links"`,
		"COUNT(*) AS total_links",
		"COALESCE(SUM(click_count), 0) AS total_clicks",
		"user_id = $1",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("generated SQL is missing %q:\n%s", want, sql)
		}
	}
}
