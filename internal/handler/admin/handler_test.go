package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/middleware"
)

type fakeModerator struct {
	block   func(ctx context.Context, code, operator, reason, evidence string) (*domain.LinkInfo, error)
	unblock func(ctx context.Context, code, operator, reason string) error
	blocks  func(ctx context.Context, code string) ([]domain.LinkBlockInfo, error)
}

func (f *fakeModerator) Block(ctx context.Context, code, operator, reason, evidence string) (*domain.LinkInfo, error) {
	if f.block != nil {
		return f.block(ctx, code, operator, reason, evidence)
	}
	return &domain.LinkInfo{ShortCode: code}, nil
}

func (f *fakeModerator) Unblock(ctx context.Context, code, operator, reason string) error {
	if f.unblock != nil {
		return f.unblock(ctx, code, operator, reason)
	}
	return nil
}

func (f *fakeModerator) Blocks(ctx context.Context, code string) ([]domain.LinkBlockInfo, error) {
	if f.blocks != nil {
		return f.blocks(ctx, code)
	}
	return nil, nil
}

type fakeReports struct {
	lastLimit int
	list      func(ctx context.Context, limit int) ([]domain.AbuseReportInfo, error)
}

func (f *fakeReports) List(ctx context.Context, limit int) ([]domain.AbuseReportInfo, error) {
	f.lastLimit = limit
	if f.list != nil {
		return f.list(ctx, limit)
	}
	return nil, nil
}

// The operator route the test router mounts, with the secret the console would have typed in.
const testSecret = "s3cret"

// testRouter mounts the routes the way main.go does: RequireAdmin with the deployment's secret. `presented`
// is what the request carries, so an empty value is "no secret at all".
func testRouter(t *testing.T, h *Handler, presented string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if presented != "" {
			c.Request.Header.Set(middleware.AdminSecretHeader, presented)
		}
		c.Next()
	})
	h.RegisterRoutes(r.Group("/api"), func(c *gin.Context) { c.Next() }, middleware.RequireAdmin(testSecret))
	return r
}

func post(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The moderation routes are the ones a missing setting must not open up.
func TestBlockLink_RefusesANonOperator(t *testing.T) {
	reached := false
	h := NewHandler(&fakeModerator{block: func(context.Context, string, string, string, string) (*domain.LinkInfo, error) {
		reached = true
		return nil, nil
	}}, &fakeReports{})

	w := post(t, testRouter(t, h, "wrong-secret"), "/api/admin/links/abc123/block", `{"reason":"gambling"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if reached {
		t.Error("a non-operator must not reach the service")
	}
}

// No secret at all is the same refusal, and it is the one a browser sends before anybody has typed the
// secret into the console.
func TestBlockLink_RefusesARequestWithoutASecret(t *testing.T) {
	h := NewHandler(&fakeModerator{}, &fakeReports{})

	w := post(t, testRouter(t, h, ""), "/api/admin/links/abc123/block", `{"reason":"gambling"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestBlockLink_RecordsWhoDecided(t *testing.T) {
	var gotOperator, gotReason string
	h := NewHandler(&fakeModerator{
		block: func(_ context.Context, code, operator, reason, _ string) (*domain.LinkInfo, error) {
			gotOperator, gotReason = operator, reason
			return &domain.LinkInfo{ShortCode: code}, nil
		},
	}, &fakeReports{})

	w := post(t, testRouter(t, h, testSecret), "/api/admin/links/abc123/block", `{"reason":"gambling","evidence":"report #4"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if gotOperator != "operator" {
		t.Errorf("operator = %q, want the record to say an operator made the decision", gotOperator)
	}
	if gotReason != "gambling" {
		t.Errorf("reason = %q, want gambling", gotReason)
	}
}

func TestBlockLink_RequiresAReason(t *testing.T) {
	reached := false
	h := NewHandler(&fakeModerator{block: func(context.Context, string, string, string, string) (*domain.LinkInfo, error) {
		reached = true
		return nil, nil
	}}, &fakeReports{})

	w := post(t, testRouter(t, h, testSecret), "/api/admin/links/abc123/block", `{}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if reached {
		t.Error("a takedown with no reason must not reach the service")
	}
}

// Blocking something already stopped is a conflict rather than a success: the operator has lost track of
// what is stopped, and telling them "done" hides that.
func TestBlockLink_ReportsAlreadyStopped(t *testing.T) {
	h := NewHandler(&fakeModerator{block: func(context.Context, string, string, string, string) (*domain.LinkInfo, error) {
		return nil, domain.ErrLinkAlreadyBlocked
	}}, &fakeReports{})

	w := post(t, testRouter(t, h, testSecret), "/api/admin/links/abc123/block", `{"reason":"gambling"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestBlockLink_ReportsAnUnknownCode(t *testing.T) {
	h := NewHandler(&fakeModerator{block: func(context.Context, string, string, string, string) (*domain.LinkInfo, error) {
		return nil, domain.ErrLinkNotFound
	}}, &fakeReports{})

	w := post(t, testRouter(t, h, testSecret), "/api/admin/links/nope/block", `{"reason":"gambling"}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Restoring a link needs no justification, so an empty body is the ordinary request rather than a 400.
func TestUnblockLink_AcceptsAnEmptyBody(t *testing.T) {
	called := false
	h := NewHandler(&fakeModerator{unblock: func(context.Context, string, string, string) error {
		called = true
		return nil
	}}, &fakeReports{})

	w := post(t, testRouter(t, h, testSecret), "/api/admin/links/abc123/unblock", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !called {
		t.Error("the service must be asked to restore the link")
	}
}

func TestUnblockLink_ReportsALinkThatIsNotStopped(t *testing.T) {
	h := NewHandler(&fakeModerator{unblock: func(context.Context, string, string, string) error {
		return domain.ErrLinkNotBlocked
	}}, &fakeReports{})

	w := post(t, testRouter(t, h, testSecret), "/api/admin/links/abc123/unblock", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestUnblockLink_SurfacesAnUnexpectedFailure(t *testing.T) {
	h := NewHandler(&fakeModerator{unblock: func(context.Context, string, string, string) error {
		return errors.New("database is gone")
	}}, &fakeReports{})

	w := post(t, testRouter(t, h, testSecret), "/api/admin/links/abc123/unblock", "")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestLinkBlocks_ReturnsTheHistory(t *testing.T) {
	h := NewHandler(&fakeModerator{blocks: func(_ context.Context, code string) ([]domain.LinkBlockInfo, error) {
		return []domain.LinkBlockInfo{{ShortCode: code, Reason: "gambling", Operator: "13800000000"}}, nil
	}}, &fakeReports{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/links/abc123/blocks", nil)
	w := httptest.NewRecorder()
	testRouter(t, h, testSecret).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "13800000000") {
		t.Errorf("the history must name the operator, got %s", w.Body.String())
	}
}

func TestListReports_PassesTheLimitThrough(t *testing.T) {
	reports := &fakeReports{}
	h := NewHandler(&fakeModerator{}, reports)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/reports?limit=7", nil)
	w := httptest.NewRecorder()
	testRouter(t, h, testSecret).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if reports.lastLimit != 7 {
		t.Errorf("limit = %d, want 7", reports.lastLimit)
	}
}
