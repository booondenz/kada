package analytics

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/chun/kada-backend/internal/domain"
)

// statement is one SQL statement a handler rendered, with the values it was going to bind.
type statement struct {
	sql  string
	vars []any
}

// newRecordingDB returns a dry-run handle and every statement issued through it.
//
// There is no database here, and a dry-run handle refuses the row scan these handlers finish with, so the
// responses are 500s and their status is deliberately not asserted. What the tests read instead is the SQL
// the handlers actually built - not a copy of their queries rebuilt in the test, which could drift from
// them. A query that lost its filter would keep working and keep answering with a plausible number, so the
// mistake has no symptom to catch; the rendered statement is where it is visible.
func newRecordingDB(t *testing.T) (*gorm.DB, *[]statement) {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		// #nosec G101 -- a throwaway DSN that is never dialed: DryRun only renders the SQL.
		DSN: "postgres://kada:invalid@127.0.0.1:1/kada?sslmode=disable",
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		// The refusal above is expected, and a test that prints it on every run trains the reader to skip
		// the output entirely.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to build a dry-run gorm.DB: %v", err)
	}

	recorded := &[]statement{}
	capture := func(tx *gorm.DB) {
		*recorded = append(*recorded, statement{sql: tx.Statement.SQL.String(), vars: tx.Statement.Vars})
	}

	// Both processors, because the handlers use both finishers: Count() goes through the query callbacks,
	// while Scan() reaches for Rows() and is rendered by the row callbacks instead.
	if err := db.Callback().Query().After("gorm:query").Register("test:capture_query", capture); err != nil {
		t.Fatalf("failed to register the query capture: %v", err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:capture_row", capture); err != nil {
		t.Fatalf("failed to register the row capture: %v", err)
	}

	silenceLogs(t)
	return db, recorded
}

// silenceLogs drops what the handlers log while a test runs.
//
// They report the dry-run refusal as a failed query, which is true and is not what these tests are about:
// the statement is rendered and captured before that, and the assertions have already read it. Leaving the
// lines in would put "query failed" next to a passing test.
func silenceLogs(t *testing.T) {
	t.Helper()

	previous := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(previous) })
}

// call serves one request against the real routes, with the auth middleware reduced to the one thing these
// handlers take from it: the user id on the context.
func call(t *testing.T, db *gorm.DB, target string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(db, nil).RegisterRoutes(r.Group("/api"), func(c *gin.Context) {
		c.Set("user_id", int64(7))
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
	return w
}

// kindFilters reports whether a statement carried a kind predicate, and every value bound to one. The
// bound values are what the assertions read: a filter that is present but bound to something else silences
// the counters just as quietly as a missing one.
func kindFilters(recorded []statement) (found bool, bound []any) {
	for _, s := range recorded {
		if !strings.Contains(s.sql, "cl.kind =") {
			continue
		}
		found = true
		bound = append(bound, s.vars...)
	}
	return found, bound
}

// Every counter the dashboard shows has to filter on the kind, and none of them may quietly stop doing it.
func TestCountersOnlyCountVisits(t *testing.T) {
	for _, endpoint := range []string{"platforms", "daily", "customers"} {
		t.Run(endpoint, func(t *testing.T) {
			db, recorded := newRecordingDB(t)
			call(t, db, "/api/analytics/"+endpoint)

			found, bound := kindFilters(*recorded)
			if !found {
				t.Fatalf("the %s query does not filter on the kind: %+v", endpoint, *recorded)
			}
			if !slices.Contains(bound, any(string(domain.ClickVisit))) {
				t.Errorf("the %s query filters on something other than a visit: %v", endpoint, bound)
			}
		})
	}
}

// The event list is where the rows the counters leave out stay reachable. The default is unchanged - it is
// a log of what happened to the link - and the kind filter is what makes the amount of prefetch knowable
// through the API and not only in the table.
func TestEventsFiltersByKind(t *testing.T) {
	t.Run("without a filter every row is listed and labeled", func(t *testing.T) {
		db, recorded := newRecordingDB(t)
		call(t, db, "/api/analytics/events")

		var listed bool
		for _, s := range *recorded {
			if strings.Contains(s.sql, "cl.kind") && strings.Contains(s.sql, "cl.action") && strings.Contains(s.sql, "cl.referer") {
				listed = true
			}
			if strings.Contains(s.sql, "cl.kind =") {
				t.Errorf("the default listing filters by kind: %s", s.sql)
			}
		}
		if !listed {
			t.Errorf("the event rows do not carry their kind: %+v", *recorded)
		}
	})

	t.Run("a kind narrows it", func(t *testing.T) {
		db, recorded := newRecordingDB(t)
		call(t, db, "/api/analytics/events?kind=request")

		found, bound := kindFilters(*recorded)
		if !found {
			t.Fatalf("?kind=request did not reach the query: %+v", *recorded)
		}
		if !slices.Contains(bound, any(string(domain.ClickRequest))) {
			t.Errorf("?kind=request bound %v, want the request kind", bound)
		}
	})

	// An empty list is also what a working filter looks like, so a typo must not be answered with one. The
	// status is the only place this shows, and it is decided before any query is built.
	t.Run("an unknown kind is refused", func(t *testing.T) {
		db, _ := newRecordingDB(t)

		if w := call(t, db, "/api/analytics/events?kind=visits"); w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for an unknown kind, got %d", w.Code)
		}
	})
}
