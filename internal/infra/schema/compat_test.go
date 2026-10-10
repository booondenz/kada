package schema

import (
	"os"
	"strings"
	"testing"
)

// Every statement must be re-runnable: the reconciliation is executed on every deploy, including the
// first one after a database has already been fixed.
func TestLegacyConstraintStatementsAreIdempotent(t *testing.T) {
	for _, stmt := range LegacyConstraintStatements() {
		if !strings.Contains(stmt, "IF EXISTS") {
			t.Errorf("statement is not idempotent, missing IF EXISTS: %s", stmt)
		}
		if !strings.HasPrefix(stmt, "ALTER TABLE ") || !strings.Contains(stmt, "DROP CONSTRAINT") {
			t.Errorf("unexpected statement shape: %s", stmt)
		}
	}
}

// The list drives a DROP, so a typo in a table or column name would silently do nothing (the guard is
// IF EXISTS) and leave the migration broken on the next run. Pin the full set.
func TestLegacyConstraintStatementsCoverEveryAffectedColumn(t *testing.T) {
	// Derived from the original raw-SQL migrations: they declared these UNIQUE, so PostgreSQL named the
	// constraints <table>_<column>_key, while the models declare them with `uniqueIndex`.
	want := []string{
		"users_phone_key",
		"users_email_key",
		"users_wechat_openid_key",
		"links_short_code_key",
		"workspaces_slug_key",
		"api_tokens_token_hash_key",
		"click_logs_event_id_key",
		"domains_user_id_name_key",
	}

	got := make(map[string]bool, len(want))
	for _, stmt := range LegacyConstraintStatements() {
		for _, name := range want {
			if strings.Contains(stmt, name) {
				got[name] = true
			}
		}
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("no statement drops the legacy constraint %s", name)
		}
	}
}

// The reconciliation targets PostgreSQL's generated constraint names, so it must not run any DDL on a
// different engine. Pinning the guard keeps a non-Postgres deployment from failing every migration on
// syntax it does not understand.
func TestReconcileIsPostgresOnly(t *testing.T) {
	src, err := os.ReadFile("compat.go")
	if err != nil {
		t.Fatalf("cannot read compat.go: %v", err)
	}
	body := string(src)

	guard := `if db.Name() != "postgres"`
	if !strings.Contains(body, guard) {
		t.Errorf("the schema helpers must return early for non-Postgres engines (expected %s)", guard)
	}
	// The guard has to come before any DDL is executed, otherwise it is useless.
	if strings.Index(body, guard) > strings.Index(body, "db.Exec(") {
		t.Error("the engine guard appears after the first db.Exec call")
	}
	if n := strings.Count(body, guard); n < 2 {
		t.Errorf("expected the engine guard on every DDL helper, found %d", n)
	}
}

// AutoMigrate can create tables and columns but not types, so a model field tagged `type:<something>`
// only works if that type is created beforehand. Forgetting one fails the whole migration on a database
// that has never run the original SQL migrations, which is how the click_platform enum was missed.
func TestEveryEnumColumnHasACreatedType(t *testing.T) {
	// The enum types the entity package references through a `type:` tag.
	referenced := map[string]bool{
		"click_platform": true,
	}

	created := make(map[string]bool)
	for _, stmt := range EnumTypeStatements() {
		for name := range referenced {
			if strings.Contains(stmt, "CREATE TYPE "+name) {
				created[name] = true
			}
		}
	}

	for name := range referenced {
		if !created[name] {
			t.Errorf("the models use the enum type %q but no statement creates it; "+
				"AutoMigrate cannot create types, so a fresh database will fail", name)
		}
	}
}

// Enum statements run on every start, so a plain CREATE TYPE would fail the second time. The DO block
// swallows duplicate_object instead.
func TestEnumTypeStatementsAreIdempotent(t *testing.T) {
	statements := EnumTypeStatements()
	if len(statements) == 0 {
		t.Fatal("expected at least one enum type statement")
	}
	for _, stmt := range statements {
		if !strings.Contains(stmt, "duplicate_object") {
			t.Errorf("statement is not idempotent, it does not handle duplicate_object: %s", stmt)
		}
		if !strings.Contains(stmt, "CREATE TYPE ") {
			t.Errorf("unexpected statement shape: %s", stmt)
		}
	}
}

// The column reconciliation runs on every deploy too, so it has to survive being run twice.
func TestLegacyColumnStatementsAreIdempotent(t *testing.T) {
	statements := LegacyColumnStatements()
	if len(statements) == 0 {
		t.Fatal("expected at least one legacy column statement")
	}
	for _, stmt := range statements {
		if !strings.Contains(stmt, "IF EXISTS") {
			t.Errorf("statement is not idempotent, missing IF EXISTS: %s", stmt)
		}
		if !strings.HasPrefix(stmt, "ALTER TABLE ") || !strings.Contains(stmt, "DROP COLUMN") {
			t.Errorf("unexpected statement shape: %s", stmt)
		}
	}
}

// sms_codes.code is the column that failed a live deployment: its leftover NOT NULL rejected every insert
// of a verification code, after the SMS had already been sent. Losing the statement silently would put the
// login flow straight back into "the code arrives and the page says error".
func TestLegacyColumnStatementsCoverTheSmsCodeColumn(t *testing.T) {
	want := []struct{ table, column string }{
		{"sms_codes", "code"},
	}

	joined := strings.Join(LegacyColumnStatements(), "\n")
	for _, w := range want {
		if !strings.Contains(joined, "TABLE "+w.table) || !strings.Contains(joined, "COLUMN IF EXISTS "+w.column) {
			t.Errorf("no statement drops the legacy column %s.%s", w.table, w.column)
		}
	}
}

// The action reconciliation is an UPDATE that runs on every deploy, so what makes it idempotent is its
// predicate: it matches the prefix the old endpoint wrote and clears that prefix in the same statement, so
// a second run finds nothing. Both halves are pinned here, along with the name being moved rather than
// dropped - the column it lands in is the point of the whole statement.
func TestLegacyActionStatementsAreIdempotentAndMoveTheName(t *testing.T) {
	statements := LegacyActionRefererStatements()
	if len(statements) == 0 {
		t.Fatal("expected the legacy action statement")
	}

	for _, stmt := range statements {
		if !strings.Contains(stmt, "WHERE referer LIKE 'action:%'") {
			t.Errorf("the statement does not target what the old endpoint wrote: %s", stmt)
		}
		if !strings.Contains(stmt, "referer = NULL") {
			t.Errorf("the statement leaves the action in the referer column, so it would run again: %s", stmt)
		}
		// 'action:' is seven characters, so the name starts at the eighth. Off by one here would store a
		// truncated action ("opy_link") and clear the referer, which is silent and final.
		if !strings.Contains(stmt, "substring(referer from 8)") {
			t.Errorf("the name is not taken from just after the prefix: %s", stmt)
		}
	}
}

// A row whose action name was empty must not land in the column as an empty string: `action IS NOT NULL` is
// how a report asks whether a row recorded an interaction, and ” would answer yes.
func TestLegacyActionStatementStoresNoEmptyName(t *testing.T) {
	for _, stmt := range LegacyActionRefererStatements() {
		if !strings.Contains(stmt, "NULLIF(") {
			t.Errorf("an empty action name would be stored as an empty string: %s", stmt)
		}
	}
}
