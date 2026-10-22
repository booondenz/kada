package redirect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/metrics"
)

// mockLinkService is used for redirect handler tests.
type mockLinkService struct {
	getByCode     func(ctx context.Context, code string) (*domain.LinkInfo, error)
	checkPassword func(ctx context.Context, code, password string) (bool, *domain.LinkInfo, error)
	logClick      func(ctx context.Context, event domain.ClickEvent)
	buildShortURL func(domain, code string) string
	blocked       func(targetURL string) (string, bool)
	pickTarget    func(link *domain.LinkInfo) *domain.LinkTargetInfo
}

func (m *mockLinkService) GetByCode(ctx context.Context, code string) (*domain.LinkInfo, error) {
	if m.getByCode != nil {
		return m.getByCode(ctx, code)
	}
	return &domain.LinkInfo{
		ID:          1,
		ShortCode:   code,
		ShortURL:    "https://kada.click/r/" + code,
		OriginalURL: "https://example.com/target",
		Domain:      "kada.click",
		ClickCount:  0,
		IsActive:    true,
	}, nil
}

func (m *mockLinkService) CheckPassword(ctx context.Context, code, password string) (bool, *domain.LinkInfo, error) {
	if m.checkPassword != nil {
		return m.checkPassword(ctx, code, password)
	}
	return true, &domain.LinkInfo{OriginalURL: "https://example.com/target"}, nil
}

func (m *mockLinkService) LogClick(ctx context.Context, event domain.ClickEvent) {
	if m.logClick != nil {
		m.logClick(ctx, event)
	}
}

func (m *mockLinkService) BuildShortURL(domain, code string) string {
	if m.buildShortURL != nil {
		return m.buildShortURL(domain, code)
	}
	return "https://" + domain + "/r/" + code
}

func (m *mockLinkService) Blocked(targetURL string) (string, bool) {
	if m.blocked != nil {
		return m.blocked(targetURL)
	}
	return "", false
}

func (m *mockLinkService) PickTarget(link *domain.LinkInfo) *domain.LinkTargetInfo {
	if m.pickTarget != nil {
		return m.pickTarget(link)
	}
	return nil
}

// The User-Agents these tests use. The WeChat one is the interesting case: it is what the in-app browser
// sends, and it is also what WeChat's preview fetcher sends, which is why nothing here can tell them apart
// and the guide page has to.
const (
	wechatUA    = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 MicroMessenger/8.0.40"
	browserUA   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36"
	googlebotUA = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
)

// clickRecorder collects the click events the handler hands to the service.
//
// They arrive on a channel rather than in a slice because the handler logs them from a goroutine - nothing
// about recording a click may sit between the visitor and the redirect - so the channel is what
// synchronizes the test with that goroutine instead of racing it.
type clickRecorder struct {
	events chan domain.ClickEvent
}

func newClickRecorder() *clickRecorder {
	return &clickRecorder{events: make(chan domain.ClickEvent, 16)}
}

func (r *clickRecorder) record(_ context.Context, event domain.ClickEvent) {
	r.events <- event
}

// collect reads exactly want events, failing instead of hanging when a request that should have been
// recorded was not.
func (r *clickRecorder) collect(t *testing.T, want int) []domain.ClickEvent {
	t.Helper()

	events := make([]domain.ClickEvent, 0, want)
	for len(events) < want {
		select {
		case e := <-r.events:
			events = append(events, e)
		case <-time.After(2 * time.Second):
			t.Fatalf("recorded %d of %d click events: %+v", len(events), want, events)
		}
	}
	return events
}

// counted mirrors what ClickStore does with the column: only a visit moves a counter. Asserting on this
// rather than on the labels is what makes these the tests for the numbers.
func counted(events []domain.ClickEvent) int {
	n := 0
	for _, e := range events {
		if e.Kind == domain.ClickVisit {
			n++
		}
	}
	return n
}

// ========== Redirect ==========

func TestRedirect_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return nil, domain.ErrLinkNotFound
		},
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestRedirect_Browser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandler(&mockLinkService{})

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 for browser, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRedirect_WechatReturnsGuidePage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandler(&mockLinkService{})

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 MicroMessenger/8.0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for wechat guide page, got %d", w.Code)
	}

	body := w.Body.String()
	if len(body) < 100 {
		t.Error("guide page HTML seems too short")
	}
}

