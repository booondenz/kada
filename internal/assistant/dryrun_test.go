package assistant

import (
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newDryRunDB builds a GORM handle that generates SQL without ever contacting the server.
//
// The conversation store is queried through the builder, and the failure mode that matters here is not a
// typo but a clause that renders something the database rejects - or, worse, something subtly different
// that matches another account's rows. DryRun renders the SQL that would be sent, which is enough to assert
// on the ownership condition with no PostgreSQL available.
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
