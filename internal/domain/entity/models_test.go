package entity

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// parse builds the GORM schema for a model with the default naming strategy, which is what AutoMigrate
// (and therefore the database) sees.
func parse(t *testing.T, model any) *schema.Schema {
	t.Helper()
	s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("schema.Parse(%T) failed: %v", model, err)
	}
	return s
}

// The table names must not change: an existing installation keeps its data, so AutoMigrate has to find
// the same tables it created before. These are the names from the original SQL migrations.
func TestTableNames(t *testing.T) {
	want := map[string]string{
		"User":                "users",
		"Link":                "links",
		"ClickLog":            "click_logs",
		"SMSVerificationCode": "sms_codes",
		"LoginCaptcha":        "login_captchas",
		"Folder":              "folders",
		"Tag":                 "tags",
		"LinkTag":             "link_tags",
		"Domain":              "domains",
		"UTMTemplate":         "utm_templates",
		"APIToken":            "api_tokens",
		"Workspace":           "workspaces",
		"AIConversation":      "ai_conversations",
		"AIMessage":           "ai_messages",
	}
	for _, model := range Models() {
		s := parse(t, model)
		name := reflect.TypeOf(model).Elem().Name()
		expected, ok := want[name]
		if !ok {
			t.Errorf("model %s is not covered by this test; add its expected table name", name)
			continue
		}
		if s.Table != expected {
			t.Errorf("%s maps to table %q, want %q", name, s.Table, expected)
		}
	}
}

// Every column the services SELECT/INSERT must exist in the model, otherwise AutoMigrate would create a
// schema that the queries cannot use.
func TestColumns(t *testing.T) {
	want := map[string][]string{
		"users": {"id", "phone", "email", "wechat_openid", "wechat_unionid", "name", "avatar",
			"password_hash", "created_at", "updated_at", "last_login_at", "last_login_ip"},
		"links": {"id", "short_code", "original_url", "title", "description", "image_url", "domain",
			"password_hash", "expires_at", "is_active", "click_count", "user_id", "workspace_id",
			"folder_id", "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
			"ios_url", "android_url", "created_at", "updated_at"},
		"click_logs": {"id", "link_id", "ip", "user_agent", "platform", "kind", "action", "referer",
			"country", "province", "city", "created_at", "event_id"},
		"sms_codes":      {"id", "phone", "code_hash", "ip", "used", "attempts", "expires_at", "created_at"},
		"login_captchas": {"id", "code_hash", "ip", "used", "attempts", "expires_at", "created_at"},
		"folders":        {"id", "user_id", "name", "created_at", "updated_at"},
		"tags":           {"id", "user_id", "name", "color", "created_at"},
		"link_tags":      {"link_id", "tag_id"},
		"domains": {"id", "user_id", "name", "verified", "verified_at", "created_at",
			"updated_at"},
		"utm_templates": {"id", "user_id", "name", "utm_source", "utm_medium", "utm_campaign",
			"utm_term", "utm_content", "created_at", "updated_at"},
		"api_tokens":       {"id", "user_id", "name", "token_hash", "last_used", "created_at"},
		"workspaces":       {"id", "name", "slug", "user_id", "created_at", "updated_at"},
		"ai_conversations": {"id", "user_id", "created_at"},
		"ai_messages":      {"id", "conversation_id", "role", "content", "created_at"},
	}

	byTable := make(map[string]*schema.Schema)
	for _, model := range Models() {
		s := parse(t, model)
		byTable[s.Table] = s
	}

	for table, columns := range want {
		s, ok := byTable[table]
		if !ok {
			t.Errorf("no model maps to table %s", table)
			continue
		}
		for _, column := range columns {
			if _, ok := s.FieldsByDBName[column]; !ok {
				t.Errorf("table %s is missing column %s", table, column)
			}
		}
	}
}