func TestRedirect_QQReturnsGuidePage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandler(&mockLinkService{})

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 QQ/9.0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for QQ guide page, got %d", w.Code)
	}
}

// ========== Unsafe scheme blocking ==========

func TestRedirect_UnsafeSchemeBlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return &domain.LinkInfo{
				ID:          1,
				ShortCode:   code,
				OriginalURL: "javascript:alert(document.cookie)",
				Domain:      "kada.click",
				IsActive:    true,
			}, nil
		},
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	// The WeChat UA goes through the guide page, which has the largest attack surface.
	req := httptest.NewRequest("GET", "/r/evil123", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 MicroMessenger/8.0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for javascript: target, got %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "not supported") && !strings.Contains(body, "Link unavailable") {
		t.Errorf("expected blocked page, got: %s", body)
	}
}

// ========== Blocked targets ==========

// A link created before its host was blocked has to stop being served, which is why the list is asked on
// every request instead of being decided once and cached with the link.
func TestRedirect_BlockedTargetIsNotServed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		blocked: func(string) (string, bool) { return "gambling", true },
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", browserUA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a blocked target, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Errorf("a blocked target must not redirect, got Location %q", loc)
	}
	if body := w.Body.String(); !strings.Contains(body, "链接已停止访问") || !strings.Contains(body, "gambling") {
		t.Errorf("expected the stopped page naming the category, got: %s", body)
	}
	if body := w.Body.String(); !strings.Contains(body, "/report?code=abc123") {
		t.Errorf("the stopped page must offer a way to report it, got: %s", body)
	}
}

// A link an operator stopped is refused whatever its target is, and without naming a category: the note
// the operator wrote is not the visitor's business.
func TestRedirect_LinkStoppedByAnOperatorIsNotServed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	blockedAt := time.Now()
	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return &domain.LinkInfo{
				ID:            1,
				ShortCode:     code,
				OriginalURL:   "https://example.com/",
				Domain:        "kada.click",
				IsActive:      true,
				BlockedAt:     &blockedAt,
				BlockedReason: stringPtr("operator note"),
			}, nil
		},
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", browserUA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a stopped link, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Errorf("a stopped link must not redirect, got Location %q", loc)
	}
	body := w.Body.String()
	if !strings.Contains(body, "链接已停止访问") {
		t.Errorf("expected the stopped page, got: %s", body)
	}
	if strings.Contains(body, "operator note") {
		t.Error("the operator's note must not be shown to the visitor")
	}
}

func stringPtr(s string) *string { return &s }

// ========== Group codes ==========

// A group code is shown rather than redirected to: the visitor has to scan it with a second device, and a
// 302 would hand them an image file with no instruction.
func TestRedirect_ShowsAGroupCodeAsAPage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := newClickRecorder()
	target := domain.LinkTargetInfo{
		ID:    5,
		URL:   "https://kada.click/f/abc123",
		Kind:  domain.TargetWechatGroup,
		Label: stringPtr("三群"),
	}
	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return &domain.LinkInfo{
				ID:          1,
				ShortCode:   code,
				OriginalURL: "https://example.com/fallback",
				Domain:      "kada.click",
				IsActive:    true,
				Targets:     []domain.LinkTargetInfo{target},
			}, nil
		},
		pickTarget: func(*domain.LinkInfo) *domain.LinkTargetInfo { return &target },
		logClick:   rec.record,
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", wechatUA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Errorf("a group code must not redirect, got Location %q", loc)
	}

	body := w.Body.String()
	for _, want := range []string{"加入群聊", "https://kada.click/f/abc123", "三群", "/r/abc123/visit"} {
		if !strings.Contains(body, want) {
			t.Errorf("the group page is missing %q", want)
		}
	}

	events := rec.collect(t, 1)
	if events[0].TargetID == nil || *events[0].TargetID != target.ID {
		t.Errorf("recorded target = %v, want %d", events[0].TargetID, target.ID)
	}
}

// A group pool with nothing left is a state, not a broken link: the page says so with a 200, where a plain
// pool that cannot serve is still a 404.
func TestRedirect_AGroupPoolWithNothingLeftSaysSo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	expired := time.Now().Add(-time.Hour)
	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return &domain.LinkInfo{
				ID:          1,
				ShortCode:   code,
				OriginalURL: "https://example.com/fallback",
				Domain:      "kada.click",
				IsActive:    true,
				Targets: []domain.LinkTargetInfo{{
					ID: 1, URL: "https://kada.click/f/old", Kind: domain.TargetWechatGroup, ExpiresAt: &expired,
				}},
			}, nil
		},
		pickTarget: func(*domain.LinkInfo) *domain.LinkTargetInfo { return nil },
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", browserUA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: an exhausted group is not a broken link", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "群已满") {
		t.Errorf("expected the no-group page, got: %s", body)
	}
}

