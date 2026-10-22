// Package metrics defines every series this service exposes on /metrics.
//
// The names and the label values live here rather than at the call sites. A metric name is an interface -
// a dashboard, an alert, or a number in a README is written against it - and an instrument call with a
// mistyped label value does not fail: it creates a series that never appears, and the only way to notice
// is to go looking for a graph that is not there. Keeping the vocabulary in one file makes that typo a
// compile error.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// What a request to /r/:code was answered with. "not_found" covers an expired link as well: the API
// answers both with the same 404, so counting them apart here would describe a difference no caller sees.
//
// OutcomeUnknown is the value a handler starts from and should never reach a response: a branch that
// answers without naming itself leaves this series behind, which is how a missing measurement announces
// itself instead of quietly not existing.
const (
	OutcomeUnknown      = "unknown"
	OutcomeRedirect     = "redirect"
	OutcomeGuidePage    = "guide_page"
	OutcomePasswordPage = "password_page"
	OutcomeNotFound     = "not_found"
	OutcomeUnsafeTarget = "unsafe_target"
	// OutcomeBlockedTarget is a target on the blocked list. It is counted apart from unsafe_target because
	// the two call for different reactions: an unsafe scheme is a bug or an old row, while a blocked
	// target is a decision somebody made and may have to defend.
	OutcomeBlockedTarget = "blocked_target"
	// OutcomeBlockedLink is a link an operator stopped. It is counted apart from blocked_target because
	// the two are decided by different things - one by the target's host, one by a person - and the second
	// is the one that ends up in a complaint.
	OutcomeBlockedLink = "blocked_link"
	// OutcomeGroupPage is a WeChat group code shown to the visitor, and OutcomeGroupEmpty is a group pool
	// with nothing left to show. The second is not a failure of the link: it is the state a group reaches
	// when it is full, and it is counted apart so the owner can see it happening.
	OutcomeGroupPage  = "group_page"
	OutcomeGroupEmpty = "group_empty"
)

// What the short-code cache did with a lookup.
//
// hit and negative_hit are both answers served without PostgreSQL, which is what makes the hit ratio a
// statement about the database rather than about Redis alone. miss is a lookup that reached the database,
// and negative_stored is a database miss that was then remembered.
const (
	CacheHit            = "hit"
	CacheMiss           = "miss"
	CacheNegativeHit    = "negative_hit"
	CacheNegativeStored = "negative_stored"
)

// What happened to a click on its way to the store.
//
// There is one route now - the row is written on a goroutine the redirect handler spawns - so these three
// are the whole story: written, refused, or crashed.
const (
	ClickWritten     = "written"
	ClickWriteFailed = "write_failed"
	// ClickPanicked is the click pipeline crashing rather than returning an error. It should never appear:
	// recording a click runs on its own goroutine, where a panic would end the process instead of failing a
	// request - so the counter existing at all is a bug report.
	ClickPanicked = "panicked"
)

var (
	// Redirects counts what the redirect endpoint answered.
	Redirects = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kada_redirect_requests_total",
		Help: "Requests to a short link, by what they were answered with.",
	}, []string{"outcome"})

	// RedirectDuration measures how long that took, as the handler's own time: it starts when the handler
	// is entered and ends when it has written the answer, so it excludes everything the client and the
	// network add on top.
	//
	// The buckets start below a millisecond on purpose. This path is a Redis GET and a redirect when the
	// cache is warm, and a histogram whose smallest bucket is 10ms would put almost every request in the
	// first bucket and answer nothing.
	RedirectDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kada_redirect_duration_seconds",
		Help:    "Time the redirect handler took to answer, by outcome.",
		Buckets: []float64{0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"outcome"})

	// CacheLookups counts what the short-code cache did. It is the only evidence that the cache earns its
	// place: "there is a cache" is a statement about code, and this is the statement about behavior.
	CacheLookups = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kada_link_cache_total",
		Help: "Short-code cache lookups by result.",
	}, []string{"result"})

	// ClickEvents counts what happened to a click on its way to the store.
	ClickEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kada_click_events_total",
		Help: "Click events by what happened to them.",
	}, []string{"result"})
)

// Redirect records one answered request: what it was answered with, and how long that took.
//
// The two are always recorded together, which is why they share a function: a latency histogram whose
// labels were filled in by a different call site than the counter would eventually disagree with it, and
// the disagreement would be invisible.
func Redirect(outcome string, elapsed time.Duration) {
	Redirects.WithLabelValues(outcome).Inc()
	RedirectDuration.WithLabelValues(outcome).Observe(elapsed.Seconds())
}

// CacheLookup records what the cache did with one lookup.
func CacheLookup(result string) {
	CacheLookups.WithLabelValues(result).Inc()
}

// ClickEvent records what happened to one click event.
func ClickEvent(result string) {
	ClickEvents.WithLabelValues(result).Inc()
}
