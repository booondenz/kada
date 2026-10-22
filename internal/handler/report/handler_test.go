package report

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
)

type fakeReporter struct {
	gotIP string
	got   domain.CreateReportRequest
	call  func(ctx context.Context, req domain.CreateReportRequest) (*domain.AbuseReportInfo, error)
}

func (f *fakeReporter) Create(ctx context.Context, req domain.CreateReportRequest, ip string) (*domain.AbuseReportInfo, error) {
	f.got = req
	f.gotIP = ip
	if f.call != nil {
		return f.call(ctx, req)
	}
	return &domain.AbuseReportInfo{ID: 1, ShortCode: req.ShortCode, Reason: req.Reason}, nil
}

func testRouter(t *testing.T, svc Reporter) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	NewHandler(svc).RegisterRoutes(r.Group("/api"), nil)
	return r
}

func post(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/reports", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Real-IP", "203.0.113.9")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreate_FilesTheReportWithTheReporterAddress(t *testing.T) {
	svc := &fakeReporter{}

	w := post(t, testRouter(t, svc), `{"short_code":"abc123","reason":"gambling","detail":"it went to a casino"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if svc.got.ShortCode != "abc123" || svc.got.Reason != "gambling" {
		t.Errorf("report = %+v, want the code and the reason it was sent", svc.got)
	}
	// The address is kept for investigating a flood of reports, so it has to arrive.
	if svc.gotIP != "203.0.113.9" {
		t.Errorf("ip = %q, want the reporter's address", svc.gotIP)
	}
}

func TestCreate_RequiresAShortCodeAndAReason(t *testing.T) {
	svc := &fakeReporter{}

	w := post(t, testRouter(t, svc), `{"detail":"no code and no reason"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if svc.got.ShortCode != "" {
		t.Error("an incomplete report must not reach the service")
	}
}

// A refusal from the service is the caller's fault - an unknown reason, or a code that cannot exist - so it
// is a 400 rather than the 500 a bare error would become.
func TestCreate_ReportsARefusal(t *testing.T) {
	svc := &fakeReporter{call: func(context.Context, domain.CreateReportRequest) (*domain.AbuseReportInfo, error) {
		return nil, errors.New("unknown report reason")
	}}

	w := post(t, testRouter(t, svc), `{"short_code":"abc123","reason":"whatever"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "unknown report reason") {
		t.Errorf("the refusal must say what was wrong, got %s", w.Body.String())
	}
}
