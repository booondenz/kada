package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/booondenz/kada/internal/metrics"
)

// counters is the part of /metrics this tool reads, summed over each series' outcome label.
//
// The label values come from internal/metrics rather than from string literals: that package is the
// vocabulary the server writes, so a renamed outcome breaks this build instead of silently reporting a
// series that no longer exists.
type counters struct {
	redirects float64
	cache     map[string]float64
	clicks    map[string]float64
}

// delta is what happened between two snapshots. A series absent from the earlier snapshot contributes all
// of its value, which is what a counter's first appearance means.
func (c counters) delta(earlier counters) counters {
	d := counters{
		redirects: c.redirects - earlier.redirects,
		cache:     make(map[string]float64, len(c.cache)),
		clicks:    make(map[string]float64, len(c.clicks)),
	}
	for result, value := range c.cache {
		d.cache[result] = value - earlier.cache[result]
	}
	for result, value := range c.clicks {
		d.clicks[result] = value - earlier.clicks[result]
	}
	return d
}

// lookups is how many times the short-code cache was consulted. Every request to a short link consults it
// exactly once, so this doubles as the denominator of the hit ratio.
func (c counters) lookups() float64 {
	return c.cache[metrics.CacheHit] + c.cache[metrics.CacheMiss] + c.cache[metrics.CacheNegativeHit]
}

// answeredWithoutPostgres is the number worth reporting, and it is why the negative cache exists at all:
// a hit and a negative hit are both answers PostgreSQL never saw.
func (c counters) answeredWithoutPostgres() float64 {
	return c.cache[metrics.CacheHit] + c.cache[metrics.CacheNegativeHit]
}

// parseMetrics reads the series above out of the Prometheus text format.
//
// A full parser would mean depending on prometheus/common; the three series read here have one label each
// and a fixed shape, so a line split is enough. Anything unrecognized - the Go runtime's series, the
// histogram buckets, the HELP and TYPE comments - is skipped, not rejected: /metrics carries a lot that
// this tool does not report on.
func parseMetrics(r io.Reader) (counters, error) {
	c := counters{cache: map[string]float64{}, clicks: map[string]float64{}}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		split := strings.LastIndexByte(line, ' ')
		if split < 0 {
			continue
		}
		sample := line[:split]
		value, err := strconv.ParseFloat(line[split+1:], 64)
		if err != nil {
			return counters{}, fmt.Errorf("read %q from /metrics: %w", line, err)
		}
		switch {
		case strings.HasPrefix(sample, "kada_redirect_requests_total"):
			c.redirects += value
		case strings.HasPrefix(sample, "kada_link_cache_total"):
			c.cache[labelValue(sample, "result")] += value
		case strings.HasPrefix(sample, "kada_click_events_total"):
			c.clicks[labelValue(sample, "result")] += value
		}
	}
	if err := scanner.Err(); err != nil {
		return counters{}, err
	}
	return c, nil
}

// labelValue returns one label of a sample line: the hit in `name{result="hit"} 3`.
func labelValue(sample, key string) string {
	_, rest, found := strings.Cut(sample, key+`="`)
	if !found {
		return ""
	}
	value, _, _ := strings.Cut(rest, `"`)
	return value
}

// tally is one measured scenario: what the client saw, and what the server counted while it ran.
type tally struct {
	label       string
	detail      string
	requests    int
	concurrency int
	expected    int // the status the scenario is supposed to be answered with
	wall        time.Duration
	latencies   []time.Duration // sorted, one per request
	status      map[int]int
	failures    []string // the distinct transport errors, most frequent first
	server      counters // the /metrics deltas across the scenario
}

// rate is the throughput the client achieved: requests over the wall-clock time the scenario took.
func (t *tally) rate() float64 {
	if t.wall <= 0 {
		return 0
	}
	return float64(t.requests) / t.wall.Seconds()
}

// notAnsweredWith is how many answers were not the expected one: a 302 for a working set, a 404 for an
// absent code. A 429 counted here is not noise: it means the address pool was too small for the request
// rate, and what the run measured was the limiter.
func (t *tally) notAnsweredWith(status int) int {
	n := 0
	for code, count := range t.status {
		if code != status {
			n += count
		}
	}
	return n
}

// hitRatio is the share of cache lookups that were answered without PostgreSQL.
func (t *tally) hitRatio() float64 {
	lookups := t.server.lookups()
	if lookups <= 0 {
		return 0
	}
	return t.server.answeredWithoutPostgres() / lookups
}

// percentile returns the p-th percentile of a sorted duration slice by nearest rank, the definition hey
// and k6 use: a p99 of 20,000 samples is the 19,800th of them.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	rank = max(rank, 0)
	rank = min(rank, len(sorted)-1)
	return sorted[rank]
}

