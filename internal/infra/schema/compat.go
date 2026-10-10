// Package schema holds the schema evolution helpers that sit around GORM AutoMigrate.
package schema

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

// legacyUniqueConstraint is a unique constraint that an older revision of this project created under a
// different name than the GORM model expects in its `uniqueIndex` tag.
//
// The original schema was written as raw SQL (`phone VARCHAR(20) UNIQUE`), so PostgreSQL generated the
// name (users_phone_key). GORM instead derives uni_users_phone from the model. On a database that was
// created before the move to AutoMigrate, MigrateColumnUnique sees a column-level unique constraint that
// the model does not declare with the `unique` tag, concludes it is a leftover, and runs
// ALTER TABLE ... DROP CONSTRAINT uni_users_phone - which does not exist, aborting the whole migration
// with SQLSTATE 42704.
type legacyUniqueConstraint struct {
	Table     string
	Column    string
	LegacySQL string
}

// legacyUniqueConstraints lists every (table, column) pair where AutoMigrate and the original raw-SQL
// schema disagree about the constraint name.
//
// Only additive-free cases belong here: the list is used to DROP a legacy unique constraint so that
// AutoMigrate can recreate it under the name the model declares. Dropping a unique constraint whose
// column is also covered by a primary key would be wrong, so those are not listed.
var legacyUniqueConstraints = []legacyUniqueConstraint{
	{Table: "users", Column: "phone", LegacySQL: "ALTER TABLE users DROP CONSTRAINT IF EXISTS users_phone_key"},
	{Table: "users", Column: "email", LegacySQL: "ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key"},
	{Table: "users", Column: "wechat_openid", LegacySQL: "ALTER TABLE users DROP CONSTRAINT IF EXISTS users_wechat_openid_key"},
	{Table: "links", Column: "short_code", LegacySQL: "ALTER TABLE links DROP CONSTRAINT IF EXISTS links_short_code_key"},
	{Table: "workspaces", Column: "slug", LegacySQL: "ALTER TABLE workspaces DROP CONSTRAINT IF EXISTS workspaces_slug_key"},
	{Table: "api_tokens", Column: "token_hash", LegacySQL: "ALTER TABLE api_tokens DROP CONSTRAINT IF EXISTS api_tokens_token_hash_key"},
	{Table: "click_logs", Column: "event_id", LegacySQL: "ALTER TABLE click_logs DROP CONSTRAINT IF EXISTS click_logs_event_id_key"},
	// The original 000006 migration declared UNIQUE(user_id, name) on domains, which PostgreSQL named
	// domains_user_id_name_key. The model uses the composite index idx_domains_user_name instead, so the
	// legacy constraint has to go or AutoMigrate tries to add a second unique rule for the same columns.
	{Table: "domains", Column: "name", LegacySQL: "ALTER TABLE domains DROP CONSTRAINT IF EXISTS domains_user_id_name_key"},
}