// The default of a NOT NULL column is what the ALTER TABLE that adds it to an existing database carries.
// GORM writes it into the DDL verbatim ("DEFAULT " + the tag value) unless the tag parsed as a value of
// the field's type, so an unparsed `default:visit` reaches PostgreSQL as `DEFAULT visit` - an identifier,
// which it rejects as a column reference. That would fail on the production database, during a deploy, so
// the quoting is asserted here rather than discovered there.
//
// The value matters as much as the quoting: the rows recorded before this column existed are already
// counted in links.click_count and in every chart, and 'visit' is the default that leaves them alone.
func TestClickKindDefaultIsQuotedAndCountsExistingRows(t *testing.T) {
	// #nosec G101 -- a throwaway DSN that is never dialed: this only renders DDL.
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "postgres://kada:invalid@127.0.0.1:1/kada?sslmode=disable",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("failed to build a dry-run gorm.DB: %v", err)
	}

	field := parse(t, &ClickLog{}).LookUpField("kind")
	if field == nil {
		t.Fatal("click_logs has no kind column")
	}

	if ddl := db.Migrator().FullDataTypeOf(field).SQL; !strings.Contains(ddl, "DEFAULT 'visit'") {
		t.Errorf("the kind column renders as %q, want a quoted DEFAULT 'visit'", ddl)
	}
}

// The unique constraints are what actually prevent duplicate short codes, emails and token hashes.
// Losing one would silently allow duplicates, so assert on the schema rather than hoping.
func TestUniqueIndexes(t *testing.T) {
	tests := []struct {
		model  any
		column string
		reason string
	}{
		{&Link{}, "short_code", "duplicate short codes would break redirection"},
		{&User{}, "phone", "one account per phone number"},
		{&User{}, "email", "one account per email"},
		{&User{}, "wechat_openid", "one account per WeChat identity"},
		{&Workspace{}, "slug", "workspace slugs are the public identifier"},
		{&APIToken{}, "token_hash", "a token hash must identify exactly one token"},
		{&ClickLog{}, "event_id", "click event idempotency relies on it"},
	}

	for _, tt := range tests {
		s := parse(t, tt.model)
		if _, ok := s.FieldsByDBName[tt.column]; !ok {
			t.Errorf("%s.%s: field not found", s.Table, tt.column)
			continue
		}
		if !hasUniqueIndex(s, tt.column) {
			t.Errorf("%s.%s must be unique (%s)", s.Table, tt.column, tt.reason)
		}
	}
}

// hasUniqueIndex reports whether the field participates in a unique index (single-column or composite).
func hasUniqueIndex(s *schema.Schema, dbColumn string) bool {
	if field, ok := s.FieldsByDBName[dbColumn]; ok && field.UniqueIndex != "" {
		return true
	}
	return indexCovers(s, dbColumn, func(index *schema.Index) bool { return index.Class == "UNIQUE" })
}

// indexCovers reports whether some index accepted by `match` covers dbColumn.
func indexCovers(s *schema.Schema, dbColumn string, match func(*schema.Index) bool) bool {
	for _, index := range s.ParseIndexes() {
		if !match(index) {
			continue
		}
		for _, option := range index.Fields {
			if option.DBName == dbColumn {
				return true
			}
		}
	}
	return false
}

// domains are unique per (user_id, name), which is a composite index and easy to break when editing tags.
func TestDomainCompositeUniqueIndex(t *testing.T) {
	s := parse(t, &Domain{})
	if !hasUniqueIndex(s, "name") || !hasUniqueIndex(s, "user_id") {
		t.Errorf("domains (user_id, name) must be covered by a unique index, indexes: %+v", s.ParseIndexes())
	}
}

