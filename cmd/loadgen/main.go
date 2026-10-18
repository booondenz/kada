// Command loadgen measures the redirect path of a running Kada instance: throughput, tail latency, and
// the share of lookups the cache answered.
//
// It seeds its own links - directly in PostgreSQL, because creating one through the API needs a signed-in
// account and an SMS code - and drives the two scenarios issue #9 asked for:
//
//	working set  codes that exist, so after the first pass the cache answers almost everything
//	absent codes  distinct codes that do not exist, the stream a scanner sends
//
// The cache numbers are read back from /metrics rather than inferred: a hit is invisible to the client (a
// 302 looks the same whether PostgreSQL was reached or not), and the server's own counter is the only
// witness. That is also why this is not `hey`: hey loads a single URL and cannot read /metrics.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/joho/godotenv"
)

// The codes the two scenarios ask for, and where the seeded ones point. The prefixes are what keeps a
// working-set code from ever being a real link: a load test that redirected to a live target would measure
// somebody else's server.
const (
	workingPrefix = "load-"
	absentPrefix  = "absent-"
	targetBase    = "https://example.com/load/"

	// A current desktop browser's user agent, so the answer is the 302 a visitor gets rather than the guide
	// page served to WeChat and QQ. A crawler's would also be a redirect, but naming one would mean the
	// click is recorded as a request rather than a visit and the count in /metrics would differ for a
	// reason that has nothing to do with the load.
	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// config is what the flags say.
type config struct {
	base        string
	databaseURL string
	codes       int
	absent      int
	requests    int
	warmup      int
	clients     int
	concurrency int
	timeout     time.Duration
	seed        bool
	markdown    bool
}

func main() {
	// The same .env the server and the migration read, and loaded first so that -database's default
	// ($DATABASE_URL) is the database the API is actually using.
	_ = godotenv.Load()

	cfg := config{}
	flag.StringVar(&cfg.base, "base", "http://127.0.0.1:8080", "the running API to load, without a trailing slash")
	flag.StringVar(&cfg.databaseURL, "database", os.Getenv("DATABASE_URL"),
		"PostgreSQL DSN used to seed the working set (default $DATABASE_URL)")
	flag.IntVar(&cfg.codes, "codes", 200, "how many existing short codes the working set holds")
	flag.IntVar(&cfg.absent, "absent", 2000, "how many absent short codes the second scenario asks for")
	flag.IntVar(&cfg.requests, "requests", 20000, "measured requests per scenario")
	flag.IntVar(&cfg.warmup, "warmup", 2000, "requests sent before the working set is measured, to fill the cache")
	flag.IntVar(&cfg.clients, "clients", 4000, "how many synthetic client addresses the requests are spread over")
	flag.IntVar(&cfg.concurrency, "concurrency", 50, "concurrent requests")
	flag.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per-request timeout")
	flag.BoolVar(&cfg.seed, "seed", true, "insert the working set into the database first")
	flag.BoolVar(&cfg.markdown, "markdown", false, "print the scenarios as a markdown table instead of a report")
	flag.Parse()

	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "loadgen:", err)
		os.Exit(1)
	}
}

