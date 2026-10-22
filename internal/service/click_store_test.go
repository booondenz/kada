package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/metrics"
)

type fakeWriter struct {
	calls  int
	events []domain.ClickEvent
}

func (f *fakeWriter) WriteClick(_ context.Context, e domain.ClickEvent) error {
	f.calls++
	f.events = append(f.events, e)
	return nil
}

// panickingWriter crashes in the store. There is no second route left to fall back to, so all that is left
// to guarantee is that the process survives it: this test fails by dying if the panic escapes.
type panickingWriter struct{}

func (panickingWriter) WriteClick(context.Context, domain.ClickEvent) error {
	panic("the store crashed")
}

// failingWriter is a store that says no, which is the ordinary failure - a deadlock, a dropped connection.
type failingWriter struct{}

func (failingWriter) WriteClick(context.Context, domain.ClickEvent) error {
	return errors.New("the store is down")
}

func TestLogClickSurvivesAPanickingStore(t *testing.T) {
	before := testutil.ToFloat64(metrics.ClickEvents.WithLabelValues(metrics.ClickPanicked))

	svc := &LinkService{clickWriter: panickingWriter{}}
	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 3, Kind: domain.ClickVisit})

	if got := testutil.ToFloat64(metrics.ClickEvents.WithLabelValues(metrics.ClickPanicked)) - before; got != 1 {
		t.Errorf("the panic was not counted (moved by %v)", got)
	}
}

// One write, with the key the store deduplicates on and the kind the handler decided. The kind cannot be
// decided here: only the caller knows whether the client was sent to the target or served the guide page.
func TestLogClickWritesOnceWithAKeyAndTheHandlersKind(t *testing.T) {
	w := &fakeWriter{}
	svc := &LinkService{clickWriter: w}

	targetID := int64(9)
	svc.LogClick(context.Background(), domain.ClickEvent{
		LinkID:   3,
		Platform: domain.PlatformWechat,
		Kind:     domain.ClickRequest,
		TargetID: &targetID,
	})

	if w.calls != 1 {
		t.Fatalf("writes = %d, want exactly one", w.calls)
	}

	written := w.events[0]
	if written.EventID == "" {
		t.Error("the click was written without an idempotency key")
	}
	if written.CreatedAt.IsZero() {
		t.Error("the click was written without a timestamp")
	}
	if written.Kind != domain.ClickRequest || written.Platform != domain.PlatformWechat {
		t.Errorf("written kind/platform = %v/%v, want the handler's", written.Kind, written.Platform)
	}
	if written.TargetID == nil || *written.TargetID != targetID {
		t.Errorf("written target = %v, want %d", written.TargetID, targetID)
	}
}

// A service without a store has nowhere to put the click. Saying so must not be a nil dereference on the
// goroutine the redirect spawns, where a panic would end the process.
func TestLogClickWithoutAStoreDoesNothing(t *testing.T) {
	svc := &LinkService{}

	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 3, Kind: domain.ClickVisit})
}

// A refused write is counted: a store that is failing has to be visible, or it looks like a quiet afternoon.
func TestLogClickCountsARefusedWrite(t *testing.T) {
	before := testutil.ToFloat64(metrics.ClickEvents.WithLabelValues(metrics.ClickWriteFailed))

	svc := &LinkService{clickWriter: failingWriter{}}
	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 3, Kind: domain.ClickVisit})

	if got := testutil.ToFloat64(metrics.ClickEvents.WithLabelValues(metrics.ClickWriteFailed)) - before; got != 1 {
		t.Errorf("the refused write was not counted (moved by %v)", got)
	}
}

// ========== WriteClick: the one place a click becomes a number ==========

// recordingDB stands in for the database: it remembers every statement and answers with a fixed number of
// affected rows.
//
// ClickStore cannot be exercised through a dry-run session. The work happens inside Transaction(), which
// starts by asking the connection for a transaction, and a dry-run handle has a real *sql.DB behind it
// that is never dialed - Begin fails before a single statement is built. Replacing the connection rather
// than the query keeps the code under test exactly as it runs in production: the transaction, the
// idempotency check and the counter, with nothing sent anywhere.
type recordingDB struct {
	statements   []string
	args         [][]any
	rowsAffected int64
}

