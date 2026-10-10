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

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/metrics"
	"github.com/chun/kada-backend/internal/mq"
)

type fakePublisher struct {
	calls  int
	events []mq.ClickEvent
	err    error
}

func (f *fakePublisher) PublishClick(_ context.Context, e mq.ClickEvent) error {
	f.calls++
	f.events = append(f.events, e)
	return f.err
}

type fakeWriter struct {
	calls  int
	events []mq.ClickEvent
}

func (f *fakeWriter) WriteClick(_ context.Context, e mq.ClickEvent) error {
	f.calls++
	f.events = append(f.events, e)
	return nil
}

// panickingPublisher is a publisher that crashes instead of returning an error. It stands for every way that
// can happen - a driver bug, a nil receiver, a library that panics on unexpected input - because the property
// being tested belongs to the caller: whatever the publisher does, recording a click must not be able to end
// the process.
type panickingPublisher struct{}

func (panickingPublisher) PublishClick(context.Context, mq.ClickEvent) error {
	panic("the publisher crashed")
}

// The production failure, in one line: NewKafkaClickPublisher returns a nil *KafkaClickPublisher when no broker
// is configured, and storing that in the mq.ClickPublisher interface makes the interface non-nil - so the
// service's `kafka != nil` guard passed and every click called a method on a nil receiver. The panic happened
// on the goroutine the redirect handler spawns, which Gin's recovery does not cover, so the API died on the
// first click it served: systemd restarted it and nginx answered 502 in between. The click was lost as well,
// because the direct-write fallback sits *after* the publish call.
func TestLogClickSurvivesANilPublisher(t *testing.T) {
	w := &fakeWriter{}
	svc := &LinkService{kafka: (*mq.KafkaClickPublisher)(nil), clickWriter: w}

	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 3, Kind: domain.ClickVisit})

	if w.calls != 1 {
		t.Fatalf("the click was not written directly after the publisher failed (writes=%d)", w.calls)
	}
}

// The same property, one level more general: a publisher that panics must leave the click written and the
// process alive, not only a nil one.
func TestLogClickSurvivesAPanickingPublisher(t *testing.T) {
	before := testutil.ToFloat64(metrics.ClickEvents.WithLabelValues(metrics.ClickPanicked))

	w := &fakeWriter{}
	svc := &LinkService{kafka: panickingPublisher{}, clickWriter: w}

	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 3, Kind: domain.ClickVisit})

	if w.calls != 1 {
		t.Fatalf("a panic lost the click instead of falling back (writes=%d)", w.calls)
	}
	if got := testutil.ToFloat64(metrics.ClickEvents.WithLabelValues(metrics.ClickPanicked)) - before; got != 1 {
		t.Errorf("the panic was not counted (moved by %v)", got)
	}
}

// panickingWriter crashes in the store instead. There is no fallback left at that point - the row is the last
// step - so all that is left to guarantee is that the process survives it: this test fails by dying if the
// panic escapes, which is exactly what used to happen when a nil publisher escaped instead.
type panickingWriter struct{}

func (panickingWriter) WriteClick(context.Context, mq.ClickEvent) error {
	panic("the store crashed")
}

func TestLogClickSurvivesAPanickingStore(t *testing.T) {
	before := testutil.ToFloat64(metrics.ClickEvents.WithLabelValues(metrics.ClickPanicked))

	svc := &LinkService{clickWriter: panickingWriter{}}
	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 3, Kind: domain.ClickVisit})

	if got := testutil.ToFloat64(metrics.ClickEvents.WithLabelValues(metrics.ClickPanicked)) - before; got != 1 {
		t.Errorf("the panic was not counted (moved by %v)", got)
	}
}

// ========== LogClick: queued, or written directly when the queue is down ==========

func TestLogClick_PublishSuccess(t *testing.T) {
	pub := &fakePublisher{}
	w := &fakeWriter{}
	svc := &LinkService{kafka: pub, clickWriter: w}
	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 1, Platform: domain.PlatformBrowser, Kind: domain.ClickVisit})
	if pub.calls != 1 {
		t.Fatalf("expected publisher called once, got %d", pub.calls)
	}
	if w.calls != 0 {
		t.Fatalf("expected no direct write when publish succeeds, got %d", w.calls)
	}
}

