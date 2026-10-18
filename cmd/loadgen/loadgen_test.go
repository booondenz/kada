package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/booondenz/kada/internal/metrics"
)

func TestParseMetricsReadsOnlyTheSeriesItReports(t *testing.T) {
	// A body shaped like promhttp's: comments, the Go runtime's own series, a histogram for the same request
	// path (whose buckets must not be added to the request count), and the three series this tool reports.
	body := strings.Join([]string{
		`# HELP kada_link_cache_total Short-code cache lookups by result.`,
		`# TYPE kada_link_cache_total counter`,
		`kada_link_cache_total{result="hit"} 19800`,
		`kada_link_cache_total{result="miss"} 200`,
		`kada_link_cache_total{result="negative_stored"} 5`,
		`kada_redirect_requests_total{outcome="redirect"} 19990`,
		`kada_redirect_requests_total{outcome="not_found"} 10`,
		`kada_redirect_duration_seconds_bucket{outcome="redirect",le="0.0001"} 5`,
		`kada_redirect_duration_seconds_count{outcome="redirect"} 19990`,
		`kada_click_events_total{result="direct_write"} 20000`,
		`go_goroutines 42`,
		``,
	}, "\n")

	c, err := parseMetrics(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseMetrics: %v", err)
	}

	// Both outcomes add up: the counter answers "how many requests", not "how many succeeded".
	if want := 20000.0; c.redirects != want {
		t.Errorf("redirects = %v, want %v", c.redirects, want)
	}
	if got := c.cache[metrics.CacheHit]; got != 19800 {
		t.Errorf("cache hits = %v, want 19800", got)
	}
	if got := c.cache[metrics.CacheMiss]; got != 200 {
		t.Errorf("cache misses = %v, want 200", got)
	}
	if got := c.clicks[metrics.ClickDirectWrite]; got != 20000 {
		t.Errorf("direct writes = %v, want 20000", got)
	}

	// The hit ratio is the number the issue asked for, so it is checked against the parsed series rather
	// than against a literal: 19800 of 19800+200 lookups.
	if got, want := c.lookups(), 20000.0; got != want {
		t.Errorf("lookups = %v, want %v", got, want)
	}
	if got, want := c.answeredWithoutPostgres(), 19800.0; got != want {
		t.Errorf("answered without postgres = %v, want %v", got, want)
	}
}

func TestParseMetricsRejectsAValueItCannotRead(t *testing.T) {
	_, err := parseMetrics(strings.NewReader(`kada_link_cache_total{result="hit"} not-a-number`))
	if err == nil {
		t.Fatal("a malformed sample must be an error: reporting zero hits would be a plausible lie")
	}
}

func TestDeltaOfASeriesThatAppearedMidRunIsItsWholeValue(t *testing.T) {
	// A counter vector has no series until its first use, so the earlier snapshot of a cold server has
	// neither of these keys. Subtracting an absent key is the same as subtracting zero.
	before := counters{cache: map[string]float64{}, clicks: map[string]float64{}}
	after := counters{
		redirects: 7,
		cache:     map[string]float64{metrics.CacheMiss: 3, metrics.CacheNegativeStored: 3},
		clicks:    map[string]float64{metrics.ClickDirectWrite: 5},
	}

	got := after.delta(before)
	if got.redirects != 7 {
		t.Errorf("redirects = %v, want 7", got.redirects)
	}
	if got.cache[metrics.CacheMiss] != 3 || got.cache[metrics.CacheNegativeStored] != 3 {
		t.Errorf("cache delta = %v, want the whole values", got.cache)
	}
	if got.clicks[metrics.ClickDirectWrite] != 5 {
		t.Errorf("click delta = %v, want 5", got.clicks)
	}
}

