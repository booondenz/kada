package infra

import (
	"context"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/chun/kada-backend/internal/domain/entity"
)

// This is the regression test for the two failures that only ever appeared on a real PostgreSQL server:
//
//  1. `type "click_platform" does not exist` on a fresh database, because AutoMigrate cannot create the
//     enum type the click_logs table references.
//  2. `constraint "uni_users_phone" does not exist` on a database created by the original raw-SQL
//     migrations, because the constraint names did not match what the models derive.
//
// Neither can be caught by a unit test: they are properties of the DDL that reaches the server. It runs
// only when KADA_TEST_DATABASE_URL points at a throwaway database, so `go test ./...` stays offline.
//
//	createdb -U postgres kada_spectest
//	KADA_TEST_DATABASE_URL='postgres://postgres:PASS@localhost:5432/kada_spectest?sslmode=disable' \
//	  go test ./internal/infra/schema/ -run Integration -v
//
// The test drops and recreates every table it touches, so it must never be pointed at a database whose
// contents matter.
func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	url := os.Getenv("KADA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KADA_TEST_DATABASE_URL is not set; skipping the PostgreSQL integration test")
	}

	db, err := gorm.Open(postgres.Open(url), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("cannot connect to KADA_TEST_DATABASE_URL: %v", err)
	}

	// Start from an empty schema, the state a fresh install is in.
	if err := db.Exec("DROP SCHEMA public CASCADE").Error; err != nil {
		t.Fatalf("failed to drop the public schema: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA public").Error; err != nil {
		t.Fatalf("failed to recreate the public schema: %v", err)
	}

	return db
}

// A fresh database must migrate without touching the legacy reconciliation, and a second run must be a
// no-op: this is what every deploy does.
func TestIntegrationMigrateFromScratchIsIdempotent(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	for attempt := 1; attempt <= 2; attempt++ {
		if err := Migrate(db); err != nil {
			t.Fatalf("Migrate failed on attempt %d: %v", attempt, err)
		}
	}

	// The enum type the click_logs column depends on.
	var typeCount int64
	if err := db.Raw(`SELECT count(*) FROM pg_type WHERE typname = 'click_platform'`).Scan(&typeCount).Error; err != nil {
		t.Fatalf("failed to look up the enum type: %v", err)
	}
	if typeCount != 1 {
		t.Errorf("expected the click_platform enum type to exist exactly once, found %d", typeCount)
	}

	// Every model must have its table.
	for _, model := range entity.Models() {
		if !db.WithContext(ctx).Migrator().HasTable(model) {
			t.Errorf("table for %T was not created", model)
		}
	}

	// The click_logs.platform column has to use the enum, not plain text.
	var dataType string
	err := db.Raw(`
		SELECT udt_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'click_logs' AND column_name = 'platform'
	`).Scan(&dataType).Error
	if err != nil {
		t.Fatalf("failed to inspect click_logs.platform: %v", err)
	}
	if dataType != "click_platform" {
		t.Errorf("click_logs.platform uses %q, want the click_platform enum", dataType)
	}

	// The legacy <table>_<column>_key constraints must be gone, replaced by the model's unique indexes.
	assertUniqueIndexExists(t, db, "users", []string{"phone"})
	assertUniqueIndexExists(t, db, "users", []string{"email"})
	assertUniqueIndexExists(t, db, "users", []string{"wechat_openid"})
	assertUniqueIndexExists(t, db, "links", []string{"short_code"})
	assertUniqueIndexExists(t, db, "workspaces", []string{"slug"})
	assertUniqueIndexExists(t, db, "api_tokens", []string{"token_hash"})
	assertUniqueIndexExists(t, db, "click_logs", []string{"event_id"})
	assertUniqueIndexExists(t, db, "domains", []string{"user_id", "name"})

	// No index may list the same column twice: that is what a field carrying both `index` and
	// `uniqueIndex` produced (`CREATE UNIQUE INDEX ... ON users (phone, phone)`).
	for _, index := range indexColumns(t, db) {
		seen := make(map[string]bool, len(index.columns))
		for _, column := range index.columns {
			if seen[column] {
				t.Errorf("index %s on %s lists column %s twice: %v",
					index.name, index.table, column, index.columns)
			}
			seen[column] = true
		}
	}
}