// A link with a pool is served from the pool, and the click row says which destination took the visitor -
// which is the only answer a split can give afterwards.
func TestRedirect_ServesAPoolTargetAndRecordsWhichOne(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := newClickRecorder()
	target := domain.LinkTargetInfo{ID: 42, URL: "https://example.com/group-b", Weight: 1, Enabled: true}
	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return &domain.LinkInfo{
				ID:          1,
				ShortCode:   code,
				OriginalURL: "https://example.com/fallback",
				Domain:      "kada.click",
				IsActive:    true,
				Targets:     []domain.LinkTargetInfo{target},
			}, nil
		},
		pickTarget: func(*domain.LinkInfo) *domain.LinkTargetInfo { return &target },
		logClick:   rec.record,
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", browserUA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if loc := w.Header().Get("Location"); loc != target.URL {
		t.Errorf("Location = %q, want the pool target %q", loc, target.URL)
	}

	events := rec.collect(t, 1)
	if events[0].TargetID == nil || *events[0].TargetID != target.ID {
		t.Errorf("recorded target = %v, want %d", events[0].TargetID, target.ID)
	}
}

// A pool that cannot take this visitor is not served at all. original_url is where the link pointed before
// it had a pool, and sending traffic there is the "never the stale URL" half of the rule.
func TestRedirect_APoolWithNothingToServeDoesNotFallBack(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return &domain.LinkInfo{
				ID:          1,
				ShortCode:   code,
				OriginalURL: "https://example.com/fallback",
				Domain:      "kada.click",
				IsActive:    true,
				Targets:     []domain.LinkTargetInfo{{ID: 7, URL: "https://example.com/full", Enabled: false}},
			}, nil
		},
		pickTarget: func(*domain.LinkInfo) *domain.LinkTargetInfo { return nil },
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", browserUA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Errorf("an exhausted pool must not redirect anywhere, got Location %q", loc)
	}
}

// The check is on the target, so a guide page - the branch that would otherwise show the target's host and
// a button that opens it - must not be served either.
func TestRedirect_BlockedTargetIsNotServedAsAGuidePage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		blocked: func(string) (string, bool) { return "phishing", true },
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/abc123", nil)
	req.Header.Set("User-Agent", wechatUA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 inside the WeChat browser too, got %d", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, "example.com") {
		t.Error("the guide page leaked the blocked target's host")
	}
}

func TestVerifyPassword_BlockedTargetIsNotServed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		checkPassword: func(ctx context.Context, code, password string) (bool, *domain.LinkInfo, error) {
			return true, &domain.LinkInfo{ID: 1, OriginalURL: "https://evil.example/"}, nil
		},
		blocked: func(string) (string, bool) { return "drugs", true },
	}
	h := NewHandler(svc)

	r := gin.New()
	r.POST("/r/:code/verify-password", h.VerifyPassword)

	req := httptest.NewRequest("POST", "/r/abc123/verify-password", strings.NewReader("password=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("a correct password is not permission to reach a blocked target: got %d", w.Code)
	}
}

func TestVerifyPassword_UnsafeSchemeBlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		checkPassword: func(ctx context.Context, code, password string) (bool, *domain.LinkInfo, error) {
			return true, &domain.LinkInfo{OriginalURL: "data:text/html,<script>alert(1)</script>"}, nil
		},
	}
	h := NewHandler(svc)

	r := gin.New()
	r.POST("/r/:code/verify-password", h.VerifyPassword)

	req := httptest.NewRequest("POST", "/r/evil123/verify-password", strings.NewReader("password=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for data: target, got %d", w.Code)
	}
}

// ========== QR Code ==========

func TestQRCode_Valid(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandler(&mockLinkService{})

	r := gin.New()
	r.GET("/r/:code/qrcode", h.QRCode)

	req := httptest.NewRequest("GET", "/r/abc123/qrcode", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("expected image/png, got %s", w.Header().Get("Content-Type"))
	}
	if len(w.Body.Bytes()) < 100 {
		t.Error("QR code PNG too small")
	}
}