// run measures both scenarios and reports them, then returns the failures it saw. Reporting comes first:
// a run that hit the rate limiter still produced numbers worth reading, and the error is what says they
// are not the numbers that were asked for.
func run(cfg config) error {
	ctx := context.Background()
	base := strings.TrimRight(cfg.base, "/")

	client := newClient(cfg)
	if err := waitForAPI(ctx, client, base); err != nil {
		return err
	}

	if cfg.seed {
		if cfg.databaseURL == "" {
			return errors.New("nothing to seed with: pass -database or set DATABASE_URL, " +
				"or run with -seed=false once the codes exist")
		}
		if err := seed(ctx, cfg.databaseURL, cfg.codes); err != nil {
			return err
		}
	}

	driver := &driver{client: client, base: base, ips: syntheticIPs(cfg.clients)}

	// The working set first, and every code asked for more than once: the second run of a code is the one
	// the cache is supposed to answer, so a scenario that asked for each code once could only measure the
	// database. An existing code is answered with a redirect.
	working, err := measure(ctx, driver, &tally{
		label:       "working set",
		detail:      fmt.Sprintf("%d codes that exist", cfg.codes),
		concurrency: cfg.concurrency,
		expected:    http.StatusFound,
	}, codesWithPrefix(workingPrefix, cfg.codes), cfg.requests, cfg.warmup)
	if err != nil {
		return err
	}

	// Then the stream a scanner sends. Its first pass is the database seeing every code once; everything
	// after that is the negative cache, which is the thing being measured - so the codes repeat here too.
	// An absent code is answered with a 404, and that is the answer this scenario expects.
	absent, err := measure(ctx, driver, &tally{
		label:       "absent codes",
		detail:      fmt.Sprintf("%d codes that do not exist", cfg.absent),
		concurrency: cfg.concurrency,
		expected:    http.StatusNotFound,
	}, codesWithPrefix(absentPrefix, cfg.absent), cfg.requests, 0)
	if err != nil {
		return err
	}

	tallies := []*tally{working, absent}
	output := markdownTable(tallies)
	if !cfg.markdown {
		reports := make([]string, 0, len(tallies))
		for _, t := range tallies {
			reports = append(reports, report(t))
		}
		output = strings.Join(reports, "")
	}
	if _, err := io.WriteString(os.Stdout, output); err != nil {
		return err
	}

	var problems []error
	for _, t := range tallies {
		if n := t.notAnsweredWith(t.expected); n != 0 {
			problems = append(problems, fmt.Errorf("%s: %d of %d requests were not answered with %d "+
				"(raise -clients: one address is allowed 120 requests a minute)",
				t.label, n, t.requests, t.expected))
		}
		for _, failure := range t.failures {
			problems = append(problems, fmt.Errorf("%s: %s", t.label, failure))
		}
	}
	return errors.Join(problems...)
}

// newClient builds the HTTP client the scenarios share.
//
// CheckRedirect is the line that makes the numbers mean anything: the answer being measured is the 302
// itself, and a client that follows it would spend the rest of the request talking to example.com.
func newClient(cfg config) *http.Client {
	return &http.Client{
		Timeout: cfg.timeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.concurrency * 2,
			MaxIdleConnsPerHost: cfg.concurrency * 2,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// waitForAPI fails early and with the fix in the message, instead of turning a missing server into twenty
// thousand connection errors.
func waitForAPI(ctx context.Context, client *http.Client, base string) error {
	var last error
	for attempt := 0; attempt < 30; attempt++ {
		last = probe(ctx, client, base)
		if last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("no API answered at %s (%v): start it with `go run ./cmd/server`", base, last)
}

// probe asks the health endpoint once, so the retry loop above does not have to defer a close per attempt.
func probe(ctx context.Context, client *http.Client, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/health", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", base+"/api/health", resp.Status)
	}
	return nil
}

// seed inserts the working set. It writes to the links table directly because the alternative is to sign
// in, create links one at a time over HTTP and need an SMS code to do it - and because a baseline should
// not depend on the account it was created with. The rows are the same rows the API would have made.
func seed(ctx context.Context, dsn string, codes int) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}
	defer conn.Close(ctx)

	// ON CONFLICT keeps a second run from failing on the first run's rows: the target is the same, so an
	// existing row already is the row this would have inserted.
	_, err = conn.Exec(ctx, `
		INSERT INTO links (short_code, original_url, domain, is_active, click_count, created_at, updated_at)
		SELECT $1 || lpad(n::text, 4, '0'), $2 || n::text, 'kada.link', TRUE, 0, now(), now()
		FROM generate_series(1, $3) AS n
		ON CONFLICT (short_code) DO NOTHING`,
		workingPrefix, targetBase, codes)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // undefined_table
			return fmt.Errorf("the links table does not exist yet - run `go run ./cmd/migrate` first: %w", err)
		}
		return fmt.Errorf("seed the working set: %w", err)
	}
	return nil
}

// codesWithPrefix builds a scenario's code list with the same names seed inserts, so the two scenarios
// differ in nothing but their prefix.
func codesWithPrefix(prefix string, n int) []string {
	codes := make([]string, n)
	for i := range codes {
		codes[i] = fmt.Sprintf("%s%04d", prefix, i+1)
	}
	return codes
}

// syntheticIPs builds the pool of client addresses the requests are attributed to.
//
// Without it every request would carry the loopback address and be answered with 429 after 120 of them:
// the limiter keys on the address, and one machine is not one visitor of a public short-link service. The
// addresses sit in 10/8 so a run's rows in the click table can never be mistaken for a real visitor.
func syntheticIPs(n int) []string {
	if n < 1 {
		n = 1
	}
	ips := make([]string, n)
	for i := range ips {
		ips[i] = fmt.Sprintf("10.%d.%d.%d", 100+i/(250*250), (i/250)%250, i%250+1)
	}
	return ips
}