func (d *recordingDB) exec(query string, args ...any) (sql.Result, error) {
	d.statements = append(d.statements, query)
	d.args = append(d.args, args)
	return affectedRows(d.rowsAffected), nil
}

// counted reports whether the counter was incremented, which is the only thing the dashboard reads.
func (d *recordingDB) counted() bool {
	return slices.ContainsFunc(d.statements, func(s string) bool {
		return strings.Contains(s, "UPDATE links SET click_count")
	})
}

// recordedKind returns the kind the INSERT was given, or "" if no INSERT ran.
func (d *recordingDB) recordedKind() string {
	for i, s := range d.statements {
		if !strings.Contains(s, "INSERT INTO click_logs") {
			continue
		}
		for _, arg := range d.args[i] {
			if kind, ok := arg.(string); ok {
				switch domain.ClickKind(kind) {
				case domain.ClickVisit, domain.ClickRequest, domain.ClickAction:
					return kind
				}
			}
		}
	}
	return ""
}

// insertedArgs returns the values bound to the click_logs INSERT, or nil if none ran.
func (d *recordingDB) insertedArgs() []any {
	for i, s := range d.statements {
		if strings.Contains(s, "INSERT INTO click_logs") {
			return d.args[i]
		}
	}
	return nil
}

type affectedRows int64

func (r affectedRows) LastInsertId() (int64, error) { return 0, nil }
func (r affectedRows) RowsAffected() (int64, error) { return int64(r), nil }

// recordingConn implements the four methods of gorm.ConnPool that database/sql offers. Only ExecContext is
// reached: everything ClickStore issues is a statement, never a query.
type recordingConn struct{ db *recordingDB }

func (c recordingConn) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("the recording connection does not prepare statements")
}

func (c recordingConn) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	return c.db.exec(query, args...)
}

func (c recordingConn) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("the recording connection does not run queries")
}

func (c recordingConn) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return &sql.Row{}
}

// recordingPool is the handle gorm.Open leaves on the statement. It is deliberately not a TxCommitter:
// that is what sends Transaction() down the ordinary path, through BeginTx, instead of the savepoint path
// it takes for a nested transaction.
type recordingPool struct{ recordingConn }

func (p *recordingPool) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	// A pointer, not a value: gorm's Commit calls reflect.ValueOf(committer).IsNil(), which panics on a
	// struct held in an interface.
	return &recordingTx{p.recordingConn}, nil
}

// recordingTx is the handle inside the transaction, and it is a TxCommitter so Commit succeeds without a
// server to commit to. It has no BeginTx of its own, so a nested Transaction would not silently reuse it.
type recordingTx struct{ recordingConn }

func (t *recordingTx) Commit() error   { return nil }
func (t *recordingTx) Rollback() error { return nil }

func newRecordingStore(t *testing.T, rowsAffected int64) (*ClickStore, *recordingDB) {
	t.Helper()

	// DisableAutomaticPing keeps gorm.Open from dialing the DSN, and the connection is replaced before any
	// statement is issued, so this URL is never contacted.
	db, err := gorm.Open(postgres.New(postgres.Config{
		// #nosec G101 -- a throwaway DSN that is never dialed.
		DSN: "postgres://kada:invalid@127.0.0.1:1/kada?sslmode=disable",
	}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("failed to build the gorm handle: %v", err)
	}

	recorder := &recordingDB{rowsAffected: rowsAffected}
	pool := &recordingPool{recordingConn{recorder}}
	db.ConnPool = pool
	db.Statement.ConnPool = pool

	return NewClickStore(db), recorder
}

// A visit moves the counter; nothing else does. This is the number the dashboard totals and the link list
// sorts by, and a prefetch that moved it is the whole complaint: the row is worth keeping, the person was
// never there.
func TestWriteClickCountsOnlyAVisit(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{string(domain.ClickVisit), true},
		{string(domain.ClickRequest), false},
		{string(domain.ClickAction), false},
		{string(domain.PlatformWechat), false}, // any other value, including a kind that does not exist
		{"", false},
	}

	for _, tt := range tests {
		t.Run("kind="+tt.kind, func(t *testing.T) {
			store, recorder := newRecordingStore(t, 1)

			err := store.WriteClick(context.Background(), domain.ClickEvent{
				EventID: "e1", LinkID: 3, Kind: domain.ClickKind(tt.kind), CreatedAt: time.Now(),
			})
			if err != nil {
				t.Fatalf("WriteClick returned %v", err)
			}

			if got := recorder.counted(); got != tt.want {
				t.Errorf("click_count incremented = %v, want %v (statements: %v)", got, tt.want, recorder.statements)
			}
		})
	}
}