func TestQRCode_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return nil, domain.ErrLinkNotFound
		},
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code/qrcode", h.QRCode)

	req := httptest.NewRequest("GET", "/r/nonexistent/qrcode", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ========== What counts as a visit ==========

func newRedirectServer(t *testing.T, rec *clickRecorder) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	h := NewHandler(&mockLinkService{logClick: rec.record})
	r := gin.New()
	// No limiters: rate limiting is the middleware's business and is tested where it lives. These tests are
	// about what the routes record.
	h.RegisterRoutes(r, nil, nil)
	return r
}

// do sends one request with a User-Agent and returns the response.
func do(r *gin.Engine, method, path, userAgent string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// A prefetch and a visit are the same request with the same User-Agent: WeChat fetches the link to build a
// preview card with the in-app browser's own User-Agent, so the request alone cannot say which it is.
// What it can do is record the request - which is what keeps the amount of prefetch measurable - and
// refuse to count it.
func TestPrefetchIsRecordedButNotCounted(t *testing.T) {
	rec := newClickRecorder()
	r := newRedirectServer(t, rec)

	// One request, and nothing else: a crawler reads the HTML and stops there.
	if w := do(r, "GET", "/r/abc123", wechatUA); w.Code != http.StatusOK {
		t.Fatalf("expected the guide page, got %d", w.Code)
	}

	events := rec.collect(t, 1)
	if events[0].Kind != domain.ClickRequest {
		t.Errorf("the prefetch was recorded as %q, want %q", events[0].Kind, domain.ClickRequest)
	}
	if n := counted(events); n != 0 {
		t.Errorf("a prefetch moved %d counters", n)
	}
}

// The same request, followed by the page's own confirmation: a prefetcher does not run JavaScript, a person
// with a browser does.
func TestARequestFollowedByAConfirmationIsCountedOnce(t *testing.T) {
	rec := newClickRecorder()
	r := newRedirectServer(t, rec)

	do(r, "GET", "/r/abc123", wechatUA)
	if w := do(r, "POST", "/r/abc123/visit", wechatUA); w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from the confirmation, got %d", w.Code)
	}

	events := rec.collect(t, 2)
	if n := counted(events); n != 1 {
		t.Errorf("the confirmed visit moved %d counters, want exactly 1: %+v", n, events)
	}
}

// A client that is sent straight to the target never runs anything of ours, so its request is the only
// evidence that will ever exist. Dropping it would erase most of the traffic a normal browser produces.
func TestDirectBrowserRequestIsCounted(t *testing.T) {
	rec := newClickRecorder()
	r := newRedirectServer(t, rec)

	if w := do(r, "GET", "/r/abc123", browserUA); w.Code != http.StatusFound {
		t.Fatalf("expected 302 for a browser, got %d", w.Code)
	}

	events := rec.collect(t, 1)
	if events[0].Kind != domain.ClickVisit {
		t.Errorf("a browser request was recorded as %q, want %q", events[0].Kind, domain.ClickVisit)
	}
}

// The crawler list only holds names a browser never sends, so being on it is not a guess. Search engines
// are the traffic a link shortener cannot afford to count as readers: Googlebot follows every link it is
// given, whether or not anybody ever clicks it.
func TestCrawlerRequestIsRecordedButNotCounted(t *testing.T) {
	rec := newClickRecorder()
	r := newRedirectServer(t, rec)

	if w := do(r, "GET", "/r/abc123", googlebotUA); w.Code != http.StatusFound {
		t.Fatalf("expected the crawler to be redirected like anyone else, got %d", w.Code)
	}

	events := rec.collect(t, 1)
	if events[0].Kind != domain.ClickRequest {
		t.Errorf("a crawler request was recorded as %q, want %q", events[0].Kind, domain.ClickRequest)
	}
	if n := counted(events); n != 0 {
		t.Errorf("a crawler moved %d counters", n)
	}
}

