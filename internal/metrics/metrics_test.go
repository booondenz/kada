package metrics

import (
	"fmt"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// exposition renders exactly what /metrics answers, because that text is the only interface this package
// has: a series that does not appear in it does not exist for a dashboard, an alert or a number in the
// README, and a bucket that is missing from it cannot be summed by anything downstream.
func exposition(t *testing.T) string {
	t.Helper()

	w := httptest.NewRecorder()
	promhttp.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("the metrics handler answered %d", w.Code)
	}
	return w.Body.String()
}

// seriesValues parses the exposition into "name{labels}" -> value.
//
// Tests assert differences rather than absolute values: these vectors live in one process-wide registry,
// so an absolute assertion would depend on which tests ran before it - and would break the day somebody
// adds a test, which is a bad reason for a test to break.
func seriesValues(t *testing.T, body string) map[string]float64 {
	t.Helper()

	values := make(map[string]float64)
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		space := strings.LastIndex(line, " ")
		if space < 0 {
			continue
		}
		var value float64
		if _, err := fmt.Sscanf(line[space+1:], "%g", &value); err != nil {
			t.Fatalf("cannot read the value of %q: %v", line, err)
		}
		values[line[:space]] = value
	}
	return values
}

// tolerance is how close two float64s have to be to count as equal. The exposition writes a decimal
// representation of a sum of times, and 0.0002 + 0.002 is not exactly 0.0022 in binary, so an equality
// test on the sum would fail for a reason that has nothing to do with the code.
const tolerance = 1e-9

func delta(t *testing.T, before, after map[string]float64, series string) float64 {
	t.Helper()
	return after[series] - before[series]
}

// movedBy reports whether the series moved by want. The label order in the key is the one the exposition
// uses: the encoder sorts labels by name, so `le` comes before `outcome`.
func assertMoved(t *testing.T, before, after map[string]float64, series string, want float64) {
	t.Helper()
	if got := delta(t, before, after, series); math.Abs(got-want) > tolerance {
		t.Errorf("%s moved by %v, want %v", series, got, want)
	}
}

// A redirect is counted and timed, and the timing lands in the buckets that can actually tell one request
// from another: a cache-warm redirect is hundreds of microseconds, so the ladder has to start below a
// millisecond or every observation sits in the first bucket.
func TestTheRedirectSeriesCarryCountAndLatency(t *testing.T) {
	before := seriesValues(t, exposition(t))

	Redirect(OutcomeRedirect, 200*time.Microsecond)
	Redirect(OutcomeRedirect, 2*time.Millisecond)
	Redirect(OutcomeGuidePage, 30*time.Millisecond)

	after := seriesValues(t, exposition(t))

	for _, tc := range []struct {
		series string
		want   float64
	}{
		{`kada_redirect_requests_total{outcome="redirect"}`, 2},
		{`kada_redirect_duration_seconds_count{outcome="redirect"}`, 2},
		// 200µs lands in the first bucket that can hold it, and 2ms in le="0.0025": the ladder is what makes
		// the difference between a cache-warm redirect and a database read visible at all. The encoder
		// appends le after the vector's own labels rather than sorting the two together, which is why it
		// reads outcome first here.
		{`kada_redirect_duration_seconds_bucket{outcome="redirect",le="0.0001"}`, 0},
		{`kada_redirect_duration_seconds_bucket{outcome="redirect",le="0.00025"}`, 1},
		{`kada_redirect_duration_seconds_bucket{outcome="redirect",le="0.001"}`, 1},
		{`kada_redirect_duration_seconds_bucket{outcome="redirect",le="0.0025"}`, 2},
		// A histogram has to sum to +Inf, which is what a quantile is computed from.
		{`kada_redirect_duration_seconds_bucket{outcome="redirect",le="+Inf"}`, 2},
		{`kada_redirect_duration_seconds_sum{outcome="redirect"}`, 0.0022},
		{`kada_redirect_requests_total{outcome="guide_page"}`, 1},
	} {
		assertMoved(t, before, after, tc.series, tc.want)
	}
}

// The cache and click series are the ones a claim about behavior rests on: "the cache serves most
// requests" and "every click is written or the failure is visible" are both numbers this exposes or does
// not.
func TestTheCacheAndClickSeriesCarryTheirResults(t *testing.T) {
	before := seriesValues(t, exposition(t))

	CacheLookup(CacheHit)
	CacheLookup(CacheHit)
	CacheLookup(CacheMiss)
	CacheLookup(CacheNegativeHit)
	CacheLookup(CacheNegativeStored)

	ClickEvent(ClickWritten)
	ClickEvent(ClickWriteFailed)

	after := seriesValues(t, exposition(t))

	for _, tc := range []struct {
		series string
		want   float64
	}{
		{`kada_link_cache_total{result="hit"}`, 2},
		{`kada_link_cache_total{result="miss"}`, 1},
		{`kada_link_cache_total{result="negative_hit"}`, 1},
		{`kada_link_cache_total{result="negative_stored"}`, 1},
		{`kada_click_events_total{result="written"}`, 1},
		{`kada_click_events_total{result="write_failed"}`, 1},
		{`kada_click_events_total{result="panicked"}`, 0},
	} {
		assertMoved(t, before, after, tc.series, tc.want)
	}
}

// The Go runtime's own series come with the registry, and they are half of what a baseline is: the service
// series say what the code did, and these say what the process had to spend to do it.
func TestTheRuntimeSeriesAreExposed(t *testing.T) {
	body := exposition(t)

	for _, series := range []string{"go_goroutines ", "go_memstats_alloc_bytes ", "process_resident_memory_bytes "} {
		if !strings.Contains(body, "\n"+series) {
			t.Errorf("the exposition has no %s series", strings.TrimSpace(series))
		}
	}
}