// driver sends the requests of one scenario.
type driver struct {
	client *http.Client
	base   string
	ips    []string
}

// request sends one redirect and returns the status it was answered with.
func (d *driver) request(ctx context.Context, code, ip string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/r/"+code, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", browserUA)
	// X-Real-IP is the header the API trusts for the client address (see middleware.RealIP), which is what
	// the rate limiter keys on. Sending it is the difference between measuring the redirect path and
	// measuring the limiter.
	req.Header.Set("X-Real-IP", ip)

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Drained rather than just closed: a body left unread returns the connection to the pool unusable, and
	// the next request would pay for a new TCP connection - which is latency this path does not have.
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

// drive sends requests for codes in order, spread over workers and the address pool, and reports what the
// client saw.
func (d *driver) drive(ctx context.Context, codes []string, requests, workers int) (*tally, error) {
	if len(codes) == 0 {
		return nil, errors.New("no codes to ask for")
	}
	if requests < 1 || workers < 1 {
		return nil, fmt.Errorf("nothing to do: requests=%d workers=%d", requests, workers)
	}
	if len(d.ips) == 0 {
		return nil, errors.New("no client addresses: the rate limiter keys on them")
	}

	var (
		next     atomic.Int64
		mu       sync.Mutex
		status   = make(map[int]int, 4)
		failures = make(map[string]int)
		latency  = make([]time.Duration, requests)
		wg       sync.WaitGroup
	)

	start := time.Now()
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				// The counter rather than a per-worker slice: the codes and the addresses are then
				// distributed the same way however many workers there are, so two runs of one scenario are
				// comparable.
				i := int(next.Add(1)) - 1
				if i >= requests || ctx.Err() != nil {
					return
				}
				begin := time.Now()
				code, err := d.request(ctx, codes[i%len(codes)], d.ips[i%len(d.ips)])
				latency[i] = time.Since(begin) // a distinct index per goroutine: no lock needed

				mu.Lock()
				if err != nil {
					failures[failureKind(err)]++
				} else {
					status[code]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	slices.Sort(latency)
	return &tally{
		requests:    requests,
		concurrency: workers,
		wall:        elapsed,
		latencies:   latency,
		status:      status,
		failures:    topFailures(failures, 3),
	}, nil
}

// measure runs one scenario: an optional warm-up pass that is not reported, then the measured pass with a
// /metrics snapshot on either side of it.
//
// The warm-up is not a courtesy. The first request for a code is the one that reaches PostgreSQL and
// writes the cache entry, so counting it would only measure the cold path again; the point of the working
// set is the pass after it.
func measure(ctx context.Context, d *driver, t *tally, codes []string, requests, warmup int) (*tally, error) {
	if warmup > 0 {
		if _, err := d.drive(ctx, codes, warmup, t.concurrency); err != nil {
			return nil, err
		}
	}

	before, err := snapshot(ctx, d.client, d.base)
	if err != nil {
		return nil, err
	}
	measured, err := d.drive(ctx, codes, requests, t.concurrency)
	if err != nil {
		return nil, err
	}
	after, err := snapshot(ctx, d.client, d.base)
	if err != nil {
		return nil, err
	}

	measured.label = t.label
	measured.detail = t.detail
	measured.expected = t.expected
	measured.server = after.delta(before)
	return measured, nil
}

// snapshot reads the counters the report is built from.
func snapshot(ctx context.Context, client *http.Client, base string) (counters, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/metrics", nil)
	if err != nil {
		return counters{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return counters{}, fmt.Errorf("read /metrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return counters{}, fmt.Errorf("read /metrics: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return counters{}, fmt.Errorf("read /metrics: %w", err)
	}
	// The series this tool reads may legitimately be absent - a counter vector has no series before its
	// first use - so the check is for something /metrics always has, not for something it should.
	if !strings.Contains(string(body), "go_goroutines") {
		return counters{}, fmt.Errorf("%s/metrics did not answer with Prometheus data: is the API reachable "+
			"at that address, or did a proxy answer instead?", base)
	}
	return parseMetrics(strings.NewReader(string(body)))
}

// count formats a counter without a decimal point unless the server actually reported one.
func count(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