// enumTypes are the PostgreSQL enum types the models reference with `type:<name>`.
//
// AutoMigrate creates tables and columns, but it cannot create a type: a model field tagged
// `type:click_platform` fails with `type "click_platform" does not exist` (SQLSTATE 42704) on a database
// that has never seen the original raw-SQL migrations, which is exactly what happens on a fresh install.
// The type therefore has to be created before AutoMigrate runs.
//
// DO blocks are used so that every statement is idempotent: they are re-run on every startup and on
// every deploy. See internal/domain/entity for the columns that reference each type.
var enumTypes = []string{
	`DO $$ BEGIN
		CREATE TYPE click_platform AS ENUM ('browser', 'wechat', 'qq', 'weibo', 'xiaohongshu', 'sms', 'unknown');
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,
}

// EnumTypeStatements returns the idempotent DDL that creates the enum types the models reference.
func EnumTypeStatements() []string {
	return append([]string(nil), enumTypes...)
}

// EnsureEnumTypes creates the PostgreSQL enum types the models depend on.
//
// It must run BEFORE AutoMigrate. On an existing database the types are already there and the DO blocks
// are no-ops; on a fresh one they are the only way the click_logs table can be created at all.
func EnsureEnumTypes(db *gorm.DB) error {
	if db.Name() != "postgres" {
		// The CREATE TYPE ... AS ENUM syntax below is PostgreSQL-specific.
		return nil
	}

	for _, stmt := range enumTypes {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("failed to create an enum type: %w", err)
		}
	}

	log.Println("PostgreSQL enum types ensured")
	return nil
}

// LegacyConstraintStatements returns the idempotent DDL that reconciles the constraint naming of a
// database created from the original raw-SQL migrations with the current GORM models.
//
// It is exported so it can be asserted in tests without a database, and it is safe to run repeatedly.
func LegacyConstraintStatements() []string {
	statements := make([]string, 0, len(legacyUniqueConstraints))
	for _, c := range legacyUniqueConstraints {
		statements = append(statements, c.LegacySQL)
	}
	return statements
}

// ReconcileLegacyConstraints drops the unique constraints that the pre-GORM schema created under
// PostgreSQL's generated names, so that AutoMigrate can recreate them with the names the models declare.
//
// It must run BEFORE AutoMigrate. Every statement is guarded with IF EXISTS, so a database that has
// already been reconciled, or one created from scratch by AutoMigrate, is left untouched.
func ReconcileLegacyConstraints(db *gorm.DB) error {
	if db.Name() != "postgres" {
		// The generated names these statements target are PostgreSQL-specific.
		return nil
	}

	// Only touch a table that exists: on a fresh database there is nothing to reconcile, and skipping the
	// statements keeps the "create from scratch" path free of error noise.
	migrator := db.Migrator()
	for _, c := range legacyUniqueConstraints {
		if !migrator.HasTable(c.Table) {
			continue
		}
		if err := db.Exec(c.LegacySQL).Error; err != nil {
			return fmt.Errorf("failed to drop legacy unique constraint on %s.%s: %w", c.Table, c.Column, err)
		}
	}

	log.Println("Legacy schema constraints reconciled")
	return nil
}

// legacyColumn is a column the original raw-SQL schema created with a rule the current models no longer
// satisfy. AutoMigrate is additive: it adds what the models declare and never relaxes or removes what they
// left behind, so a stale NOT NULL stays in the database and rejects every insert.
type legacyColumn struct {
	Table     string
	Column    string
	LegacySQL string
}

// legacyColumns lists the columns to remove for that reason.
//
// sms_codes.code is the one that broke a live deployment. It held the plaintext verification code back
// when codes were stored as text; the service now stores only sha256(code) and never writes this column,
// so the leftover NOT NULL rejected every insert with SQLSTATE 23502. That insert happens *after* the SMS
// has been sent, which is what made the failure so confusing: the recipient got a code, the page answered
// with an error, and because no row was written the per-phone cooldown could never match one, so every
// retry went straight to the provider until it answered biz.FREQUENCY. Dropping the column rather than
// relaxing it to nullable also makes the table match what the privacy page promises: no plaintext code is
// persisted.
var legacyColumns = []legacyColumn{
	{Table: "sms_codes", Column: "code", LegacySQL: "ALTER TABLE sms_codes DROP COLUMN IF EXISTS code"},
}

// LegacyColumnStatements returns the idempotent DDL that removes the columns the models no longer write.
//
// It is exported so it can be asserted in tests without a database, and it is safe to run repeatedly.
func LegacyColumnStatements() []string {
	statements := make([]string, 0, len(legacyColumns))
	for _, c := range legacyColumns {
		statements = append(statements, c.LegacySQL)
	}
	return statements
}

// ReconcileLegacyColumns drops columns whose leftover rules reject the inserts the models perform.
//
// It must run BEFORE AutoMigrate, so that the table matches its model by the time AutoMigrate compares the
// two. Every statement is guarded with IF EXISTS, so a database that never had the legacy schema is left
// untouched.
func ReconcileLegacyColumns(db *gorm.DB) error {
	if db.Name() != "postgres" {
		// SQLite cannot drop a column at all, so these statements are PostgreSQL-only.
		return nil
	}

	// Only touch a table that exists, for the same reason as the constraint reconciliation.
	migrator := db.Migrator()
	for _, c := range legacyColumns {
		if !migrator.HasTable(c.Table) {
			continue
		}
		if err := db.Exec(c.LegacySQL).Error; err != nil {
			return fmt.Errorf("failed to drop legacy column %s.%s: %w", c.Table, c.Column, err)
		}
	}

	log.Println("Legacy schema columns reconciled")
	return nil
}

// legacyActionRefererSQL moves the guide page action out of the referer column, where it never belonged.
//
// The action endpoint used to store it as "action:copy_link" in `referer`, so a report that reads referers
// had to know that some of its rows were not referers at all. The column for it exists now, and this is the
// rows that predate it: the name is moved across and the referer is cleared.
//
// The WHERE clause is the whole guard. It matches the prefix the old code wrote and nothing else, so a
// genuine referer - a URL, starting with http - cannot be touched, and because the statement clears the
// referer it also cannot match a second time. That is what makes it safe to run on every deploy.
//
// `kind` is deliberately left alone. Those rows moved a denormalized counter when they were written
// (links.click_count), and that number cannot be recomputed from the rows, so reclassifying them now would
// put the rows and the counter they fed into disagreement.
const legacyActionRefererSQL = `
	UPDATE click_logs
	SET action = NULLIF(substring(referer from 8), ''), referer = NULL
	WHERE referer LIKE 'action:%'
`

// LegacyActionRefererStatements returns the statement that empties the action names out of the referer
// column. It is exported so it can be asserted in tests without a database.
func LegacyActionRefererStatements() []string {
	return []string{legacyActionRefererSQL}
}

// ReconcileLegacyActions moves the action names the old endpoint left in `referer` into the `action` column.
//
// It must run AFTER AutoMigrate: it writes to a column AutoMigrate is what creates.
func ReconcileLegacyActions(db *gorm.DB) error {
	if db.Name() != "postgres" {
		// The string functions are PostgreSQL's; there is nothing to reconcile on another engine.
		return nil
	}
	if !db.Migrator().HasTable("click_logs") {
		return nil
	}

	res := db.Exec(legacyActionRefererSQL)
	if res.Error != nil {
		return fmt.Errorf("failed to move the legacy actions out of the referer column: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		log.Printf("Moved %d legacy guide page actions out of the referer column", res.RowsAffected)
	}
	return nil
}