// A database created from the original raw-SQL migrations must converge to the same schema, without the
// migration aborting on a constraint name it cannot drop.
func TestIntegrationMigrateFromLegacySchema(t *testing.T) {
	db := integrationDB(t)

	// Recreate the legacy shape: inline UNIQUE, which PostgreSQL names <table>_<column>_key.
	legacy := []string{
		`CREATE TABLE users (
			id BIGSERIAL PRIMARY KEY,
			phone VARCHAR(20) UNIQUE,
			email VARCHAR(255) UNIQUE,
			wechat_openid VARCHAR(128) UNIQUE,
			wechat_unionid VARCHAR(128),
			name VARCHAR(100),
			avatar TEXT,
			password_hash VARCHAR(255),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_login_at TIMESTAMPTZ,
			last_login_ip VARCHAR(45)
		)`,
		`CREATE TABLE links (
			id BIGSERIAL PRIMARY KEY,
			short_code VARCHAR(20) UNIQUE NOT NULL,
			original_url TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE workspaces (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			slug VARCHAR(50) NOT NULL UNIQUE,
			user_id BIGINT NOT NULL REFERENCES users(id),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE api_tokens (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL,
			token_hash VARCHAR(64) NOT NULL UNIQUE,
			last_used TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TYPE click_platform AS ENUM ('browser','wechat','qq','weibo','xiaohongshu','sms','unknown')`,
		`CREATE TABLE click_logs (
			id BIGSERIAL PRIMARY KEY,
			link_id BIGINT REFERENCES links(id) ON DELETE CASCADE,
			ip VARCHAR(45),
			user_agent TEXT,
			platform click_platform DEFAULT 'unknown',
			referer TEXT,
			country VARCHAR(10),
			province VARCHAR(50),
			city VARCHAR(50),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			event_id VARCHAR(64) UNIQUE
		)`,
		`CREATE TABLE domains (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			verified BOOLEAN DEFAULT FALSE,
			verified_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(user_id, name)
		)`,
	}
	for _, stmt := range legacy {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to build the legacy schema: %v\n%s", err, stmt)
		}
	}

	// This is the call that used to fail with SQLSTATE 42704.
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate on a legacy schema failed: %v", err)
	}
	// And it must stay stable on the next run.
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate on a legacy schema failed: %v", err)
	}

	assertUniqueIndexExists(t, db, "users", []string{"phone"})
	assertUniqueIndexExists(t, db, "domains", []string{"user_id", "name"})

	// The legacy constraint names must be gone, so a later migration cannot trip over them again.
	var leftover int64
	if err := db.Raw(`
		SELECT count(*) FROM pg_constraint
		WHERE connamespace = 'public'::regnamespace
		  AND contype = 'u'
		  AND conname IN ('users_phone_key','users_email_key','users_wechat_openid_key',
		                  'links_short_code_key','workspaces_slug_key','api_tokens_token_hash_key',
		                  'click_logs_event_id_key','domains_user_id_name_key')
	`).Scan(&leftover).Error; err != nil {
		t.Fatalf("failed to count leftover legacy constraints: %v", err)
	}
	if leftover != 0 {
		t.Errorf("expected no legacy unique constraints to remain, found %d", leftover)
	}
}

type indexInfo struct {
	table   string
	name    string
	columns []string
}

// indexColumns reads every index on the public schema, with its columns in order, straight from the
// catalog so it reflects what the server actually stored.
func indexColumns(t *testing.T, db *gorm.DB) []indexInfo {
	t.Helper()

	rows, err := db.Raw(`
		SELECT t.relname AS table_name, i.relname AS index_name, a.attname AS column_name, k.ord
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE n.nspname = 'public'
		ORDER BY t.relname, i.relname, k.ord
	`).Rows()
	if err != nil {
		t.Fatalf("failed to read the index catalog: %v", err)
	}
	defer rows.Close()

	var (
		out     []indexInfo
		current *indexInfo
	)
	for rows.Next() {
		var table, name, column string
		var ord int
		if err := rows.Scan(&table, &name, &column, &ord); err != nil {
			t.Fatalf("failed to scan the index catalog: %v", err)
		}
		if current == nil || current.table != table || current.name != name {
			out = append(out, indexInfo{table: table, name: name})
			current = &out[len(out)-1]
		}
		current.columns = append(current.columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate the index catalog: %v", err)
	}
	return out
}

// assertUniqueIndexExists checks that a unique index covering exactly these columns exists.
func assertUniqueIndexExists(t *testing.T, db *gorm.DB, table string, columns []string) {
	t.Helper()

	want := strings.Join(columns, ",")
	for _, index := range uniqueIndexColumns(t, db) {
		if index.table == table && strings.Join(index.columns, ",") == want {
			return
		}
	}
	t.Errorf("no unique index on %s(%s)", table, want)
}

// uniqueIndexColumns returns only the unique indexes.
func uniqueIndexColumns(t *testing.T, db *gorm.DB) []indexInfo {
	t.Helper()

	var out []indexInfo
	for _, index := range indexColumns(t, db) {
		var isUnique bool
		err := db.Raw(`
			SELECT ix.indisunique FROM pg_index ix
			JOIN pg_class i ON i.oid = ix.indexrelid
			JOIN pg_namespace n ON n.oid = i.relnamespace
			WHERE n.nspname = 'public' AND i.relname = ?
		`, index.name).Scan(&isUnique).Error
		if err != nil {
			t.Fatalf("failed to check whether %s is unique: %v", index.name, err)
		}
		if isUnique {
			out = append(out, index)
		}
	}
	return out
}