// A request that will not be counted is still written. That is the difference between ignoring prefetches
// and recording them while refusing to count them: the second one leaves the amount of prefetch knowable.
func TestWriteClickRecordsTheRowItDoesNotCount(t *testing.T) {
	store, recorder := newRecordingStore(t, 1)

	err := store.WriteClick(context.Background(), domain.ClickEvent{
		EventID: "e1", LinkID: 3, IP: "1.2.3.4", Platform: domain.PlatformWechat,
		Kind: domain.ClickRequest, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("WriteClick returned %v", err)
	}

	if len(recorder.statements) != 1 || !strings.Contains(recorder.statements[0], "INSERT INTO click_logs") {
		t.Fatalf("expected the insert and nothing else, got %v", recorder.statements)
	}
	if got := recorder.recordedKind(); got != string(domain.ClickRequest) {
		t.Errorf("the row was written with kind %q, want %q", got, domain.ClickRequest)
	}
}

// Kafka delivers at least once and the degraded direct write can race the worker, so the same event
// reaches the store twice. The second insert hits the unique index and reports no affected row.
func TestWriteClickDoesNotCountADuplicateEvent(t *testing.T) {
	store, recorder := newRecordingStore(t, 0)

	err := store.WriteClick(context.Background(), domain.ClickEvent{
		EventID: "e1", LinkID: 3, Kind: domain.ClickVisit, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("WriteClick returned %v", err)
	}

	if recorder.counted() {
		t.Errorf("a duplicate event moved click_count: %v", recorder.statements)
	}
}

// An event from a producer older than the kind column carries none. "Nobody can say this was a person" is
// the one answer that must not count, and it should not leave an unlabeled row behind either.
func TestWriteClickLabelsAnUnlabeledEventAsARequest(t *testing.T) {
	store, recorder := newRecordingStore(t, 1)

	err := store.WriteClick(context.Background(), domain.ClickEvent{EventID: "e1", LinkID: 3, CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("WriteClick returned %v", err)
	}

	if recorder.counted() {
		t.Errorf("an event without a kind moved click_count: %v", recorder.statements)
	}
	if got := recorder.recordedKind(); got != string(domain.ClickRequest) {
		t.Errorf("the row was written with kind %q, want %q", got, domain.ClickRequest)
	}
}

// The action is its own column, so a report that reads referers no longer has to know that some of its rows
// are not referers.
func TestWriteClickRecordsTheActionInItsOwnColumn(t *testing.T) {
	store, recorder := newRecordingStore(t, 1)

	err := store.WriteClick(context.Background(), domain.ClickEvent{
		EventID: "e1", LinkID: 3, Platform: domain.PlatformWechat,
		Kind: domain.ClickAction, Action: domain.ActionCopyLink, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("WriteClick returned %v", err)
	}

	if !strings.Contains(recorder.statements[0], "action") {
		t.Fatalf("the insert does not mention the action column: %s", recorder.statements[0])
	}
	if !slices.Contains(recorder.insertedArgs(), any(string(domain.ActionCopyLink))) {
		t.Errorf("the action did not reach the insert: %v", recorder.insertedArgs())
	}
}

// An action that is not an action has to be absent rather than empty: the question "did this row record an
// interaction" is asked as `action IS NOT NULL`, and an empty string would answer yes for every redirect.
func TestWriteClickStoresNoActionAsNull(t *testing.T) {
	store, recorder := newRecordingStore(t, 1)

	// Every other field is set, so the action is the only value in this insert that could be an empty
	// string, and the assertion does not have to know which position it occupies.
	err := store.WriteClick(context.Background(), domain.ClickEvent{
		EventID: "e1", LinkID: 3, IP: "1.2.3.4", UserAgent: "ua", Platform: domain.PlatformWechat,
		Kind: domain.ClickVisit, Referer: "https://example.com/from", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("WriteClick returned %v", err)
	}

	if slices.Contains(recorder.insertedArgs(), any("")) {
		t.Errorf("a redirect bound an empty action instead of NULL: %v", recorder.insertedArgs())
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