func TestPercentileIsTheNearestRankOfASortedRun(t *testing.T) {
	// One millisecond apart, so the answer is the index and nothing else.
	hundred := make([]time.Duration, 100)
	for i := range hundred {
		hundred[i] = time.Duration(i+1) * time.Millisecond
	}

	tests := []struct {
		name   string
		sorted []time.Duration
		p      float64
		want   time.Duration
	}{
		{"empty", nil, 0.99, 0},
		{"a single sample is every percentile", []time.Duration{7 * time.Millisecond}, 0.99, 7 * time.Millisecond},
		{"p50 of an even run is the upper middle", []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond}, 0.5, 2 * time.Millisecond},
		{"p50 of a hundred is the fiftieth", hundred, 0.50, 50 * time.Millisecond},
		{"p99 of a hundred is the ninety-ninth", hundred, 0.99, 99 * time.Millisecond},
		{"the maximum is the last sample", hundred, 1, 100 * time.Millisecond},
		{"p0 is the first sample", hundred, 0, 1 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percentile(tt.sorted, tt.p); got != tt.want {
				t.Errorf("percentile(%v) = %v, want %v", tt.p, got, tt.want)
			}
		})
	}
}

func TestReportReadsBackWhatTheServerCounted(t *testing.T) {
	// The report is the only thing anybody reads out of a run, so the two numbers the issue asked for - the
	// tail latency and the hit ratio - have to be in it next to the counters they came from.
	tal := &tally{
		label:       "working set",
		detail:      "200 codes that exist",
		requests:    100,
		concurrency: 5,
		expected:    http.StatusFound,
		wall:        2 * time.Second,
		latencies:   []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond},
		status:      map[int]int{http.StatusFound: 100},
		server: counters{
			redirects: 100,
			cache:     map[string]float64{metrics.CacheHit: 90, metrics.CacheMiss: 10},
			clicks:    map[string]float64{metrics.ClickDirectWrite: 99},
		},
	}

	got := report(tal)
	for _, want := range []string{
		"50 req/s",
		"302: 100",
		"hit 90  miss 10",
		"90.0% of 100 lookups",
		"direct_write 99",
		"counted 100 redirects",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not contain %q:\n%s", want, got)
		}
	}

	// The markdown table is quoted into the README, so its columns are part of the interface too.
	if row := markdownTable([]*tally{tal}); !strings.Contains(row, "| working set | 100 | 50 req/s | 2ms | 3ms | 90.0% |") {
		t.Errorf("the markdown row does not carry the measured numbers:\n%s", row)
	}
}

func TestCodesAndAddressesMatchWhatTheSeederWrites(t *testing.T) {
	codes := codesWithPrefix(workingPrefix, 3)
	want := []string{"load-0001", "load-0002", "load-0003"}
	for i := range want {
		if codes[i] != want[i] {
			t.Errorf("code %d = %q, want %q (the seed inserts $1 || lpad(n, 4, '0'))", i, codes[i], want[i])
		}
	}

	// The limiter keys on the address, so a pool with repeats would count twice for one visitor; and the
	// API parses the header, so a value net.ParseIP rejects would fall back to the loopback address and
	// throttle the whole run.
	ips := syntheticIPs(4000)
	seen := make(map[string]bool, len(ips))
	for _, ip := range ips {
		if net.ParseIP(ip) == nil {
			t.Fatalf("synthetic address %q is not an IP", ip)
		}
		if seen[ip] {
			t.Fatalf("synthetic address %q appears twice", ip)
		}
		seen[ip] = true
	}
	if !strings.HasPrefix(ips[0], "10.") {
		t.Errorf("addresses must be private, got %q", ips[0])
	}
}