// The guide page fires its confirmation when it loads, so every later tap is an interaction with a visit
// that is already counted. Counting them would make one person who copies the link and then opens it into
// two visitors.
//
// The action goes in its own field, and the referer stays empty: the Referer header of this POST is the
// guide page this backend served, so storing it would put our own domain at the top of every referrer
// report and keep the column unreadable for its actual purpose.
func TestGuidePageActionsAreRecordedButNotCounted(t *testing.T) {
	rec := newClickRecorder()
	r := newRedirectServer(t, rec)

	req := httptest.NewRequest("POST", "/r/abc123/click-action", strings.NewReader(`{"action":"copy_link"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", wechatUA)
	req.Header.Set("Referer", "https://kada.click/r/abc123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	events := rec.collect(t, 1)
	if events[0].Kind != domain.ClickAction {
		t.Errorf("a guide page action was recorded as %q, want %q", events[0].Kind, domain.ClickAction)
	}
	if n := counted(events); n != 0 {
		t.Errorf("an action moved %d counters", n)
	}
	if events[0].Action != domain.ActionCopyLink {
		t.Errorf("the action was recorded as %q, want %q", events[0].Action, domain.ActionCopyLink)
	}
	if events[0].Referer != "" {
		t.Errorf("the action was filed as a referrer again: %q", events[0].Referer)
	}
}

// The action arrives as free text from a public endpoint. An unrecognized one is refused rather than
// stored, because a value the client chooses is what made the referer column unreadable in the first
// place - and the row must not be written at all.
func TestUnknownActionIsRejectedAndNotStored(t *testing.T) {
	for _, action := range []string{`{"action":"whatever"}`, `{"action":""}`, `{}`, `{"action":"open_link "}`} {
		t.Run(action, func(t *testing.T) {
			rec := newClickRecorder()
			r := newRedirectServer(t, rec)

			req := httptest.NewRequest("POST", "/r/abc123/click-action", strings.NewReader(action))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", wechatUA)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d", action, w.Code)
			}

			select {
			case e := <-rec.events:
				t.Errorf("the rejected action %s was recorded anyway: %+v", action, e)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

// The action limiter is wired to one route, not to the group. That distinction is not cosmetic: the
// redirect ceiling is generous because a mobile carrier puts thousands of visitors behind one address, so a
// tight limit applied to the hot path would drop real visits - and the only symptom would be a dashboard
// that is quietly lower than the truth.
func TestOnlyTheActionEndpointIsBehindTheActionLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(&mockLinkService{})

	marked := func(c *gin.Context) { c.Header("X-Test-Limiter", "action") }
	r := gin.New()
	h.RegisterRoutes(r, nil, marked)

	for _, route := range []struct{ method, path string }{
		{"GET", "/r/abc123"},
		{"GET", "/r/abc123/qrcode"},
		{"POST", "/r/abc123/visit"},
		{"POST", "/r/abc123/verify-password"},
	} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader("password=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if got := w.Header().Get("X-Test-Limiter"); got != "" {
			t.Errorf("%s %s is behind the action limiter", route.method, route.path)
		}
	}

	req := httptest.NewRequest("POST", "/r/abc123/click-action", strings.NewReader(`{"action":"copy_link"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-Test-Limiter"); got != "action" {
		t.Errorf("the action endpoint is not behind the action limiter, so nothing bounds it")
	}
}

// The visit confirmation is written by the same page and has the same problem: the referer it sends is our
// own guide page. The visit still counts - it is the JavaScript signal that makes a click a click - but it
// carries no referer.
func TestConfirmedVisitCarriesNoReferer(t *testing.T) {
	rec := newClickRecorder()
	r := newRedirectServer(t, rec)

	req := httptest.NewRequest("POST", "/r/abc123/visit", nil)
	req.Header.Set("User-Agent", wechatUA)
	req.Header.Set("Referer", "https://kada.click/r/abc123")
	r.ServeHTTP(httptest.NewRecorder(), req)

	events := rec.collect(t, 1)
	if events[0].Kind != domain.ClickVisit {
		t.Fatalf("the confirmation was recorded as %q, want a visit", events[0].Kind)
	}
	if events[0].Referer != "" {
		t.Errorf("the confirmation filed our own page as the referrer: %q", events[0].Referer)
	}
}

// The page and the vocabulary have to move together. A button added to the template whose action is not in
// domain.AllActions is refused by a 400 no visitor ever sees - the button simply stops being recorded - and
// an action defined but never sent is dead code that hides the next rename. Both directions are checked
// against the template itself, which is the only place the list of buttons exists.
func TestEveryActionTheGuidePageSendsIsKnown(t *testing.T) {
	sent := make(map[domain.Action]bool)
	for _, match := range regexp.MustCompile(`logAction\('([a-z_]+)'\)`).FindAllStringSubmatch(guidePageHTML, -1) {
		action := domain.Action(match[1])
		sent[action] = true
		if !action.Known() {
			t.Errorf("the guide page sends %q, which the endpoint would reject", action)
		}
	}

	if len(sent) == 0 {
		t.Fatal("no logAction call was found in the guide page; this test is checking nothing")
	}
	for _, action := range domain.AllActions {
		if !sent[action] {
			t.Errorf("domain.AllActions defines %q but the guide page never sends it", action)
		}
	}
}

// The password form is the other way into a redirect, and it follows the same rule: typing a password is
// evidence of a person, but not of which client the answer was sent to.
func TestVerifyPasswordCountsOnlyWhenTheTargetIsSent(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		want      int
	}{
		{"browser gets the target", browserUA, 1},
		{"wechat gets the guide page", wechatUA, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newClickRecorder()
			r := newRedirectServer(t, rec)

			req := httptest.NewRequest("POST", "/r/abc123/verify-password", strings.NewReader("password=x"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("User-Agent", tt.userAgent)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK && w.Code != http.StatusFound {
				t.Fatalf("expected a redirect or the guide page, got %d", w.Code)
			}

			if n := counted(rec.collect(t, 1)); n != tt.want {
				t.Errorf("the visit was counted %d times, want %d", n, tt.want)
			}
		})
	}
}

// ========== What the redirect path reports about itself ==========

// Every answer names itself, and the counter moves with it.
//
// This is the test for the failure mode metrics have and logs do not: a branch that answers without being
// measured raises nothing, it just never appears in a series, and the only way to notice is to go looking
// for a graph that is not there. Starting from OutcomeUnknown and asserting that it did not move turns
// that silence into a failing test.
func TestEveryAnswerIsMeasured(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		link      *domain.LinkInfo
		notFound  bool
		want      string
	}{
		{"a browser is redirected", browserUA, nil, false, metrics.OutcomeRedirect},
		{"wechat gets the guide page", wechatUA, nil, false, metrics.OutcomeGuidePage},
		{"a crawler is redirected like anyone else", googlebotUA, nil, false, metrics.OutcomeRedirect},
		{"a password page is served", browserUA, &domain.LinkInfo{
			ID: 1, OriginalURL: "https://example.com/target", HasPassword: true,
		}, false, metrics.OutcomePasswordPage},
		{"an unsafe target is refused", browserUA, &domain.LinkInfo{
			ID: 1, OriginalURL: "javascript:alert(1)",
		}, false, metrics.OutcomeUnsafeTarget},
		{"an unknown code is a 404", browserUA, nil, true, metrics.OutcomeNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockLinkService{}
			if tt.link != nil {
				link := tt.link
				svc.getByCode = func(context.Context, string) (*domain.LinkInfo, error) { return link, nil }
			}
			if tt.notFound {
				svc.getByCode = func(context.Context, string) (*domain.LinkInfo, error) {
					return nil, domain.ErrLinkNotFound
				}
			}

			gin.SetMode(gin.TestMode)
			r := gin.New()
			NewHandler(svc).RegisterRoutes(r, nil, nil)

			before := testutil.ToFloat64(metrics.Redirects.WithLabelValues(tt.want))
			unnamed := testutil.ToFloat64(metrics.Redirects.WithLabelValues(metrics.OutcomeUnknown))

			do(r, "GET", "/r/abc123", tt.userAgent)

			if got := testutil.ToFloat64(metrics.Redirects.WithLabelValues(tt.want)) - before; got != 1 {
				t.Errorf("outcome=%q moved by %v, want 1", tt.want, got)
			}
			if got := testutil.ToFloat64(metrics.Redirects.WithLabelValues(metrics.OutcomeUnknown)) - unnamed; got != 0 {
				t.Errorf("an answer did not name its outcome: outcome=%q moved by %v", metrics.OutcomeUnknown, got)
			}
		})
	}
}

// ========== Password Page ==========

func TestRedirect_PasswordPage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &mockLinkService{
		getByCode: func(ctx context.Context, code string) (*domain.LinkInfo, error) {
			return &domain.LinkInfo{
				ID:          1,
				ShortCode:   code,
				OriginalURL: "https://example.com/target",
				Domain:      "kada.click",
				IsActive:    true,
				HasPassword: true,
			}, nil
		},
	}
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/r/:code", h.Redirect)

	req := httptest.NewRequest("GET", "/r/secret123", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for password page, got %d", w.Code)
	}
}