func TestLogClick_PublishErrorFallsBack(t *testing.T) {
	pub := &fakePublisher{err: assertErr("kafka down")}
	w := &fakeWriter{}
	svc := &LinkService{kafka: pub, clickWriter: w}
	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 1, Platform: domain.PlatformBrowser, Kind: domain.ClickVisit})
	if pub.calls != 1 || w.calls != 1 {
		t.Fatalf("expected publish 1 + fallback 1, got publish=%d write=%d", pub.calls, w.calls)
	}
}

func TestLogClick_NoKafkaDirectWrite(t *testing.T) {
	w := &fakeWriter{}
	svc := &LinkService{kafka: nil, clickWriter: w}
	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 1, Platform: domain.PlatformBrowser, Kind: domain.ClickVisit})
	if w.calls != 1 {
		t.Fatalf("expected direct write, got %d", w.calls)
	}
}

// The fallback is the same event, not a second one. The worker deduplicates on event_id, so an event that
// both reached Kafka and was written directly - which is what a publish that times out looks like from
// here - is counted once rather than twice.
func TestLogClick_FallbackKeepsTheEventID(t *testing.T) {
	pub := &fakePublisher{err: assertErr("kafka down")}
	w := &fakeWriter{}
	svc := &LinkService{kafka: pub, clickWriter: w}
	svc.LogClick(context.Background(), domain.ClickEvent{LinkID: 7, Platform: domain.PlatformBrowser, Kind: domain.ClickVisit})

	if len(pub.events) != 1 || len(w.events) != 1 {
		t.Fatalf("expected one publish and one write, got %d and %d", len(pub.events), len(w.events))
	}
	if pub.events[0].EventID == "" || pub.events[0].EventID != w.events[0].EventID {
		t.Errorf("the fallback wrote a different event: published %q, wrote %q",
			pub.events[0].EventID, w.events[0].EventID)
	}
}

// The kind is decided in the redirect handler - the only place that knows whether the client was sent to
// the target or served the guide page - so it has to arrive at the store unchanged, by both routes.
func TestLogClick_CarriesTheKind(t *testing.T) {
	pub := &fakePublisher{}
	w := &fakeWriter{}
	event := domain.ClickEvent{LinkID: 1, Platform: domain.PlatformWechat, Kind: domain.ClickRequest}

	svc := &LinkService{kafka: pub}
	svc.LogClick(context.Background(), event)

	svc = &LinkService{kafka: nil, clickWriter: w}
	svc.LogClick(context.Background(), event)

	if len(pub.events) != 1 || pub.events[0].Kind != string(domain.ClickRequest) {
		t.Errorf("the publisher received %+v, want one request-kind event", pub.events)
	}
	if len(w.events) != 1 || w.events[0].Kind != string(domain.ClickRequest) {
		t.Errorf("the direct write received %+v, want one request-kind event", w.events)
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

			err := store.WriteClick(context.Background(), mq.ClickEvent{
				EventID: "e1", LinkID: 3, Kind: tt.kind, CreatedAt: time.Now(),
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

	err := store.WriteClick(context.Background(), mq.ClickEvent{
		EventID: "e1", LinkID: 3, IP: "1.2.3.4", Platform: string(domain.PlatformWechat),
		Kind: string(domain.ClickRequest), CreatedAt: time.Now(),
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

	err := store.WriteClick(context.Background(), mq.ClickEvent{
		EventID: "e1", LinkID: 3, Kind: string(domain.ClickVisit), CreatedAt: time.Now(),
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

	err := store.WriteClick(context.Background(), mq.ClickEvent{EventID: "e1", LinkID: 3, CreatedAt: time.Now()})
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

	err := store.WriteClick(context.Background(), mq.ClickEvent{
		EventID: "e1", LinkID: 3, Platform: string(domain.PlatformWechat),
		Kind: string(domain.ClickAction), Action: string(domain.ActionCopyLink), CreatedAt: time.Now(),
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
	err := store.WriteClick(context.Background(), mq.ClickEvent{
		EventID: "e1", LinkID: 3, IP: "1.2.3.4", UserAgent: "ua", Platform: string(domain.PlatformWechat),
		Kind: string(domain.ClickVisit), Referer: "https://example.com/from", CreatedAt: time.Now(),
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