// short_code lookups and the click_logs aggregates rely on indexes; losing one silently turns the hot
// paths into sequential scans.
func TestIndexesExist(t *testing.T) {
	tests := []struct {
		model  any
		column string
	}{
		{&Link{}, "user_id"},
		{&Link{}, "created_at"},
		{&Link{}, "folder_id"},
		{&Link{}, "workspace_id"},
		{&ClickLog{}, "created_at"},
		{&ClickLog{}, "platform"},
		// Every click aggregate filters on it.
		{&ClickLog{}, "kind"},
		{&ClickLog{}, "link_id"},
		{&SMSVerificationCode{}, "phone"},
		// The per-IP send quotas count this column before every send.
		{&SMSVerificationCode{}, "ip"},
		// Expired challenges are purged by an opportunistic DELETE on every issue.
		{&LoginCaptcha{}, "expires_at"},
		{&Folder{}, "user_id"},
		{&Tag{}, "user_id"},
		{&Domain{}, "user_id"},
		{&UTMTemplate{}, "user_id"},
		{&APIToken{}, "user_id"},
		{&Workspace{}, "user_id"},
	}

	for _, tt := range tests {
		s := parse(t, tt.model)
		found := false
		for _, index := range s.ParseIndexes() {
			for _, option := range index.Fields {
				if option.DBName == tt.column {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s.%s has no index", s.Table, tt.column)
		}
	}
}

// A field that carries both a named and an unnamed index tag (`uniqueIndex;index`, or `uniqueIndex:name`
// plus `index`) is parsed as two indexes over the same column, and the driver renders the column twice:
//
//	CREATE UNIQUE INDEX "idx_users_phone" ON "users" ("phone","phone")
//
// That is invalid in spirit and was the visible symptom of a worse problem: `uniqueIndex` only creates an
// index, while the column-level `unique` flag is what the migrator compares, so the two have to be
// declared together. This asserts on the parsed schema so the mistake cannot come back without a database.
func TestNoIndexRepeatsAColumn(t *testing.T) {
	for _, model := range Models() {
		s := parse(t, model)
		for name, index := range s.ParseIndexes() {
			seen := make(map[string]bool, len(index.Fields))
			for _, field := range index.Fields {
				if seen[field.DBName] {
					t.Errorf("%s: index %v covers column %s twice; "+
						"a field must not carry both `index` and `uniqueIndex` as separate tags",
						s.Table, name, field.DBName)
				}
				seen[field.DBName] = true
			}
		}
	}
}

// Deleting a user must clean up everything that belongs to it, and deleting a folder/workspace must only
// detach links. These constraints were explicit in the SQL migrations and drive real data-safety behavior.
func TestDeleteConstraints(t *testing.T) {
	tests := []struct {
		model    any
		field    string
		onDelete string
	}{
		{&ClickLog{}, "link_id", "CASCADE"},
		{&Folder{}, "user_id", "CASCADE"},
		{&Tag{}, "user_id", "CASCADE"},
		{&Domain{}, "user_id", "CASCADE"},
		{&UTMTemplate{}, "user_id", "CASCADE"},
		{&APIToken{}, "user_id", "CASCADE"},
		{&Link{}, "user_id", "SET NULL"},
		{&Link{}, "folder_id", "SET NULL"},
		{&Link{}, "workspace_id", "SET NULL"},
	}

	for _, tt := range tests {
		s := parse(t, tt.model)
		rel := relationshipFor(t, s, tt.field)
		if rel == nil {
			continue
		}
		// GORM keeps the constraint clause on the association field (Link.User), not on the foreign key
		// field (Link.UserID); ParseConstraint reads it from there when building the DDL.
		constraint := strings.ToUpper(rel.Field.TagSettings["CONSTRAINT"])
		wantClause := "ONDELETE:" + strings.ToUpper(tt.onDelete)
		if !strings.Contains(constraint, wantClause) {
			t.Errorf("%s.%s: constraint %q does not declare ON DELETE %s",
				s.Table, tt.field, constraint, tt.onDelete)
		}
	}
}

// relationshipFor returns the relationship whose foreign key is dbColumn, or nil (after reporting a failure).
func relationshipFor(t *testing.T, s *schema.Schema, dbColumn string) *schema.Relationship {
	t.Helper()
	for _, rel := range s.Relationships.Relations {
		for _, ref := range rel.References {
			if ref.ForeignKey != nil && ref.ForeignKey.DBName == dbColumn {
				if ref.PrimaryKey == nil {
					t.Errorf("%s.%s: relationship has no referenced primary key", s.Table, dbColumn)
				}
				return rel
			}
		}
	}
	t.Errorf("%s.%s: no relationship (foreign key) declared", s.Table, dbColumn)
	return nil
}

// A model mapped by AutoMigrate must never expose the password hash through JSON.
func TestUserHidesPasswordHash(t *testing.T) {
	s := parse(t, &User{})
	field, ok := s.FieldsByDBName["password_hash"]
	if !ok {
		t.Fatal("users.password_hash not found")
	}
	if field.Tag.Get("json") != "-" {
		t.Errorf("users.password_hash must be json:\"-\", got %q", field.Tag.Get("json"))
	}
}