// report is one scenario the way it is meant to be read: what the client saw first, then the server's own
// accounting of the same requests.
func report(t *tally) string {
	lines := []string{
		fmt.Sprintf("%s (%s)", t.label, t.detail),
		fmt.Sprintf("  requests   %d in %s = %.0f req/s, %d concurrent",
			t.requests, t.wall.Round(time.Millisecond), t.rate(), t.concurrency),
		fmt.Sprintf("  latency    p50 %v  p95 %v  p99 %v  max %v",
			percentile(t.latencies, 0.50).Round(time.Microsecond),
			percentile(t.latencies, 0.95).Round(time.Microsecond),
			percentile(t.latencies, 0.99).Round(time.Microsecond),
			percentile(t.latencies, 1).Round(time.Microsecond)),
		fmt.Sprintf("  answers    %s", statusLine(t.status)),
	}
	for _, failure := range t.failures {
		lines = append(lines, fmt.Sprintf("  failure    %s", failure))
	}
	lines = append(lines,
		fmt.Sprintf("  cache      %s", cacheLine(t.server.cache)),
		fmt.Sprintf("  hit ratio  %.1f%% of %s lookups never reached PostgreSQL",
			t.hitRatio()*100, count(t.server.lookups())),
		fmt.Sprintf("  clicks     %s", clickLine(t.server.clicks)),
		fmt.Sprintf("  server     counted %s redirects, the client saw %d",
			count(t.server.redirects), t.requests),
	)
	return strings.Join(lines, "\n") + "\n"
}

// markdownTable prints the numbers the README quotes, so the table is copied out of a run rather than
// retyped into one.
func markdownTable(tallies []*tally) string {
	lines := []string{
		"| scenario | requests | throughput | p50 | p99 | cache hit ratio |",
		"|---|---|---|---|---|---|",
	}
	for _, t := range tallies {
		lines = append(lines, fmt.Sprintf("| %s | %d | %.0f req/s | %v | %v | %.1f%% |",
			t.label, t.requests, t.rate(),
			percentile(t.latencies, 0.50).Round(time.Microsecond),
			percentile(t.latencies, 0.99).Round(time.Microsecond),
			t.hitRatio()*100))
	}
	return strings.Join(lines, "\n") + "\n"
}

// statusLine summarizes the answers by status code, in ascending order so two runs print the same line.
func statusLine(status map[int]int) string {
	codes := make([]int, 0, len(status))
	for code := range status {
		codes = append(codes, code)
	}
	sort.Ints(codes)

	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("%d: %d", code, status[code]))
	}
	return strings.Join(parts, "  ")
}

// cacheLine names every result the server counts, including the ones at zero: a scenario is read by
// comparing these four numbers, and the one that did not happen is part of the comparison.
func cacheLine(c map[string]float64) string {
	results := []string{
		metrics.CacheHit, metrics.CacheMiss, metrics.CacheNegativeHit, metrics.CacheNegativeStored,
	}
	parts := make([]string, 0, len(results))
	for _, result := range results {
		parts = append(parts, fmt.Sprintf("%s %s", result, count(c[result])))
	}
	return strings.Join(parts, "  ")
}

// clickLine is what happened to the clicks the redirects produced. It is the pipeline's own health, and a
// baseline taken while it was degraded should say so rather than look quiet.
func clickLine(c map[string]float64) string {
	results := []string{
		metrics.ClickWritten, metrics.ClickWriteFailed, metrics.ClickPanicked,
	}
	parts := make([]string, 0, len(results))
	for _, result := range results {
		parts = append(parts, fmt.Sprintf("%s %s", result, count(c[result])))
	}
	return strings.Join(parts, "  ")
}

// failureKind reduces a transport error to what it has in common with every other request's failure.
//
// The URL is part of a net/http error and differs with every short code, so grouping on the message alone
// would report "load-0001 refused, load-0002 refused, ..." and hide the one fact worth printing: the
// server refused all of them.
func failureKind(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Op + " " + urlErr.Err.Error()
	}
	return err.Error()
}

// topFailures keeps the most frequent distinct errors: one refused connection 20,000 times is one problem
// and has to read as one line.
func topFailures(counts map[string]int, keep int) []string {
	type failure struct {
		message string
		count   int
	}
	ranked := make([]failure, 0, len(counts))
	for message, n := range counts {
		ranked = append(ranked, failure{message: message, count: n})
	}
	slices.SortFunc(ranked, func(a, b failure) int {
		if a.count != b.count {
			return b.count - a.count
		}
		return strings.Compare(a.message, b.message)
	})

	out := make([]string, 0, min(keep, len(ranked)))
	for _, f := range ranked[:min(keep, len(ranked))] {
		out = append(out, fmt.Sprintf("%s (x%d)", f.message, f.count))
	}
	return out
}