func TestDriveSendsTheRedirectAndDoesNotFollowIt(t *testing.T) {
	var (
		mu        sync.Mutex
		userAgent = map[string]bool{}
		clients   = map[string]bool{}
		codes     = map[string]bool{}
		followed  int
	)

	// The target is on the same test server, so following the redirect would be observable: a client that
	// followed it would hit /target and the measurement would be of that request instead.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/target" {
			followed++
			w.WriteHeader(http.StatusOK)
			return
		}
		userAgent[r.Header.Get("User-Agent")] = true
		clients[r.Header.Get("X-Real-IP")] = true
		codes[strings.TrimPrefix(r.URL.Path, "/r/")] = true
		w.Header().Set("Location", "/target")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	d := &driver{
		client: newClient(config{concurrency: 4, timeout: 5 * time.Second}),
		base:   server.URL,
		ips:    syntheticIPs(8),
	}
	workingSet := codesWithPrefix(workingPrefix, 3)

	got, err := d.drive(context.Background(), workingSet, 60, 4)
	if err != nil {
		t.Fatalf("drive: %v", err)
	}

	if got.status[http.StatusFound] != 60 {
		t.Errorf("answers = %v, want 60 redirects", got.status)
	}
	if n := got.notAnsweredWith(http.StatusFound); n != 0 {
		t.Errorf("nonRedirects = %d, want 0", n)
	}
	if len(got.latencies) != 60 {
		t.Fatalf("recorded %d latencies, want 60 (one per request)", len(got.latencies))
	}
	for i := 1; i < len(got.latencies); i++ {
		if got.latencies[i] < got.latencies[i-1] {
			t.Fatalf("latencies are not sorted: %v before %v", got.latencies[i-1], got.latencies[i])
		}
	}
	if got.wall <= 0 {
		t.Error("wall clock time is not measured, so throughput would be a division by zero")
	}

	mu.Lock()
	defer mu.Unlock()
	if followed != 0 {
		t.Errorf("the client followed the redirect %d times: the 302 is the answer being measured", followed)
	}
	// One address per request in rotation: with 60 requests over 8 addresses every address must be used,
	// which is what keeps one address under the 120 a minute the limiter allows.
	if len(clients) != 8 {
		t.Errorf("requests used %d addresses, want 8", len(clients))
	}
	if len(codes) != len(workingSet) {
		t.Errorf("requests asked for %d codes, want %d", len(codes), len(workingSet))
	}
	if len(userAgent) != 1 {
		t.Fatalf("requests carried %d user agents, want the one browser header", len(userAgent))
	}
	for ua := range userAgent {
		if ua != browserUA {
			t.Errorf("user agent = %q, want the browser string that makes the answer a redirect", ua)
		}
	}
}

func TestDriveReportsTheLimiterRatherThanCountingItAsARedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	d := &driver{client: newClient(config{concurrency: 2, timeout: 5 * time.Second}), base: server.URL, ips: syntheticIPs(2)}
	got, err := d.drive(context.Background(), codesWithPrefix(workingPrefix, 2), 10, 2)
	if err != nil {
		t.Fatalf("drive: %v", err)
	}

	if got.status[http.StatusTooManyRequests] != 10 {
		t.Errorf("answers = %v, want ten 429s", got.status)
	}
	// run() turns this into its failure exit: a rate-limited run still printed numbers, but they are not the
	// numbers that were asked for.
	if n := got.notAnsweredWith(http.StatusFound); n != 10 {
		t.Errorf("nonRedirects = %d, want 10", n)
	}
}

func TestDriveKeepsDistinctTransportFailuresDistinct(t *testing.T) {
	// A closed port refuses every connection: 20,000 identical errors have to read as one line with a count.
	d := &driver{client: newClient(config{concurrency: 4, timeout: 2 * time.Second}), base: "http://127.0.0.1:1", ips: syntheticIPs(2)}
	got, err := d.drive(context.Background(), codesWithPrefix(workingPrefix, 2), 20, 4)
	if err != nil {
		t.Fatalf("drive: %v", err)
	}

	if len(got.status) != 0 {
		t.Errorf("answers = %v, want none: every request failed to connect", got.status)
	}
	if len(got.failures) != 1 {
		t.Fatalf("failures = %v, want one distinct error", got.failures)
	}
	if !strings.Contains(got.failures[0], "x"+strconv.Itoa(20)) {
		t.Errorf("failure %q should carry the count, so the line says how often it happened", got.failures[0])
	}
}
